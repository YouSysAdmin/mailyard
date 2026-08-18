// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package systemmail

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

// fakePool is the shared SMTP pool, reduced to what this package asks
// of it.
type fakePool struct {
	servers []*ssmodel.Shared
	err     error
}

func (f *fakePool) ListEnabled(context.Context) ([]*ssmodel.Shared, error) {
	return f.servers, f.err
}

func at(from, name string) func() Address {
	return func() Address { return Address{From: from, FromName: name} }
}

func shared(name string, platformOnly bool) *ssmodel.Shared {
	s := &ssmodel.Shared{PlatformOnly: platformOnly}
	s.Name = name
	s.Host = "smtp.example.com"
	s.Port = 587

	return s
}

// Enabled is about CONFIGURATION - a from address and somewhere to
// ask. Whether the pool can deliver right now is Send's answer, since
// a server can be disabled between the two.
func TestEnabledNeedsAnAddressAndAPool(t *testing.T) {
	pool := &fakePool{servers: []*ssmodel.Shared{shared("relay", false)}}
	cases := map[string]struct {
		pool Pool
		from string
		want bool
	}{
		"address and pool": {pool, "a@example.com", true},
		"no address":       {pool, "", false},
		"no pool":          {nil, "a@example.com", false},
		"neither":          {nil, "", false},
	}
	for name, tc := range cases {
		s := New(tc.pool, at(tc.from, ""), discard())
		if s.Enabled() != tc.want {
			t.Errorf("%s: Enabled() = %v, want %v", name, s.Enabled(), tc.want)
		}
	}

	// Unconfigured Send must not error, so callers can fire and forget.
	s := New(nil, at("", ""), discard())
	if err := s.Send(t.Context(), []string{"a@example.com"}, "s", "h", "t"); err != nil {
		t.Errorf("unconfigured Send returned %v, want nil", err)
	}

	s.SendAsync([]string{"a@example.com"}, "s", "h", "t")
}

// A reserved row wins wherever it sits in the pool, and an install
// with none still gets platform mail off the first enabled server -
// marking one platform_only is how an operator SEPARATES the traffic,
// not how they turn it on.
func TestServerPrefersAReservedRow(t *testing.T) {
	cases := map[string]struct {
		servers []*ssmodel.Shared
		want    string
		wantErr error
	}{
		"reserved last": {
			[]*ssmodel.Shared{shared("tenants", false), shared("platform", true)}, "platform", nil,
		},
		"reserved first": {
			[]*ssmodel.Shared{shared("platform", true), shared("tenants", false)}, "platform", nil,
		},
		"none reserved": {
			[]*ssmodel.Shared{shared("first", false), shared("second", false)}, "first", nil,
		},
		"empty pool": {nil, "", ErrNoServer},
	}
	for name, tc := range cases {
		s := New(&fakePool{servers: tc.servers}, at("a@example.com", ""), discard())
		srv, err := s.Server(t.Context())
		if !errors.Is(err, tc.wantErr) {
			t.Errorf("%s: err = %v, want %v", name, err, tc.wantErr)
			continue
		}

		if tc.wantErr == nil && srv.Name != tc.want {
			t.Errorf("%s: picked %q, want %q", name, srv.Name, tc.want)
		}
	}
}

// An empty pool is not silence. The caller decides what to tell the
// user, and "no server" has a different fix from "the server said no".
func TestSendReportsAnEmptyPool(t *testing.T) {
	s := New(&fakePool{}, at("a@example.com", ""), discard())
	err := s.Send(t.Context(), []string{"b@example.com"}, "s", "h", "t")
	if !errors.Is(err, ErrNoServer) {
		t.Errorf("Send with an empty pool returned %v, want ErrNoServer", err)
	}
}

func TestNilSenderIsSafe(t *testing.T) {
	var s *Sender
	if s.Enabled() {
		t.Error("nil sender must report disabled")
	}

	if s.From() != "" {
		t.Error("nil sender must report an empty From")
	}
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
	with := New(nil, at("ops@example.com", "Mailyard Ops"), discard())
	if got := with.headerFrom(); got != `"Mailyard Ops" <ops@example.com>` {
		t.Errorf("headerFrom() = %q", got)
	}

	without := New(nil, at("ops@example.com", ""), discard())
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
