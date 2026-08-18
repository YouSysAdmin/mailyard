// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package notify raises in-app notifications and the scheduled checks
// that produce them.
//
// It sits in core rather than in the notification domain because the
// producers are cross-cutting - a cron job here, the delivery worker
// there - and none of them should have to know how a notification is
// stored or how the live stream is fed.
package notify

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/eventbus"
	"github.com/yousysadmin/mailyard/internal/core/settings"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	nmodel "github.com/yousysadmin/mailyard/internal/models/notification"
	smodel "github.com/yousysadmin/mailyard/internal/models/setting"
)

// Alerter mails a notification that means something is wrong.
// Satisfied by alertmail.Notifier.
type Alerter interface {
	OnNotification(n *nmodel.Notification)
}

// Raiser files notifications and publishes them to the live stream.
type Raiser struct {
	Store *store.Store
	Bus   *eventbus.Bus
	Log   *slog.Logger

	// Alerts mails the ones that mean something is wrong. Optional: a
	// nil Alerter leaves the notification in the console only, which is
	// how this worked before there was any mail at all.
	Alerts Alerter
}

// Raise files a notification and, when it is genuinely new, pushes it
// to any live subscriber.
//
// A duplicate (same project and dedupe key) is silently dropped and
// not published: a repeat is not news, and pushing one would make a
// recurring check flash the console every time it ran.
func (r *Raiser) Raise(ctx context.Context, n *nmodel.Notification) {
	if r == nil || r.Store == nil || n == nil {
		return
	}

	created, err := r.Store.Notification.Create(ctx, n)
	if err != nil {
		// Best effort by design. A notification is a courtesy, and
		// failing the caller (a delivery worker, a cron job) because
		// one could not be filed would trade something that matters
		// for something that does not.
		if r.Log != nil {
			r.Log.Warn("notify: could not file notification",
				"project_id", n.ProjectID, "type", n.Type, "err", err)
		}

		return
	}

	if !created {
		return
	}

	if r.Log != nil {
		r.Log.Info("notify: raised",
			"project_id", n.ProjectID, "type", n.Type, "severity", n.Severity)
	}

	r.Bus.Publish(eventbus.Event{
		Type:      eventbus.TypeNotification,
		ProjectID: n.ProjectID,
		Data: map[string]any{
			"id":       n.ID,
			"type":     n.Type,
			"severity": n.Severity,
			"title":    n.Title,
			"body":     n.Body,
			"link":     n.Link,
		},
	})
	// Mailed only for a notification that is genuinely new - the
	// duplicate check above has already returned. That matters more here
	// than for the console badge: the bounce check runs every fifteen
	// minutes and dedupes per hour, so mailing before that gate would
	// send four times an hour for one problem.
	if r.Alerts != nil {
		r.Alerts.OnNotification(n)
	}
}

// BounceAlerter is the scheduled bounce-rate check.
//
// It runs on a timer rather than firing from the delivery path, for
// two reasons. A rate is a property of a window, not of one message,
// so it cannot be evaluated at the moment a single send fails. And
// hooking the failure path would make the alert fire hardest exactly
// when the system is already struggling.
type BounceAlerter struct {
	Store    *store.Store
	Settings *settings.Service
	Raiser   *Raiser
	Log      *slog.Logger
}

// Window is the period the rate is measured over. An hour is long
// enough for the denominator to mean something and short enough that
// an operator hears about a problem while it is still happening.
const Window = time.Hour

// Run evaluates every project and raises an alert where the rate is
// over the configured threshold.
func (b *BounceAlerter) Run(ctx context.Context) error {
	threshold := b.Settings.Int(smodel.KeyBounceAlertPercent)
	if threshold <= 0 {
		return nil // turned off
	}

	minVolume := max(b.Settings.Int(smodel.KeyBounceAlertMinVolume), 1)

	projects, err := b.Store.Project.List(ctx)
	if err != nil {
		return fmt.Errorf("list projects: %w", err)
	}

	now := time.Now().UTC()
	from := now.Add(-Window)

	for _, proj := range projects {
		counts, err := b.Store.Analytics.StatusBreakdown(ctx, proj.ID, from, now)
		if err != nil {
			// One project failing must not stop the sweep.
			if b.Log != nil {
				b.Log.Warn("bounce alert: status breakdown failed",
					"project_id", proj.ID, "err", err)
			}

			continue
		}

		// Only terminal outcomes count. Queued and processing rows
		// have not decided yet, and including them would dilute the
		// rate and hide a real problem behind a backlog.
		sent := counts["sent"]
		failed := counts["failed"]
		total := sent + failed
		if total < minVolume {
			continue
		}

		rate := failed * 100 / total
		if rate < threshold {
			continue
		}

		// The dedupe key pins the alert to one hour, so a rate that
		// stays high raises one notification per hour rather than one
		// per run.
		key := "bounce_rate:" + now.Format("2006-01-02T15")
		b.Raiser.Raise(ctx, &nmodel.Notification{
			ProjectID: proj.ID,
			Type:      nmodel.TypeBounceRate,
			Severity:  nmodel.SeverityWarning,
			Title:     fmt.Sprintf("Bounce rate is %d%%", rate),
			Body: fmt.Sprintf(
				"%d of %d messages that finished in the last hour failed. "+
					"Check the bounces list for the reasons, and pause sending if the addresses are stale.",
				failed, total),
			Link:      "/bounces",
			DedupeKey: key,
		})
	}

	return nil
}
