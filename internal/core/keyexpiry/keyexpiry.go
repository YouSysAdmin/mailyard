// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package keyexpiry reports sender signing keys that are about to run
// out.
//
// An S/MIME certificate expires on a date, and from that day every
// message the address sends shows the reader a broken seal - which is
// the exact signal the signature exists to give, now given about the
// sender's own mail. Nothing else fails: the send goes out, the log says
// sent, and the first report is a recipient asking why the mail looks
// forged. So the sweep is loud, and it is aimed at the PROJECT that
// owns the address rather than at the platform, because renewing a mail
// certificate is the project's errand.
//
// The platform's own certificates have the same sweep in certexpiry.
// Separate because the audience is different, not the mechanism.
package keyexpiry

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	nmodel "github.com/yousysadmin/mailyard/internal/models/notification"
	smodel "github.com/yousysadmin/mailyard/internal/models/sender"
)

// Store is the read side this needs.
type Store interface {
	SigningExpiringBefore(ctx context.Context, t time.Time) ([]*smodel.SigningKey, error)
}

// Mailer is the platform mail sender, as much of it as this uses.
type Mailer interface {
	Enabled() bool
	Send(ctx context.Context, to []string, subject, html, text string) error
}

// Notifier files an in-app notification without mailing it, since the
// digest below is the mail. Satisfied by notify.Raiser.
type Notifier interface {
	RaiseInConsole(ctx context.Context, n *nmodel.Notification)
}

// Recipients answers who in a project should hear about it.
type Recipients func(ctx context.Context, projectID string) ([]string, error)

// Window is how far ahead the sweep looks: thirty days is time to
// order a certificate from a public authority.
const Window = 30 * 24 * time.Hour

// Critical is when the tone changes from a warning to an error.
const Critical = 7 * 24 * time.Hour

// Checker is the sweep, registered as a scheduled job.
type Checker struct {
	Store      Store
	Mail       Mailer
	Recipients Recipients
	Notify     Notifier
	Log        *slog.Logger

	// lastMailed collapses the daily mail per project. In-process, so
	// one a day per node, the same trade certexpiry and alertmail make.
	mu         sync.Mutex
	lastMailed map[string]time.Time
}

// Run reports every key expiring inside the window, one digest per
// project.
func (c *Checker) Run(ctx context.Context) error {
	if c.Store == nil {
		return nil
	}

	rows, err := c.Store.SigningExpiringBefore(ctx, time.Now().Add(Window))
	if err != nil {
		return err
	}

	byProject := map[string][]*smodel.SigningKey{}
	for _, k := range rows {
		left := time.Until(*k.NotAfter)
		args := []any{
			"project_id", k.ProjectID, "sender", k.SenderEmail, "kind", k.Kind,
			"expires_at", k.NotAfter.Format(time.RFC3339), "days_left", int(left.Hours() / 24),
		}
		switch {
		case left <= 0:
			c.log().Error("signing keys: EXPIRED, every message from the address shows an invalid signature", args...)
		case left <= Critical:
			c.log().Error("signing keys: expires within a week", args...)
		default:
			c.log().Warn("signing keys: expires soon", args...)
		}

		byProject[k.ProjectID] = append(byProject[k.ProjectID], k)
	}

	for projectID, keys := range byProject {
		c.notify(ctx, projectID, keys)
		c.mail(ctx, projectID, keys)
	}

	return nil
}

// notify files one console notification per project per day, whether
// or not platform mail is configured.
func (c *Checker) notify(ctx context.Context, projectID string, keys []*smodel.SigningKey) {
	if c.Notify == nil {
		return
	}

	severity := nmodel.SeverityWarning
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		left := time.Until(*k.NotAfter)
		if left <= 0 {
			severity = nmodel.SeverityError
			lines = append(lines, k.SenderEmail+" ("+k.Kind+") has expired")

			continue
		}

		lines = append(lines, fmt.Sprintf("%s (%s) expires in %d days", k.SenderEmail, k.Kind, int(left.Hours()/24)))
	}

	c.Notify.RaiseInConsole(ctx, &nmodel.Notification{
		ProjectID: projectID,
		Type:      nmodel.TypeSigningKeyExpiry,
		Severity:  severity,
		Title:     "Sender signing certificates need renewing",
		Body:      strings.Join(lines, ", "),
		Link:      "/domains",
		DedupeKey: "signing_key_expiry:" + time.Now().UTC().Format("2006-01-02"),
	})
}

// mail sends one digest a day to a project while anything of its is
// inside the window.
func (c *Checker) mail(ctx context.Context, projectID string, keys []*smodel.SigningKey) {
	if c.Mail == nil || !c.Mail.Enabled() || c.Recipients == nil {
		return
	}

	if !c.claim(projectID) {
		return
	}

	to, err := c.Recipients(ctx, projectID)
	if err != nil || len(to) == 0 {
		return
	}

	subject, html, text := digest(keys)
	if err := c.Mail.Send(ctx, to, subject, html, text); err != nil {
		c.log().Error("signing keys: could not mail the expiry digest", "project_id", projectID, "err", err)
	}
}

func (c *Checker) claim(projectID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastMailed == nil {
		c.lastMailed = map[string]time.Time{}
	}

	if time.Since(c.lastMailed[projectID]) < 24*time.Hour {
		return false
	}

	c.lastMailed[projectID] = time.Now()

	return true
}

// digest renders the message. Short: it exists to get somebody to
// renew, not to explain itself.
func digest(keys []*smodel.SigningKey) (subject, html, text string) {
	worst := time.Until(*keys[0].NotAfter)
	switch {
	case worst <= 0:
		subject = "A sender signing certificate has EXPIRED"
	case worst <= Critical:
		subject = "A sender signing certificate expires within a week"
	default:
		subject = "Sender signing certificates are expiring soon"
	}

	var t, h strings.Builder
	t.WriteString("These sender signing keys are expiring:\n\n")
	h.WriteString("<p>These sender signing keys are expiring:</p><ul>")
	for _, k := range keys {
		days := int(time.Until(*k.NotAfter).Hours() / 24)
		line := fmt.Sprintf("%s (%s) - %s (%d days)", k.SenderEmail, strings.ToUpper(k.Kind),
			k.NotAfter.Format(time.DateOnly), days)
		fmt.Fprintf(&t, "  %s\n", line)
		fmt.Fprintf(&h, "<li>%s</li>", htmlEscape(line))
	}

	t.WriteString("\nMail from an address whose key has expired still goes out. " +
		"Only the signature shows as invalid, so nothing else will report this. " +
		"Import a renewed certificate under Domains, Sender Addresses.\n")
	h.WriteString("</ul><p>Mail from an address whose key has expired still goes out. " +
		"Only the signature shows as invalid, so nothing else will report this. " +
		"Import a renewed certificate under Domains, Sender Addresses.</p>")

	return subject, h.String(), t.String()
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")

	return r.Replace(s)
}

func (c *Checker) log() *slog.Logger {
	if c.Log != nil {
		return c.Log
	}

	return slog.Default()
}
