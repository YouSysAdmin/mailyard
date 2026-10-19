// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package alertmail

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/safego"
	amodel "github.com/yousysadmin/mailyard/internal/models/audit"
	nmodel "github.com/yousysadmin/mailyard/internal/models/notification"
)

type fakeMail struct {
	mu      sync.Mutex
	enabled bool
	sent    []sentMail
}

type sentMail struct {
	to      []string
	subject string
	text    string
}

func (f *fakeMail) Enabled() bool { return f.enabled }

func (f *fakeMail) SendAsync(to []string, subject, _, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sentMail{to: to, subject: subject, text: text})
}

func (f *fakeMail) all() []sentMail {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.sent)
}

type fakeRecipients struct {
	project  []string
	admins   []string
	user     string
	slug     string
	projCall int
}

func (f *fakeRecipients) ProjectAlert(context.Context, string) ([]string, error) {
	f.projCall++

	return f.project, nil
}
func (f *fakeRecipients) PlatformAdmins(context.Context) ([]string, error)    { return f.admins, nil }
func (f *fakeRecipients) UserEmail(context.Context, string) (string, error)   { return f.user, nil }
func (f *fakeRecipients) ProjectSlug(context.Context, string) (string, error) { return f.slug, nil }

func testNotifier(m *fakeMail, r *fakeRecipients) *Notifier {
	return &Notifier{
		Mail:       m,
		Recipients: r,
		ConsoleURL: "https://mail.example.com/app",
		Log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		Background: &safego.Group{},
	}
}

// deliver is called directly rather than through OnAudit, because
// OnAudit deliberately hands the work to a goroutine - the audit writer
// must not wait on a database round trip and an SMTP send. The
// asynchrony is the point there and would only make this test sleep.
func TestEachTierMailsItsOwnAudience(t *testing.T) {
	mail := &fakeMail{enabled: true}
	rec := &fakeRecipients{
		project: []string{"owner@example.com", "ops@example.com"},
		admins:  []string{"admin@example.com"},
		user:    "person@example.com",
	}
	n := testNotifier(mail, rec)

	cases := []struct {
		name  string
		event *amodel.Event
		want  []string
	}{
		{"an account's own credentials go to the account",
			&amodel.Event{Type: amodel.TypeTOTPDisabled, ActorID: "u1"},
			[]string{"person@example.com"}},
		{"a project's access goes to its owners and its alert address",
			&amodel.Event{Type: "apikey.created", ProjectID: "p1", ActorEmail: "dev@example.com"},
			[]string{"owner@example.com", "ops@example.com"}},
		{"the installation goes to its administrators",
			&amodel.Event{Type: "admin.apikeys", ActorEmail: "root@example.com"},
			[]string{"admin@example.com"}},
	}
	for _, tc := range cases {
		a, ok := Lookup(tc.event.Type)
		if !ok {
			t.Fatalf("%s: %s is not a mailable event", tc.name, tc.event.Type)
		}

		n.deliver(t.Context(), a, tc.event)
	}

	sent := mail.all()
	if len(sent) != len(cases) {
		t.Fatalf("sent %d mails, want %d", len(sent), len(cases))
	}

	for i, tc := range cases {
		if strings.Join(sent[i].to, ",") != strings.Join(tc.want, ",") {
			t.Errorf("%s: mailed %v, want %v", tc.name, sent[i].to, tc.want)
		}
	}
}

