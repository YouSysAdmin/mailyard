// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/systemmail"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	dmodel "github.com/yousysadmin/mailyard/internal/models/domain"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
	sendermodel "github.com/yousysadmin/mailyard/internal/models/sender"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

// Fakes that FAIL the test when reached: every one of these is a step
// a system send has to skip, and reaching it is the bug.
type refusingProjects struct {
	store.ProjectStore
	t *testing.T
}

func (f *refusingProjects) Get(context.Context, string) (*projmodel.Project, error) {
	f.t.Error("the project row was read - strict senders or default headers were applied to system mail")

	return nil, nil
}

type refusingSuppressions struct {
	store.SuppressionStore
	t *testing.T
}

func (f *refusingSuppressions) FilterSuppressedForList(_ context.Context, _, _ string, emails []string) ([]string, []string, error) {
	f.t.Error("suppressions were consulted for system mail")

	return emails, nil, nil
}

type capturingEmails struct {
	store.EmailStore
	t   *testing.T
	put *emailmodel.Email
}

func (f *capturingEmails) Put(_ context.Context, e *emailmodel.Email) error {
	f.put = e

	return nil
}

func (f *capturingEmails) AcceptedSince(context.Context, string, time.Time) (int, error) {
	f.t.Error("the quota was checked for system mail")

	return 0, nil
}

type noSenders struct{ store.SenderStore }

func (noSenders) GetByEmail(context.Context, string, string) (*sendermodel.Sender, error) {
	return nil, nil
}

// SendSystem lends the project its servers and its verified domain and
// nothing else. The row it writes is marked, untracked, carries no
// project default header, lost no recipient to a suppression, spent no
// quota and raised no webhook.
func TestASystemSendSkipsQuotaSuppressionsAndDefaults(t *testing.T) {
	emails := &capturingEmails{t: t}
	emitted := 0
	svc := &Service{
		Store: &store.Store{
			Email:       emails,
			Project:     &refusingProjects{t: t},
			Suppression: &refusingSuppressions{t: t},
			Sender:      noSenders{},
			SMTPServer:  &fakeProjectServers{count: 0},
			SMTPGroup:   &fakeGroups{},
			SharedSMTP:  &fakeSharedPool{servers: []*ssmodel.Shared{sharedServer("pool-1", nil)}},
			Domain:      &fakeDomains{verified: &dmodel.Domain{Domain: "example.com", ProjectID: "proj-a", Verified: true}},
		},
		Log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		MaxAttempts: 3,
		Wake:        func() {},
		Emit:        func(context.Context, string, *emailmodel.Email) { emitted++ },
	}

	err := svc.SendSystem(t.Context(), "proj-a", systemmail.Message{
		From: "no-reply@example.com", To: []string{"blocked@x.test"}, Subject: "reset",
		HTML: "<p>link</p>", Text: "link", Headers: map[string]string{"Auto-Submitted": "auto-generated"},
	})
	if err != nil {
		t.Fatalf("SendSystem = %v", err)
	}

	e := emails.put
	if e == nil {
		t.Fatal("no row was written")
	}

	if !e.System || e.Tracked || e.Status != emailmodel.StatusQueued {
		t.Errorf("row: system=%v tracked=%v status=%s, want a queued untracked system row", e.System, e.Tracked, e.Status)
	}

	if len(e.Recipients) != 1 || e.Recipients[0] != "blocked@x.test" {
		t.Errorf("recipients = %v, want the one asked for", e.Recipients)
	}

	if e.Headers["Auto-Submitted"] != "auto-generated" {
		t.Errorf("headers = %v, want Auto-Submitted kept", e.Headers)
	}

	if emitted != 0 {
		t.Errorf("%d webhook events were raised for system mail", emitted)
	}
}

// Without the flag the same request goes through the project's rules,
// which is what proves the flag is what skipped them above.
func TestATenantSendStillReadsTheProject(t *testing.T) {
	reads := 0
	svc := &Service{
		Store: &store.Store{
			Project:    projectReads{n: &reads},
			Domain:     &fakeDomains{verified: &dmodel.Domain{Domain: "example.com", ProjectID: "proj-a", Verified: true}},
			SMTPServer: &fakeProjectServers{count: 0}, SMTPGroup: &fakeGroups{},
			SharedSMTP: &fakeSharedPool{servers: []*ssmodel.Shared{sharedServer("pool-1", nil)}},
		},
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if err := svc.Validate(t.Context(), "proj-a", &SendRequest{From: "no-reply@example.com", To: []string{"a@x.test"}, Subject: "s", Text: "t"}); err != nil {
		t.Fatalf("Validate = %v", err)
	}

	if reads != 1 {
		t.Fatalf("project read %d times during a tenant Validate, want 1 (strict senders)", reads)
	}
}

type projectReads struct {
	store.ProjectStore
	n *int
}

func (p projectReads) Get(context.Context, string) (*projmodel.Project, error) {
	*p.n++

	return &projmodel.Project{}, nil
}
