// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package alertmail

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

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

	return append([]sentMail(nil), f.sent...)
}

type fakeRecipients struct {
	project  []string
	admins   []string
	user     string
	projCall int
}

func (f *fakeRecipients) ProjectAlert(context.Context, string) ([]string, error) {
	f.projCall++

	return f.project, nil
}
func (f *fakeRecipients) PlatformAdmins(context.Context) ([]string, error)  { return f.admins, nil }
func (f *fakeRecipients) UserEmail(context.Context, string) (string, error) { return f.user, nil }

func testNotifier(m *fakeMail, r *fakeRecipients) *Notifier {
	return &Notifier{
		Mail:       m,
		Recipients: r,
		ConsoleURL: "https://mail.example.com/app",
		Log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
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
		Type: "apikey.created", ProjectID: "p1",
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