// A script minting keys in a loop is a normal caller. Twenty mails about
// one intention is how somebody comes to filter the whole channel, so
// the same audience and event collapse for a window - while the trail
// keeps every event.
func TestARepeatIsCollapsedAndADifferentEventIsNot(t *testing.T) {
	mail := &fakeMail{enabled: true}
	n := testNotifier(mail, &fakeRecipients{project: []string{"owner@example.com"}})

	created, _ := Lookup("apikey.created")
	deleted, _ := Lookup("apikey.deleted")
	for range 5 {
		n.deliver(t.Context(), created, &amodel.Event{Type: "apikey.created", ProjectID: "p1"})
	}

	if got := len(mail.all()); got != 1 {
		t.Errorf("five identical events sent %d mails, want 1", got)
	}

	// A different event type is different news.
	n.deliver(t.Context(), deleted, &amodel.Event{Type: "apikey.deleted", ProjectID: "p1"})
	if got := len(mail.all()); got != 2 {
		t.Errorf("a different event type sent %d mails in total, want 2", got)
	}

	// And so is the same event in another project - collapsing per
	// audience, not globally, or one busy tenant would silence the rest.
	n.deliver(t.Context(), created, &amodel.Event{Type: "apikey.created", ProjectID: "p2"})
	if got := len(mail.all()); got != 3 {
		t.Errorf("another project sent %d mails in total, want 3", got)
	}
}

// Without platform mail there is nothing to send, and resolving an
// audience is a database round trip - so the check has to come first.
func TestNothingHappensWithoutPlatformMail(t *testing.T) {
	mail := &fakeMail{enabled: false}
	rec := &fakeRecipients{project: []string{"owner@example.com"}}
	n := testNotifier(mail, rec)

	n.OnAudit(&amodel.Event{Type: "apikey.created", ProjectID: "p1"})
	n.OnNotification(&nmodel.Notification{
		ProjectID: "p1", Type: nmodel.TypeBounceRate,
		Severity: nmodel.SeverityWarning, Title: "Bounce rate is 40%",
	})
	if got := len(mail.all()); got != 0 {
		t.Errorf("sent %d mails with platform mail off, want 0", got)
	}

	if rec.projCall != 0 {
		t.Errorf("resolved recipients %d times with platform mail off, want 0", rec.projCall)
	}
}

// The setting is read fresh, so turning it off takes effect without a
// restart.
func TestTheSettingGatesEverything(t *testing.T) {
	mail := &fakeMail{enabled: true}
	on := false
	n := testNotifier(mail, &fakeRecipients{project: []string{"owner@example.com"}})
	n.Enabled = func() bool { return on }

	a, _ := Lookup("apikey.created")
	if n.on() {
		t.Error("on() is true while the setting is off")
	}

	on = true
	if !n.on() {
		t.Error("on() is false while the setting is on")
	}

	n.deliver(t.Context(), a, &amodel.Event{Type: "apikey.created", ProjectID: "p1"})
	if got := len(mail.all()); got != 1 {
		t.Errorf("sent %d mails with the setting on, want 1", got)
	}
}

// An informational notification is what the console badge is for.
// Mailing those would train people to ignore the ones that matter.
func TestOnlyAProblemIsMailed(t *testing.T) {
	mail := &fakeMail{enabled: true}
	n := testNotifier(mail, &fakeRecipients{project: []string{"owner@example.com"}})

	n.OnNotification(&nmodel.Notification{
		ProjectID: "p1", Type: nmodel.TypeCampaignDone,
		Severity: nmodel.SeverityInfo, Title: "Campaign finished",
	})
	if got := len(mail.all()); got != 0 {
		t.Errorf("an info notification sent %d mails, want 0", got)
	}
}

// An event nobody can be told about is not an error, and must not send
// mail to an empty recipient list - which some SMTP servers accept and
// deliver to nobody, and others reject as a bad transaction.
func TestNoAudienceSendsNothing(t *testing.T) {
	mail := &fakeMail{enabled: true}
	n := testNotifier(mail, &fakeRecipients{})

	project, _ := Lookup("apikey.created")
	account, _ := Lookup(amodel.TypeTOTPDisabled)
	// A project with no owners and no alert address.
	n.deliver(t.Context(), project, &amodel.Event{Type: "apikey.created", ProjectID: "p1"})
	// A project-tier event with no project at all, which happens: the
	// /projects routes decide access in handlers.
	n.deliver(t.Context(), project, &amodel.Event{Type: "apikey.created"})
	// An account event whose user has since been deleted.
	n.deliver(t.Context(), account, &amodel.Event{Type: amodel.TypeTOTPDisabled, ActorID: "gone"})
	if got := len(mail.all()); got != 0 {
		t.Errorf("sent %d mails with no audience, want 0", got)
	}
}

