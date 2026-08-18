// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package certexpiry reports certificates that are about to run out.
//
// A certificate is the one piece of configuration that breaks by doing
// nothing. Everything else in this installation fails when somebody
// changes it - this fails on a date, at once, for every client, and the
// first symptom is usually a customer saying mail stopped - because a
// listener with an expired certificate comes up perfectly and only the
// handshake fails.
//
// So the sweep is loud on purpose: a log line at a level that matches
// how close it is, and mail to the platform admins once a day while it
// is inside the window. Silence is the failure mode this exists to
// prevent.
package certexpiry

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	certmodel "github.com/yousysadmin/mailyard/internal/models/certificate"
)

// Store is the read side this needs.
type Store interface {
	ExpiringBefore(ctx context.Context, t time.Time) ([]*certmodel.Certificate, error)
}

// Mailer is the platform mail sender, as much of it as this uses.
// An interface so a test does not need one and so this package does
// not depend on the whole runtime.
type Mailer interface {
	Enabled() bool
	Send(ctx context.Context, to []string, subject, html, text string) error
}

// Admins answers who should hear about it.
type Admins func(ctx context.Context) ([]string, error)

// Window is how far ahead the sweep looks. Thirty days is chosen to
// sit outside ACME's own renewal point (autocert aims for thirty), so
// an ACME certificate inside this window means renewal is FAILING
// rather than pending - which is the thing worth waking somebody for.
const Window = 30 * 24 * time.Hour

// Critical is when the tone changes from a warning to an error.
const Critical = 7 * 24 * time.Hour

// Checker is the sweep, registered as a scheduled job.
type Checker struct {
	Store  Store
	Mail   Mailer
	Admins Admins
	Log    *slog.Logger

	// lastMailed collapses the daily mail. The job runs more often
	// than once a day so a certificate that lands inside the window at
	// noon is not first reported the following morning, and nobody
	// wants that frequency in their inbox.
	//
	// IN-PROCESS, so "one a day" means one a day PER NODE, and a restart
	// inside the window sends another. Written down because it looks like
	// an oversight and is the same trade the whole alert path makes -
	// alertmail's collapse window is an in-process map too, and cron
	// runs every job on every node with no leader election by design.
	//
	// Making this one durable would mean giving a single job shared state
	// that no other job has. If the duplication ever matters it should be
	// solved for the alert path as a whole, not here.
	lastMailed time.Time
}

// Run reports everything expiring inside the window.
func (c *Checker) Run(ctx context.Context) error {
	if c.Store == nil {
		return nil
	}

	rows, err := c.Store.ExpiringBefore(ctx, time.Now().Add(Window))
	if err != nil {
		return err
	}

	if len(rows) == 0 {
		return nil
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].NotAfter.Before(*rows[j].NotAfter)
	})

	for _, r := range rows {
		left := time.Until(*r.NotAfter)
		args := []any{
			"scope", r.Scope, "name", r.Name,
			"expires_at", r.NotAfter.Format(time.RFC3339),
			"days_left", int(left.Hours() / 24),
		}
		switch {
		case left <= 0:
			c.log().Error("certificates: EXPIRED, every handshake against it is failing", args...)
		case left <= Critical:
			c.log().Error("certificates: expires within a week", args...)
		default:
			c.log().Warn("certificates: expires soon", args...)
		}
	}

	c.mail(ctx, rows)

	return nil
}

// mail sends one digest a day while anything is inside the window.
func (c *Checker) mail(ctx context.Context, rows []*certmodel.Certificate) {
	if c.Mail == nil || !c.Mail.Enabled() || c.Admins == nil {
		return
	}

	if time.Since(c.lastMailed) < 24*time.Hour {
		return
	}

	to, err := c.Admins(ctx)
	if err != nil || len(to) == 0 {
		return
	}

	subject, html, text := digest(rows)
	if err := c.Mail.Send(ctx, to, subject, html, text); err != nil {
		// Not fatal, and not silent: the log above already carries
		// every line of this, so the sweep has done its job even when
		// the mail cannot leave.
		c.log().Error("certificates: could not mail the expiry digest", "err", err)

		return
	}

	c.lastMailed = time.Now()
}

// digest renders the message. Plain and short - it exists to get
// somebody to look, not to explain itself.
func digest(rows []*certmodel.Certificate) (subject, html, text string) {
	worst := time.Until(*rows[0].NotAfter)
	switch {
	case worst <= 0:
		subject = "A certificate has EXPIRED"
	case worst <= Critical:
		subject = "A certificate expires within a week"
	default:
		subject = "Certificates are expiring soon"
	}

	var t, h strings.Builder
	t.WriteString("These certificates are expiring:\n\n")
	h.WriteString("<p>These certificates are expiring:</p><ul>")
	for _, r := range rows {
		days := int(time.Until(*r.NotAfter).Hours() / 24)
		name := r.Name
		if name == "" {
			name = "(the only one in this scope)"
		}

		line := fmt.Sprintf("%s / %s - %s (%d days)",
			r.Scope, name, r.NotAfter.Format(time.DateOnly), days)
		fmt.Fprintf(&t, "  %s\n", line)
		fmt.Fprintf(&h, "<li>%s</li>", htmlEscape(line))
	}

	t.WriteString("\nA listener whose certificate has expired still starts. " +
		"Only the handshake fails, so nothing else will report this.\n")
	h.WriteString("</ul><p>A listener whose certificate has expired still starts. " +
		"Only the handshake fails, so nothing else will report this.</p>")

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
