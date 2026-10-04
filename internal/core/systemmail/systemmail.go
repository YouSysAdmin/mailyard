// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package systemmail sends the platform's own mail: project
// invitations, password resets, signup confirmations, alert mail.
//
// Platform mail is a message OF THE PROJECT the platform_mail_project
// setting names. It is queued like any message of that project
// (ProjectSender, satisfied by email.Service) and goes out through
// whatever the project sends with - its SMTP servers, SES, relay
// nodes, the shared pool when it owns none - signed with the DKIM key
// of the project's verified domain. This package picks no server.
//
// The message is marked SYSTEM, so the project lends its servers,
// failover and signature and nothing else: no quota, no suppressions,
// no tracking, no default headers, no webhooks, no entry in its log.
// That is why there is no platform DKIM key of its own - the platform
// always has a project, since it sends its own marketing from the same
// domain, and that project signs the system mail too.
//
// Off until BOTH platform_mail_from and platform_mail_project are set.
// Every caller must tolerate that: invitations keep returning copyable
// links, password reset reports itself unavailable, and alerts are not
// sent.
package systemmail

import (
	"context"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/safego"
	"github.com/yousysadmin/mailyard/internal/core/safetext"
	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
)

// Message is what a ProjectSender is handed: the rendered parts and
// the headers platform mail always carries.
type Message struct {
	From    string
	To      []string
	Subject string
	HTML    string
	Text    string
	Headers map[string]string
}

// ProjectSender queues platform mail as a system message of a project.
// An interface rather than email.Service itself, because env
// constructs the Runtime and core packages may not import the domain
// ones back.
type ProjectSender interface {
	SendSystem(ctx context.Context, projID string, m Message) error

	// CheckSystem answers whether mail from `from` can leave through
	// the project at all - it exists, it verified the domain, something
	// can carry the message - without dialling anything.
	CheckSystem(ctx context.Context, projID, from string) error
}

// Address is where platform mail says it comes from and which project
// sends it. Runtime configuration, not yaml: both are edited in the
// console by the same admin, and a value that needs a restart to
// change is the one piece they cannot fix when it is wrong.
type Address struct {
	From     string
	FromName string

	// Project is the platform_mail_project setting, the id of the
	// project whose message this is. Empty means platform mail is off.
	Project string
}

// Sender queues platform mail. A nil *Sender is a valid no-op, so
// callers can hold one unconditionally.
type Sender struct {
	// addr is read through a function so a settings change takes
	// effect without rebuilding the Sender - the settings cache
	// refreshes on write and every five minutes.
	addr        func() Address
	log         *slog.Logger
	sendTimeout time.Duration

	// project is set after construction through UseProject, because
	// the email service that satisfies it needs the queue, and the
	// Sender is handed to the alert notifier before the queue exists.
	project ProjectSender

	// Background tracks SendAsync for shutdown. Nil starts it untracked.
	Background *safego.Group
}

// New builds a Sender. It never fails: with no address or no project,
// Enabled reports false and Send does nothing.
func New(addr func() Address, log *slog.Logger) *Sender {
	return &Sender{addr: addr, log: log, sendTimeout: 30 * time.Second}
}

// UseProject gives the Sender the way into a project's queue. Until
// it is called a named project is refused with a reason, not guessed
// around.
func (s *Sender) UseProject(p ProjectSender) {
	if s != nil {
		s.project = p
	}
}

// Enabled reports whether platform mail is CONFIGURED - an address and
// a project are both set.
//
// It does not query. Whether that project can deliver right now is
// Send's answer, which is the only place that can report it honestly:
// a server can be disabled between this call and delivery, and a
// synchronous probe on every invitation would not change that.
func (s *Sender) Enabled() bool {
	if s == nil {
		return false
	}

	a := s.addr()

	return a.From != "" && a.Project != ""
}

// From returns the configured sender address, for display.
func (s *Sender) From() string {
	if s == nil {
		return ""
	}

	return s.addr().From
}

// Project returns the id of the project platform mail is sent as,
// empty when none is set.
func (s *Sender) Project() string {
	if s == nil {
		return ""
	}

	return s.addr().Project
}

// Send queues one message. Returns nil without doing anything when
// platform mail is not configured, so callers do not have to branch -
// check Enabled() first only when the outcome changes what you tell
// the user.
func (s *Sender) Send(ctx context.Context, to []string, subject, html, text string) error {
	if !s.Enabled() {
		return nil
	}

	if len(to) == 0 {
		return fmt.Errorf("systemmail: no recipients")
	}

	// Refused HERE, with the setting named, rather than inside the
	// project's validation, where it would read as a bad request from
	// nobody in particular. The write path refuses this too, but a row
	// stored before it learned to still has to fail legibly.
	a := s.addr()
	if _, err := mail.ParseAddress(strings.TrimSpace(a.From)); err != nil {
		return fmt.Errorf("systemmail: platform_mail_from %q is not a usable address"+
			" - set a bare address like no-reply@example.com", a.From)
	}

	if s.project == nil {
		return fmt.Errorf("systemmail: platform_mail_project is set but this process cannot queue mail")
	}

	if err := s.project.SendSystem(ctx, a.Project, Message{
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
	}); err != nil {
		return fmt.Errorf("systemmail: queue through project %s: %w", a.Project, err)
	}

	// Masked, and no subject: a subject names what the message is about
	// - a password reset, an invitation to a named project - and with
	// the address beside it the line is a record of who was asked to do
	// what. The event kind is already in the message this sits under.
	s.log.Info("systemmail: queued", "to", safetext.MaskAddresses(to), "project_id", a.Project)

	return nil
}

// SendAsync queues in the background and logs failures. Used where the
// HTTP response must not wait on (or fail because of) the project's
// validation - inviting a member still succeeds when the platform
// project is misconfigured, because the invitation link is returned
// anyway.
func (s *Sender) SendAsync(to []string, subject, html, text string) {
	if !s.Enabled() {
		return
	}

	s.Background.Go(s.log, "systemmail: send", func() {
		ctx, cancel := context.WithTimeout(context.Background(), s.sendTimeout)
		defer cancel()
		if err := s.Send(ctx, to, subject, html, text); err != nil {
			s.log.Error("systemmail: delivery failed", "to", safetext.MaskAddresses(to), "err", err)
		}
	})
}

// Check asks whether platform mail can leave at all, for the
// admin-facing status and test: the settings are in place and the
// project can carry the address. Nothing is dialled - a candidate can
// be a pull-mode relay node, which cannot be.
func (s *Sender) Check(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("systemmail: not configured")
	}

	a := s.addr()
	if a.From == "" {
		return fmt.Errorf("systemmail: platform_mail_from is not set")
	}

	if a.Project == "" {
		return fmt.Errorf("systemmail: platform_mail_project is not set")
	}

	if s.project == nil {
		return fmt.Errorf("systemmail: platform_mail_project is set but this process cannot queue mail")
	}

	return s.project.CheckSystem(ctx, a.Project, a.From)
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