// The mail has to say who and what, or the recipient cannot tell their
// own action from somebody else's.
func TestTheMailNamesTheActorAndTheAction(t *testing.T) {
	mail := &fakeMail{enabled: true}
	n := testNotifier(mail, &fakeRecipients{project: []string{"owner@example.com"}})

	a, _ := Lookup("apikey.created")
	n.deliver(t.Context(), a, &amodel.Event{
		Type: "apikey.created", ProjectID: "p1", ActorID: "u1",
		ActorEmail: "dev@example.com", Method: "POST", Path: "/api/v1/api-keys/",
	})
	sent := mail.all()
	if len(sent) != 1 {
		t.Fatalf("sent %d mails, want 1", len(sent))
	}

	for _, want := range []string{"dev@example.com", "POST /api/v1/api-keys/", "https://mail.example.com/app/audit-log"} {
		if !strings.Contains(sent[0].text, want) {
			t.Errorf("the mail does not mention %q:\n%s", want, sent[0].text)
		}
	}
}

// An install with no public URL still gets the mail, without a link to
// nowhere.
func TestNoPublicURLMeansNoLink(t *testing.T) {
	mail := &fakeMail{enabled: true}
	n := testNotifier(mail, &fakeRecipients{admins: []string{"admin@example.com"}})
	n.ConsoleURL = ""

	a, _ := Lookup("admin.apikeys")
	n.deliver(t.Context(), a, &amodel.Event{Type: "admin.apikeys"})
	sent := mail.all()
	if len(sent) != 1 {
		t.Fatalf("sent %d mails, want 1", len(sent))
	}

	if strings.Contains(sent[0].text, "http") {
		t.Errorf("the mail carries a link with no public url configured:\n%s", sent[0].text)
	}
}

// Every account-tier alert has to answer "what if this was not me".
// That question is the only reason the mail is worth sending, and a note
// that just restates the heading does not answer it.
func TestAccountAlertsSayWhatToDo(t *testing.T) {
	for typ, a := range alerts {
		if a.Tier != TierAccount {
			continue
		}

		if a.Heading == "" || a.Note == "" {
			t.Errorf("%s has an empty heading or note", typ)
			continue
		}

		if len(a.Note) < 40 {
			t.Errorf("%s: the note is too short to say what to do about it: %q", typ, a.Note)
		}
	}
}

// The two events an alert must never be, spelled out so removing the
// reasoning takes deleting a test that states it.
func TestLoginEventsAreNotMailed(t *testing.T) {
	// A failed login mailed to its owner hands anybody who knows an
	// address a way to fill that inbox, and the useful signal is a rate.
	if _, ok := Lookup(amodel.TypeLoginFailed); ok {
		t.Error("auth.login.failed is mailed - an attacker can then flood any address they know")
	}

	// Worth mailing only with a notion of a new device. Without one it is
	// a mail per sign-in, which people filter within a week - and the
	// filter takes the alerts that matter with it.
	if _, ok := Lookup(amodel.TypeLoginSucceeded); ok {
		t.Error("auth.login.succeeded is mailed - one mail per sign-in trains people to filter the channel")
	}
}

// lure is text a tenant or a remote server chose, written to read like a
// message from the platform.
const lure = "URGENT verify your Example Bank account at https://evil.example/verify"

// noLure fails when a mailed part carries the lure or a piece of it.
func noLure(t *testing.T, m sentMail) {
	t.Helper()

	for _, part := range []string{m.subject, m.text} {
		for _, bit := range []string{"URGENT", "evil.example", "Example Bank"} {
			if strings.Contains(part, bit) {
				t.Errorf("the mail carries %q:\n%s\n%s", bit, m.subject, m.text)
			}
		}
	}
}

