// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package inbound

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/yousysadmin/mailyard/internal/domain/store"
	bouncemodel "github.com/yousysadmin/mailyard/internal/models/bounce"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
	imodel "github.com/yousysadmin/mailyard/internal/models/inbound"
)

type fakeEmails struct {
	store.EmailStore
	byID map[string]*emailmodel.Email
}

func (f *fakeEmails) GetAny(_ context.Context, id string) (*emailmodel.Email, error) {
	return f.byID[id], nil
}

type fakeBounces struct {
	store.BounceStore
	rows []*bouncemodel.Bounce
}

func (f *fakeBounces) Put(_ context.Context, b *bouncemodel.Bounce) error {
	f.rows = append(f.rows, b)

	return nil
}

// dsnWith builds a bounce report shaped the way a provider forwards
// one: the machine-readable part, then the original headers.
func dsnWith(failed string, originalHeaders string) []byte {
	return fmt.Appendf(nil, `From: MAILER-DAEMON@ses.amazonaws.com
To: bounces@mail.example.com
Subject: Delivery Status Notification (Failure)
Content-Type: multipart/report; report-type=delivery-status; boundary="b1"

--b1
Content-Type: text/plain

Your message could not be delivered.

--b1
Content-Type: message/delivery-status

Reporting-MTA: dns; ses.amazonaws.com

Final-Recipient: rfc822; %s
Action: failed
Status: 5.1.1
Diagnostic-Code: smtp; 550 5.1.1 user unknown

--b1
Content-Type: text/rfc822-headers

%s

--b1--
`, failed, originalHeaders)
}

