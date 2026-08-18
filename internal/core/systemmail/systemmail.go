// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package systemmail sends the platform's own mail: project
// invitations, password resets, signup confirmations.
//
// Deliberately not through internal/domain/email: this mail belongs to
// the installation, and the tenant pipeline would charge a plan quota,
// land in a project's log, fire its webhooks and need that project to
// have configured a server - none of it right for a password reset.
//
// It carries no SMTP configuration of its own either, and sends
// through the SHARED POOL an admin already manages. A pool row marked
// platform_only reserves itself for this traffic.
//
// Every caller must tolerate a Sender that cannot deliver: invitations
// keep returning copyable links, so an install with no pool stays
// usable.
package systemmail

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/safego"
	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
	"github.com/yousysadmin/mailyard/internal/core/transport"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

// Pool is the part of the shared SMTP store this package needs. An
// interface rather than the store itself, because env constructs the
// Runtime and core packages may not import the domain ones back.
type Pool interface {
	ListEnabled(ctx context.Context) ([]*ssmodel.Shared, error)
}

// Address is where platform mail says it comes from. Runtime
// configuration, not yaml: everything else about the pool is edited in
// the console by the same admin, and an address that needs a restart
// to change is the one piece they cannot fix when it is wrong.
type Address struct {
	From     string
	FromName string
}

// Sender delivers platform mail through the shared pool. A nil
// *Sender is a valid no-op, so callers can hold one unconditionally.
type Sender struct {
	pool Pool

	// addr is read through a function so a settings change takes
	// effect without rebuilding the Sender - the settings cache
	// refreshes on write and every five minutes.
	addr        func() Address
	log         *slog.Logger
	sendTimeout time.Duration
}

// New builds a Sender. It never fails: with no pool or no From
// address, Enabled reports false and Send says what is missing.
func New(pool Pool, addr func() Address, log *slog.Logger) *Sender {
	return &Sender{pool: pool, addr: addr, log: log, sendTimeout: 30 * time.Second}
}

// Enabled reports whether platform mail is CONFIGURED - a From
// address is set and there is a pool to ask.
//
// It does not query. Whether a usable server exists right now is
// discovered by Send, which is the only place that can report it
// honestly: a server can be disabled between this call and delivery,
// and a synchronous probe on every invitation would not change that.
func (s *Sender) Enabled() bool {
	return s != nil && s.pool != nil && s.addr().From != ""
}

// From returns the configured envelope sender, for display.
func (s *Sender) From() string {
	if s == nil {
		return ""
	}

	return s.addr().From
}

// ErrNoServer is what Send returns when the pool holds nothing it can
// use. Separate from a delivery failure because the fix is different:
// one is "add a shared SMTP server", the other is "your server said
// no".
var ErrNoServer = errors.New("systemmail: no shared SMTP server available")

// Server picks the pool row platform mail leaves through: a
// platform_only row if the operator reserved one, else the first
// enabled row by priority.
//
// The fallback is deliberate. An install with a single shared server
// should not have to tick a box to get invitations working, and
// marking one platform_only is how an operator SEPARATES the traffic
// once they care - it is not how they enable it.
func (s *Sender) Server(ctx context.Context) (*ssmodel.Shared, error) {
	if s == nil || s.pool == nil {
		return nil, ErrNoServer
	}

	pool, err := s.pool.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}

	var first *ssmodel.Shared
	for _, srv := range pool {
		if srv.PlatformOnly {
			return srv, nil
		}

		if first == nil {
			first = srv
		}
	}

	if first == nil {
		return nil, ErrNoServer
	}

	return first, nil
}

// Send delivers one message. Returns nil without doing anything when
// no From address is configured, so callers do not have to branch -
// check Enabled() first only when the outcome changes what you tell
// the user.
func (s *Sender) Send(ctx context.Context, to []string, subject, html, text string) error {
	if !s.Enabled() {
		return nil
	}

	if len(to) == 0 {
		return fmt.Errorf("systemmail: no recipients")
	}

	srv, err := s.Server(ctx)
	if err != nil {
		return err
	}

	msg := &smtpclient.Message{
		From:    s.headerFrom(),
		To:      to,
		Subject: subject,
		HTML:    html,
		Text:    text,
		Headers: map[string]string{
			// Platform mail is transactional and must never be
			// auto-replied to or bulk-filtered as a campaign.
			"Auto-Submitted": "auto-generated",
		},
	}

	// Checked before paying for a connection. The SMTP provider is
	// synchronous with no context hook, so for that one the deadline is
	// advisory rather than a cancellation - an API-based provider does
	// honour it.
	if err := ctx.Err(); err != nil {
		return err
	}

	t, err := transport.Open(srv.Spec(nil))
	if err != nil {
		return fmt.Errorf("systemmail: %s: %w", srv.Name, err)
	}

	if err := t.Send(ctx, msg); err != nil {
		return fmt.Errorf("systemmail: send to %s via %s: %w",
			strings.Join(to, ", "), srv.Name, err)
	}

	s.log.Info("systemmail: sent", "to", to, "subject", subject, "server", srv.Name)

	return nil
}

// SendAsync delivers in the background and logs failures. Used where
// the HTTP response must not wait on (or fail because of) an SMTP
// round trip - inviting a member still succeeds when the mail server
// is briefly down, because the invitation link is returned anyway.
func (s *Sender) SendAsync(to []string, subject, html, text string) {
	if !s.Enabled() {
		return
	}

	safego.Go(s.log, "systemmail: send", func() {
		ctx, cancel := context.WithTimeout(context.Background(), s.sendTimeout)
		defer cancel()
		if err := s.Send(ctx, to, subject, html, text); err != nil {
			s.log.Error("systemmail: delivery failed", "to", to, "subject", subject, "err", err)
		}
	})
}

// TestConnection dials the server platform mail would use, without
// sending, for the admin-facing check.
func (s *Sender) TestConnection(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return ErrNoServer
	}

	if s.addr().From == "" {
		return fmt.Errorf("systemmail: no from address configured")
	}

	srv, err := s.Server(ctx)
	if err != nil {
		return err
	}

	t, err := transport.Open(srv.Spec(nil))
	if err != nil {
		return err
	}

	return t.Test(ctx)
}

// headerFrom renders the From header, adding the display name when
// the operator set one.
func (s *Sender) headerFrom() string {
	a := s.addr()
	if a.FromName == "" {
		return a.From
	}

	return smtpclient.FormatAddress(a.FromName, a.From)
}
