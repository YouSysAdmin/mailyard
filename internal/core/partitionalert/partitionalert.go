// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package partitionalert tells a platform administrator that the emails
// table is running out of partitions before it does.
//
// The condition is slow and quiet and then sudden. Partition count only
// ever grows on an installation with retention_days = 0 - the operator
// saying keep everything - and it grows by 365 a year with nothing to
// notice. What it ends in is not a slow page: measured at 730
// partitions, sixteen concurrent queue claims failed outright with "out
// of shared memory", which is the delivery queue stopping.
//
// The maintainer already logs both levels. This exists because nobody
// reads a log for a condition that takes a year to arrive, and because
// the remedy - choosing a retention window, or raising
// max_locks_per_transaction, which needs a restart - is not something
// done the afternoon it is discovered.
//
// A separate package rather than a branch inside partition, for the
// reason certexpiry is one: the maintainer is a database concern and
// mail is not, and the wiring for who hears about it belongs where the
// runtime is assembled.
package partitionalert

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/partition"
)

// Counter is the read side this needs, which is one method of
// partition.Maintainer.
type Counter interface {
	Count(ctx context.Context) (partition.Health, error)
}

// Mailer is the platform mail sender, as much of it as this uses.
type Mailer interface {
	Enabled() bool
	Send(ctx context.Context, to []string, subject, html, text string) error
}

// Admins answers who should hear about it.
type Admins func(ctx context.Context) ([]string, error)

// Checker is the sweep, registered as a scheduled job.
type Checker struct {
	Counter Counter
	Mail    Mailer
	Admins  Admins
	Log     *slog.Logger

	// lastMailed collapses the mail to one a day.
	//
	// IN-PROCESS, so that means one a day PER NODE and a restart inside
	// the window sends another. The same trade certexpiry makes and for
	// the same reason: cron runs every job on every node with no leader
	// election, by design. Making this one durable would give a single
	// job shared state no other job has.
	lastMailed time.Time
}

func (c *Checker) log() *slog.Logger {
	if c.Log == nil {
		return slog.Default()
	}

	return c.Log
}

// Run reports the partition count when it has reached the point where
// an operator should act.
//
// Silent below that, deliberately. A healthy installation with a
// retention window sits at about 45 partitions forever, and a job that
// says so every hour is a job people filter.
func (c *Checker) Run(ctx context.Context) error {
	if c.Counter == nil {
		return nil
	}

	h, err := c.Counter.Count(ctx)
	if err != nil {
		return err
	}

	if !h.NearCeiling() {
		return nil
	}

	c.mail(ctx, h)

	return nil
}

// mail sends one warning a day while the count is above the threshold.
func (c *Checker) mail(ctx context.Context, h partition.Health) {
	if c.Mail == nil || !c.Mail.Enabled() || c.Admins == nil {
		return
	}

	if time.Since(c.lastMailed) < 24*time.Hour {
		return
	}

	to, err := c.Admins(ctx)
	if err != nil {
		// Loudly, like the send failure below: a warning that cannot
		// resolve its audience is a warning that silently never fires.
		c.log().Error("partitions: could not resolve the alert recipients", "err", err)

		return
	}

	if len(to) == 0 {
		return
	}

	subject, html, text := digest(h)
	if err := c.Mail.Send(ctx, to, subject, html, text); err != nil {
		// Not fatal and not silent: the maintainer's own log line
		// carries the same numbers, so the alarm has been raised even
		// when the mail cannot leave.
		c.log().Error("partitions: could not mail the ceiling warning", "err", err)

		return
	}

	c.lastMailed = time.Now()
}

// digest is the mail. It states the numbers, what breaks, and the two
// things that fix it - a warning an operator has to go and research is
// a warning that waits.
func digest(h partition.Health) (subject, html, text string) {
	urgency := "is approaching"
	if h.Over() {
		urgency = "has reached"
	}

	subject = fmt.Sprintf("Mailyard: the emails table %s its partition ceiling (%d of %d)",
		urgency, h.Partitions, h.Ceiling)

	body := fmt.Sprintf(
		"The emails table has %d daily partitions against a ceiling of %d.\n\n"+
			"What happens at the ceiling: every queue claim has to lock every live "+
			"partition, and past this number concurrent claims fail with \"out of "+
			"shared memory\" - which stops delivery rather than slowing it.\n\n"+
			"Why it is growing: retention_days is 0, meaning keep everything. That "+
			"adds 365 partitions a year and drops none.\n\n"+
			"Either fixes it:\n"+
			"  - set a retention window, and spent partitions are dropped nightly\n"+
			"  - raise max_locks_per_transaction on the database, which needs a restart\n",
		h.Partitions, h.Ceiling)

	return subject, "<pre>" + body + "</pre>", body
}
