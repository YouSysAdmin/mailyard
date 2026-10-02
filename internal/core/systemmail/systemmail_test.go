// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package systemmail

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

func at(from, name, project string) func() Address {
	return func() Address { return Address{From: from, FromName: name, Project: project} }
}

// fakeProject stands in for email.Service, recording what it is handed.
type fakeProject struct {
	sent    []Message
	projIDs []string
	checked int
	err     error
}

func (f *fakeProject) SendSystem(_ context.Context, projID string, m Message) error {
	f.sent = append(f.sent, m)
	f.projIDs = append(f.projIDs, projID)

	return f.err
}

func (f *fakeProject) CheckSystem(context.Context, string, string) error {
	f.checked++

	return f.err
}

// Enabled is about CONFIGURATION - an address and a project. Whether
// the project can deliver right now is Send's answer, since a server
// can be disabled between the two.
func TestEnabledNeedsAnAddressAndAProject(t *testing.T) {
	cases := map[string]struct {
		from, project string
		want          bool
	}{
		"address and project": {"a@example.com", "proj-1", true},
		"no address":          {"", "proj-1", false},
		"no project":          {"a@example.com", "", false},
		"neither":             {"", "", false},
	}
	for name, tc := range cases {
		s := New(at(tc.from, "", tc.project), discard())
		if s.Enabled() != tc.want {
			t.Errorf("%s: Enabled() = %v, want %v", name, s.Enabled(), tc.want)
		}
	}

	// Unconfigured Send must not error, so callers can fire and forget.
	s := New(at("", "", ""), discard())
	if err := s.Send(t.Context(), []string{"a@example.com"}, "s", "h", "t"); err != nil {
		t.Errorf("unconfigured Send returned %v, want nil", err)
	}

	s.SendAsync([]string{"a@example.com"}, "s", "h", "t")
}

// The message is queued as the named project's, with the headers
// platform mail always carries, and the check goes the same way.
func TestTheProjectIsHandedTheMessage(t *testing.T) {
	fp := &fakeProject{}
	s := New(at("a@example.com", "Mailyard", "proj-1"), discard())
	s.UseProject(fp)

	if err := s.Send(t.Context(), []string{"b@example.com"}, "hello", "<p>h</p>", "h"); err != nil {
		t.Fatalf("Send = %v", err)
	}

	if len(fp.sent) != 1 || fp.projIDs[0] != "proj-1" {
		t.Fatalf("the project was handed %d messages for %v, want 1 for proj-1", len(fp.sent), fp.projIDs)
	}

	m := fp.sent[0]
	if m.From != `"Mailyard" <a@example.com>` || m.Subject != "hello" || len(m.To) != 1 ||
		m.Headers["Auto-Submitted"] != "auto-generated" {
		t.Errorf("message = %+v", m)
	}

	if err := s.Check(t.Context()); err != nil || fp.checked != 1 {
		t.Errorf("Check = %v, checked %d times, want nil and 1", err, fp.checked)
	}

	// A refusal from the project comes back with the project named.
	fp.err = errors.New("domain not verified")
	err := s.Send(t.Context(), []string{"b@example.com"}, "hello", "<p>h</p>", "h")
	if err == nil || !strings.Contains(err.Error(), "proj-1") || !errors.Is(err, fp.err) {
		t.Errorf("Send with a refusing project = %v", err)
	}
}

// Configured but not wired: refused with the setting in the message,
// never silently dropped.
func TestANamedProjectWithoutAQueueIsRefused(t *testing.T) {
	s := New(at("a@example.com", "", "proj-1"), discard())
	err := s.Send(t.Context(), []string{"b@example.com"}, "s", "h", "t")
	if err == nil || !strings.Contains(err.Error(), "platform_mail_project") {
		t.Fatalf("Send with no queue = %v, want a refusal naming the setting", err)
	}

	if err := s.Check(t.Context()); err == nil || !strings.Contains(err.Error(), "platform_mail_project") {
		t.Fatalf("Check with no queue = %v, want a refusal naming the setting", err)
	}
}

