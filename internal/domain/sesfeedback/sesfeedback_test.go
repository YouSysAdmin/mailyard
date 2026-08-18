// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package sesfeedback

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	bmodel "github.com/yousysadmin/mailyard/internal/models/bounce"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
	supmodel "github.com/yousysadmin/mailyard/internal/models/suppression"
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
	rows []*bmodel.Bounce
}

func (f *fakeBounces) Put(_ context.Context, b *bmodel.Bounce) error {
	f.rows = append(f.rows, b)

	return nil
}

type fakeSuppressions struct {
	store.SuppressionStore
	rows []*supmodel.Suppression
}

func (f *fakeSuppressions) Upsert(_ context.Context, s *supmodel.Suppression) error {
	f.rows = append(f.rows, s)

	return nil
}

// The delivery rows a topic can be traced back to. A notification is
// only entitled to speak about a message that left through a server
// publishing to its topic, so the tests need both tables.
type fakeShared struct {
	store.SharedSMTPStore
	byID map[string]*ssmodel.Shared
}

func (f *fakeShared) Get(_ context.Context, id string) (*ssmodel.Shared, error) {
	return f.byID[id], nil
}

type fakeServers struct {
	store.SMTPServerStore
	byID map[string]*ssmodel.Server
}

func (f *fakeServers) GetAny(_ context.Context, id string) (*ssmodel.Server, error) {
	return f.byID[id], nil
}

func testHandler(t *testing.T, sent *emailmodel.Email) (*Handler, *fakeBounces, *fakeSuppressions) {
	t.Helper()
	emails := &fakeEmails{byID: map[string]*emailmodel.Email{}}
	if sent != nil {
		emails.byID[sent.ID] = sent
	}

	bounces := &fakeBounces{}
	sup := &fakeSuppressions{}
	shared := &fakeShared{byID: map[string]*ssmodel.Shared{
		"server-1": {Server: ssmodel.Server{ID: "server-1", SESTopicARN: "arn:topic"}},
	}}
	servers := &fakeServers{byID: map[string]*ssmodel.Server{
		"tenant-server": {ID: "tenant-server", ProjectID: "other", SESTopicARN: "arn:other-topic"},
	}}

	return &Handler{
		Runtime: &env.Runtime{
			Config: &env.Config{},
			Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
			Store: &store.Store{
				Email: emails, Bounce: bounces, Suppression: sup,
				SharedSMTP: shared, SMTPServer: servers,
			},
		},
	}, bounces, sup
}

func sentMessage() *emailmodel.Email {
	return &emailmodel.Email{
		ID: "e-1", ProjectID: "tenant-project",
		Sender: "news@user.com", Recipients: []string{"gone@example.org"},
		// The topic must match the server that actually carried it.
		DeliveredVia: "server-1",
	}
}

const permanentBounce = `{
  "notificationType": "Bounce",
  "mail": {
    "messageId": "0100019-ses",
    "headers": [
      {"name": "From", "value": "news@user.com"},
      {"name": "X-Mailyard-Email-Id", "value": "e-1"}
    ],
    "headersTruncated": false
  },
  "bounce": {
    "bounceType": "Permanent",
    "bounceSubType": "General",
    "bouncedRecipients": [
      {"emailAddress": "gone@example.org", "status": "5.1.1", "diagnosticCode": "smtp; 550 user unknown"}
    ]
  }
}`

// The notification arrives over HTTPS with no project attached to it.
// The header is what says whose message it was about.
func TestPermanentBounceIsFiledAgainstTheSendingProject(t *testing.T) {
	h, bounces, sup := testHandler(t, sentMessage())
	h.record(t.Context(), "arn:topic", permanentBounce)

	if len(bounces.rows) != 1 {
		t.Fatalf("wrote %d bounce rows, want 1", len(bounces.rows))
	}

	b := bounces.rows[0]
	if b.ProjectID != "tenant-project" || b.EmailID != "e-1" {
		t.Errorf("bounce filed as %+v", b)
	}

	if b.Type != bmodel.TypeHard {
		t.Errorf("bounce type is %q, want hard", b.Type)
	}

	if len(sup.rows) != 1 || sup.rows[0].Kind != supmodel.KindBounce {
		t.Errorf("suppressions: %+v", sup.rows)
	}
}

// A full mailbox is not a dead address. Transient is recorded so the
// operator can see it and must not suppress.
func TestATransientBounceDoesNotSuppress(t *testing.T) {
	h, bounces, sup := testHandler(t, sentMessage())
	h.record(t.Context(), "arn:topic", `{
      "notificationType": "Bounce",
      "mail": {"headers": [{"name": "X-Mailyard-Email-Id", "value": "e-1"}]},
      "bounce": {
        "bounceType": "Transient",
        "bounceSubType": "MailboxFull",
        "bouncedRecipients": [{"emailAddress": "gone@example.org", "status": "4.2.2"}]
      }
    }`)

	if len(bounces.rows) != 1 || bounces.rows[0].Type != bmodel.TypeSoft {
		t.Fatalf("bounce rows: %+v", bounces.rows)
	}

	if len(sup.rows) != 0 {
		t.Errorf("a transient bounce suppressed the address: %+v", sup.rows)
	}
}

func TestAComplaintSuppresses(t *testing.T) {
	h, bounces, sup := testHandler(t, sentMessage())
	h.record(t.Context(), "arn:topic", `{
      "notificationType": "Complaint",
      "mail": {"headers": [{"name": "X-Mailyard-Email-Id", "value": "e-1"}]},
      "complaint": {
        "complainedRecipients": [{"emailAddress": "gone@example.org"}],
        "complaintFeedbackType": "abuse"
      }
    }`)

	if len(bounces.rows) != 1 || bounces.rows[0].Type != bmodel.TypeComplaint {
		t.Fatalf("bounce rows: %+v", bounces.rows)
	}

	if len(sup.rows) != 1 || sup.rows[0].Kind != supmodel.KindComplaint {
		t.Errorf("suppressions: %+v", sup.rows)
	}
}