func reportService(sent *emailmodel.Email) (*Service, *fakeBounces, *fakeSuppressions) {
	bounces := &fakeBounces{}
	sup := &fakeSuppressions{}
	emails := &fakeEmails{byID: map[string]*emailmodel.Email{}}
	if sent != nil {
		emails.byID[sent.ID] = sent
	}

	return &Service{
		Emails:       emails,
		Bounces:      bounces,
		Suppressions: sup,
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, bounces, sup
}

// arrival is the inbound row for a report that landed on the
// platform's own domain, which is where a provider forwards it. Its
// project is the operator's, deliberately not the sender's - that gap
// is what the header exists to bridge.
func arrival() *imodel.Email {
	return &imodel.Email{
		ProjectID:  "operator-project",
		Sender:     "MAILER-DAEMON@ses.amazonaws.com",
		Recipients: []string{"bounces@mail.example.com"},
	}
}

func TestBounceIsFiledAgainstTheSendingProjectNotTheReceivingOne(t *testing.T) {
	sent := &emailmodel.Email{
		ID: "e-1", ProjectID: "tenant-project",
		Sender: "news@user.com", Recipients: []string{"gone@example.org"},
	}
	svc, bounces, sup := reportService(sent)

	svc.processReport(t.Context(), arrival(),
		dsnWith("gone@example.org", "X-Mailyard-Email-Id: e-1\nSubject: hi"), "")

	if len(bounces.rows) != 1 {
		t.Fatalf("wrote %d bounce rows, want 1", len(bounces.rows))
	}

	b := bounces.rows[0]
	if b.ProjectID != "tenant-project" {
		t.Errorf("bounce filed against %q, want the project that sent the message", b.ProjectID)
	}

	if b.EmailID != "e-1" {
		t.Errorf("bounce email_id is %q, want e-1", b.EmailID)
	}

	if b.Type != bouncemodel.TypeHard {
		t.Errorf("bounce type is %q, want hard", b.Type)
	}

	if len(sup.recorded) != 1 || sup.recorded[0].ProjectID != "tenant-project" {
		t.Errorf("suppression rows: %+v", sup.recorded)
	}
}

// Anyone can post a syntactically perfect DSN at the MX. Without a
// valid sending id nothing is written, and the id is a uuid.
func TestAReportWithoutTheHeaderIsNotAttributed(t *testing.T) {
	sent := &emailmodel.Email{
		ID: "e-1", ProjectID: "tenant-project",
		Sender: "news@user.com", Recipients: []string{"gone@example.org"},
	}
	svc, bounces, sup := reportService(sent)

	svc.processReport(t.Context(), arrival(),
		dsnWith("gone@example.org", "Subject: hi\nMessage-ID: <whatever@ses>"), "")

	if len(bounces.rows) != 0 || len(sup.recorded) != 0 {
		t.Errorf("a report with no sending id wrote %d bounces and %d suppressions",
			len(bounces.rows), len(sup.recorded))
	}
}

func TestAReportNamingAnUnknownIdIsIgnored(t *testing.T) {
	svc, bounces, _ := reportService(nil)

	svc.processReport(t.Context(), arrival(),
		dsnWith("gone@example.org", "X-Mailyard-Email-Id: made-up"), "")

	if len(bounces.rows) != 0 {
		t.Errorf("an unknown sending id wrote %d bounce rows", len(bounces.rows))
	}
}

// Holding a valid id must not let somebody suppress an arbitrary
// address - the report can only speak about who that message went to.
func TestAReportCannotNameSomebodyTheMessageNeverWentTo(t *testing.T) {
	sent := &emailmodel.Email{
		ID: "e-1", ProjectID: "tenant-project",
		Sender: "news@user.com", Recipients: []string{"real@example.org"},
	}
	svc, bounces, sup := reportService(sent)

	svc.processReport(t.Context(), arrival(),
		dsnWith("victim@example.org", "X-Mailyard-Email-Id: e-1"), "")

	if len(bounces.rows) != 0 || len(sup.recorded) != 0 {
		t.Errorf("a report about an unrelated address wrote %d bounces and %d suppressions",
			len(bounces.rows), len(sup.recorded))
	}
}

// Stored recipients may carry a display name, a report never does.
func TestADisplayNameOnTheStoredRecipientStillMatches(t *testing.T) {
	sent := &emailmodel.Email{
		ID: "e-1", ProjectID: "tenant-project",
		Sender: "news@user.com", Recipients: []string{"Gone Away <gone@example.org>"},
	}
	svc, bounces, _ := reportService(sent)

	svc.processReport(t.Context(), arrival(),
		dsnWith("gone@example.org", "X-Mailyard-Email-Id: e-1"), "")

	if len(bounces.rows) != 1 {
		t.Fatalf("wrote %d bounce rows, want 1", len(bounces.rows))
	}
}

// A relay node a TENANT enrolled has less authority than our own MX.
//
// The unscoped rule above is right for the platform: a provider
// forwards its bounce copy to a mailbox on the operator's domain, so
// the receiving project and the sending project routinely differ and
// the id is the only thing that bridges them. A machine on a tenant's
// own network carries no such authority - and filing a bounce against
// a neighbour needs only a uuid and one address that message really
// went to, which a tenant who once received mail from that sender may
// well have.
func TestAScopedReportCannotReachAnotherProjectsMessage(t *testing.T) {
	sent := &emailmodel.Email{
		ID: "e-1", ProjectID: "neighbour-project",
		Sender: "news@user.com", Recipients: []string{"gone@example.org"},
	}
	svc, bounces, sup := reportService(sent)

	svc.processReport(t.Context(), arrival(),
		dsnWith("gone@example.org", "X-Mailyard-Email-Id: e-1\nSubject: hi"),
		"tenant-project")

	if len(bounces.rows) != 0 {
		t.Errorf("a tenant node filed %d bounce(s) against another project", len(bounces.rows))
	}

	if len(sup.recorded) != 0 {
		t.Errorf("a tenant node suppressed %d address(es) for another project", len(sup.recorded))
	}
}

// The same node reporting on its own project's mail is the entire
// point of letting it forward at all, so the scope must not refuse
// that too - a check that blocks everything looks identical to a
// check that works.
func TestAScopedReportStillReachesItsOwnProjectsMessage(t *testing.T) {
	sent := &emailmodel.Email{
		ID: "e-1", ProjectID: "tenant-project",
		Sender: "news@user.com", Recipients: []string{"gone@example.org"},
	}
	svc, bounces, _ := reportService(sent)

	svc.processReport(t.Context(), arrival(),
		dsnWith("gone@example.org", "X-Mailyard-Email-Id: e-1\nSubject: hi"),
		"tenant-project")

	if len(bounces.rows) != 1 {
		t.Fatalf("wrote %d bounce rows, want 1", len(bounces.rows))
	}

	if bounces.rows[0].ProjectID != "tenant-project" {
		t.Errorf("bounce filed against %q", bounces.rows[0].ProjectID)
	}
}