// Check names the setting that is missing, so the admin status page
// says what to do rather than "not configured".
func TestCheckNamesTheMissingSetting(t *testing.T) {
	for setting, addr := range map[string]func() Address{
		"platform_mail_from":    at("", "", "proj-1"),
		"platform_mail_project": at("a@example.com", "", ""),
	} {
		s := New(addr, discard())
		s.UseProject(&fakeProject{})
		if err := s.Check(t.Context()); err == nil || !strings.Contains(err.Error(), setting) {
			t.Errorf("Check = %v, want a refusal naming %s", err, setting)
		}
	}
}

func TestNilSenderIsSafe(t *testing.T) {
	var s *Sender
	if s.Enabled() {
		t.Error("nil sender must report disabled")
	}

	if s.From() != "" || s.Project() != "" {
		t.Error("nil sender must report an empty From and Project")
	}

	s.UseProject(&fakeProject{})
}

// The name is QUOTED now, because this goes through
// smtpclient.FormatAddress rather than Sprintf.
//
// `"Mailyard Ops" <ops@example.com>` and `Mailyard Ops <ops@example.com>`
// are both valid RFC 5322 - a phrase may be atoms or a quoted string -
// and mail.Address.String() always quotes. Which is the point: it also
// quotes the name with a comma in it, where the hand-composed form
// produced two addresses.
func TestHeaderFromUsesDisplayName(t *testing.T) {
	with := New(at("ops@example.com", "Mailyard Ops", ""), discard())
	if got := with.headerFrom(); got != `"Mailyard Ops" <ops@example.com>` {
		t.Errorf("headerFrom() = %q", got)
	}

	without := New(at("ops@example.com", "", ""), discard())
	if got := without.headerFrom(); got != "ops@example.com" {
		t.Errorf("headerFrom() = %q", got)
	}
}

func TestPasswordResetMessageCarriesTheLink(t *testing.T) {
	link := "https://mail.example.com/admin/reset-password?token=abc123"
	subject, html, text := PasswordReset(link, 30)
	if subject == "" {
		t.Error("subject must not be empty")
	}

	for _, body := range []string{html, text} {
		if !strings.Contains(body, link) {
			t.Errorf("body is missing the reset link: %q", body)
		}

		if !strings.Contains(body, "30 minutes") {
			t.Errorf("body is missing the expiry: %q", body)
		}
	}
}

func TestInvitationMessageEscapesUntrustedNames(t *testing.T) {
	// The project name is operator-supplied text landing in HTML.
	subject, html, _ := Invitation(`Acme <script>alert(1)</script>`, "boss@example.com",
		"https://mail.example.com/admin/invitations?token=t", 168)
	if strings.Contains(html, "<script>") {
		t.Errorf("project name was not escaped: %q", html)
	}

	if !strings.Contains(html, "&lt;script&gt;") {
		t.Errorf("expected escaped markup in the body: %q", html)
	}

	if !strings.Contains(subject, "Acme") {
		t.Errorf("subject lost the project name: %q", subject)
	}
}

func TestInvitationFallsBackWhenInviterUnknown(t *testing.T) {
	_, _, text := Invitation("Acme", "", "https://example.com/x", 168)
	if !strings.Contains(text, "A project admin") {
		t.Errorf("expected a fallback inviter, got %q", text)
	}
}

// A stored from that is not an address fails HERE with the setting
// named, before the project is asked anything. The write path refuses
// such a value, but a row that predates the check still has to fail
// legibly.
func TestABrokenFromFailsBeforeQueueing(t *testing.T) {
	// A name without angle brackets - what typing "Name address" into
	// one field produces - is the shape that does not parse.
	fp := &fakeProject{}
	s := New(at("Mailyard no-reply@example.com", "", "proj-1"), discard())
	s.UseProject(fp)

	err := s.Send(t.Context(), []string{"a@example.com"}, "s", "h", "t")
	if err == nil || !strings.Contains(err.Error(), "platform_mail_from") {
		t.Fatalf("Send = %v, want a refusal naming platform_mail_from", err)
	}

	if len(fp.sent) != 0 {
		t.Fatal("the project was handed a message with a broken From")
	}
}