// Include original headers is a checkbox in SES that an operator can
// forget. Without it there is nothing to attribute to, and guessing
// from the address alone is what the whole design avoids.
func TestANotificationWithoutTheHeaderWritesNothing(t *testing.T) {
	h, bounces, sup := testHandler(t, sentMessage())
	h.record(t.Context(), "arn:topic", `{
      "notificationType": "Bounce",
      "mail": {"headers": [{"name": "From", "value": "news@user.com"}], "headersTruncated": true},
      "bounce": {
        "bounceType": "Permanent",
        "bouncedRecipients": [{"emailAddress": "gone@example.org"}]
      }
    }`)

	if len(bounces.rows) != 0 || len(sup.rows) != 0 {
		t.Errorf("wrote %d bounces and %d suppressions with no sending id",
			len(bounces.rows), len(sup.rows))
	}
}

// Holding a valid id is not authority over an arbitrary address.
func TestANotificationCannotNameSomebodyTheMessageNeverWentTo(t *testing.T) {
	h, bounces, sup := testHandler(t, sentMessage())
	h.record(t.Context(), "arn:topic", `{
      "notificationType": "Bounce",
      "mail": {"headers": [{"name": "X-Mailyard-Email-Id", "value": "e-1"}]},
      "bounce": {
        "bounceType": "Permanent",
        "bouncedRecipients": [{"emailAddress": "victim@example.org"}]
      }
    }`)

	if len(bounces.rows) != 0 || len(sup.rows) != 0 {
		t.Errorf("a notification about an unrelated address wrote %d bounces and %d suppressions",
			len(bounces.rows), len(sup.rows))
	}
}

// Delivery notifications carry no failure. Recording one would put a
// successful send in the bounce list.
func TestADeliveryNotificationWritesNothing(t *testing.T) {
	h, bounces, _ := testHandler(t, sentMessage())
	h.record(t.Context(), "arn:topic", `{
      "notificationType": "Delivery",
      "mail": {"headers": [{"name": "X-Mailyard-Email-Id", "value": "e-1"}]},
      "delivery": {"recipients": ["gone@example.org"]}
    }`)

	if len(bounces.rows) != 0 {
		t.Errorf("a delivery notification wrote %d bounce rows", len(bounces.rows))
	}
}

// An operator can publish these from a configuration set event
// destination instead of the identity notification screen the docs
// describe. Same topic, same payload, one renamed key - and a silent
// no-op if only notificationType is read.
func TestAConfigurationSetEventIsRecordedTheSameWay(t *testing.T) {
	h, bounces, sup := testHandler(t, sentMessage())
	h.record(t.Context(), "arn:topic", `{
      "eventType": "Bounce",
      "mail": {"headers": [{"name": "X-Mailyard-Email-Id", "value": "e-1"}]},
      "bounce": {
        "bounceType": "Permanent",
        "bounceSubType": "General",
        "bouncedRecipients": [{"emailAddress": "gone@example.org", "status": "5.1.1"}]
      }
    }`)

	if len(bounces.rows) != 1 || bounces.rows[0].Type != bmodel.TypeHard {
		t.Fatalf("bounce rows: %+v", bounces.rows)
	}

	if bounces.rows[0].EmailID != "e-1" {
		t.Errorf("bounce filed against %q", bounces.rows[0].EmailID)
	}

	if len(sup.rows) != 1 {
		t.Errorf("suppressions: %+v", sup.rows)
	}
}

// The rule the per-server topic exists for.
//
// Attribution already comes from the header, so this is not how a
// bounce finds its message. It is what stops one tenant's topic
// reporting about another tenant's mail - which the old
// installation-wide allowlist allowed, because it never connected the
// topic to anything the message knew.
func TestATopicCannotReportOnAnotherServersMail(t *testing.T) {
	h, bounces, sup := testHandler(t, sentMessage())

	// The message left through server-1, whose topic is arn:topic.
	// This notification arrives on somebody else's.
	h.record(t.Context(), "arn:other-topic", permanentBounce)

	if len(bounces.rows) != 0 || len(sup.rows) != 0 {
		t.Errorf("a topic reported on mail that did not leave through its server: %d bounces, %d suppressions",
			len(bounces.rows), len(sup.rows))
	}
}

// A message that never left, or one predating the column, has no
// delivering server. An SES topic cannot have observed either, so
// waving it through would reopen the hole for exactly the rows least
// able to defend themselves.
func TestAMessageWithNoDeliveringServerIsRefused(t *testing.T) {
	sent := sentMessage()
	sent.DeliveredVia = ""
	h, bounces, sup := testHandler(t, sent)

	h.record(t.Context(), "arn:topic", permanentBounce)

	if len(bounces.rows) != 0 || len(sup.rows) != 0 {
		t.Errorf("a message with no recorded delivering server was reported on: %d bounces, %d suppressions",
			len(bounces.rows), len(sup.rows))
	}
}

// A tenant's own SES server is an smtp_servers row, and the topic has
// to resolve there too - not only in the shared pool.
func TestAProjectServersTopicResolves(t *testing.T) {
	sent := sentMessage()
	sent.DeliveredVia = "tenant-server"
	h, bounces, _ := testHandler(t, sent)

	h.record(t.Context(), "arn:other-topic", permanentBounce)

	if len(bounces.rows) != 1 {
		t.Fatalf("wrote %d bounce rows, want 1", len(bounces.rows))
	}
}