// A notification's title and body hold the server name the project chose
// and the reply its server sent. The mail is the platform's own wording,
// naming the project by its slug, and the detail stays in the console.
func TestANotificationIsMailedInThePlatformsWords(t *testing.T) {
	mail := &fakeMail{enabled: true}
	n := testNotifier(mail, &fakeRecipients{
		project: []string{"owner@example.com", "victim@third-party.example"},
		slug:    "acme-mail",
	})

	n.OnNotification(&nmodel.Notification{
		ProjectID: "p1", Type: nmodel.TypeSMTPInvalid, Severity: nmodel.SeverityError,
		Title: `SMTP server "Example Bank Security Team" was taken out of rotation`,
		Body:  "smtp auth failed: 535 " + lure + ". Test the connection to put it back.",
	})
	n.Background.Wait(5 * time.Second)

	sent := mail.all()
	if len(sent) != 1 {
		t.Fatalf("sent %d mails, want 1", len(sent))
	}

	noLure(t, sent[0])

	if want := notes[nmodel.TypeSMTPInvalid].Heading; sent[0].subject != want {
		t.Errorf("subject %q, want %q", sent[0].subject, want)
	}

	if !strings.Contains(sent[0].text, "Project: acme-mail") {
		t.Errorf("the mail does not name the project:\n%s", sent[0].text)
	}
}

// A notification type with no wording of its own is still mailed, and
// still without its title or body.
func TestAnUnlistedNotificationIsMailedInTheFallbackWords(t *testing.T) {
	mail := &fakeMail{enabled: true}
	n := testNotifier(mail, &fakeRecipients{project: []string{"owner@example.com"}})

	n.OnNotification(&nmodel.Notification{
		ProjectID: "p1", Type: "something_new", Severity: nmodel.SeverityWarning,
		Title: lure, Body: lure,
	})
	n.Background.Wait(5 * time.Second)

	sent := mail.all()
	if len(sent) != 1 {
		t.Fatalf("sent %d mails, want 1", len(sent))
	}

	noLure(t, sent[0])

	if sent[0].subject != noteFallback.Heading {
		t.Errorf("subject %q, want %q", sent[0].subject, noteFallback.Heading)
	}
}

// A webhook disabled by the dispatcher has no request behind it, and its
// detail is the endpoint URL and the endpoint's own error. Neither is
// mailed. Nor is the name of an API key, which its creator chose.
func TestAnAuditDetailIsNeverMailed(t *testing.T) {
	mail := &fakeMail{enabled: true}
	n := testNotifier(mail, &fakeRecipients{
		project: []string{"victim@third-party.example"},
		slug:    "acme-mail",
	})

	disabled, _ := Lookup(amodel.TypeWebhookDisabled)
	n.deliver(t.Context(), disabled, &amodel.Event{
		Type: amodel.TypeWebhookDisabled, ProjectID: "p1",
		Detail: "https://evil.example/verify?msg=URGENT - " + lure,
	})

	created, _ := Lookup("apikey.created")
	n.deliver(t.Context(), created, &amodel.Event{
		Type: "apikey.created", ProjectID: "p1", Method: "POST", Path: "/api/v1/api-keys",
		ActorEmail: "api key " + lure, Detail: "via api key " + lure,
	})

	sent := mail.all()
	if len(sent) != 2 {
		t.Fatalf("sent %d mails, want 2", len(sent))
	}

	for _, m := range sent {
		noLure(t, m)

		if !strings.Contains(m.text, "Project: acme-mail") {
			t.Errorf("the mail does not name the project:\n%s", m.text)
		}
	}

	if !strings.Contains(sent[1].text, "Who: an API key") {
		t.Errorf("the mail does not say an API key acted:\n%s", sent[1].text)
	}
}

// A project is named only in the shape a slug is held to.
func TestOnlyASlugShapedProjectIsNamed(t *testing.T) {
	mail := &fakeMail{enabled: true}
	n := testNotifier(mail, &fakeRecipients{
		project: []string{"owner@example.com"},
		slug:    "evil.example/verify",
	})

	a, _ := Lookup("apikey.created")
	n.deliver(t.Context(), a, &amodel.Event{Type: "apikey.created", ProjectID: "p1"})

	sent := mail.all()
	if len(sent) != 1 {
		t.Fatalf("sent %d mails, want 1", len(sent))
	}

	noLure(t, sent[0])
}
