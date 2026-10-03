// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package cli

import (
	"context"
	"log/slog"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/certexpiry"
	"github.com/yousysadmin/mailyard/internal/core/cron"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/keyexpiry"
	"github.com/yousysadmin/mailyard/internal/core/notify"
	"github.com/yousysadmin/mailyard/internal/core/partition"
	"github.com/yousysadmin/mailyard/internal/core/partitionalert"
	"github.com/yousysadmin/mailyard/internal/core/retention"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/domain/relaynode"
	"github.com/yousysadmin/mailyard/internal/domain/store"
)

// registerJobs registers the scheduled jobs this role runs on rt.Cron.
// The worker role runs the sweeps, every role refreshes its settings.
// Partition maintenance also runs once here, on ctx, before the first
// tick.
func registerJobs(ctx context.Context, r role, rt *env.Runtime, st *store.Store, db database.Database, log *slog.Logger) {
	// The assignment sweep, on worker nodes: a pull node that stopped
	// claiming loses its messages to the next candidate.
	if r.worker {
		rt.Cron.Register(cron.Job{
			Name:     "relay-assignments",
			Schedule: cron.EveryInterval(time.Minute),
			Run: func(ctx context.Context) error {
				n, err := relaynode.ReleaseExpired(ctx, rt)
				if n > 0 {
					rt.Log.Warn("relay node: assignments taken back from nodes that stopped claiming", "released", n)
				}

				return err
			},
		})
	}

	// settings-refresh is registered on every role, unlike the sweeps
	// below. It is not maintenance - it is how a node's settings
	// cache learns about a change written on another node, so an api
	// node that skipped it would serve stale settings forever.
	rt.Cron.Register(cron.Job{
		Name:     "settings-refresh",
		Schedule: cron.EveryInterval(5 * time.Minute),
		Run:      rt.Settings.Reload,
	})

	// A certificate is the one thing here that breaks by doing
	// nothing, and a listener holding an expired one starts perfectly
	// - only the handshake fails. Worker role, like the other sweeps.
	if r.worker {
		expiry := &certexpiry.Checker{
			Store: st.Certificate,
			Mail:  rt.SystemMail,
			Admins: func(ctx context.Context) ([]string, error) {
				users, err := st.User.List(ctx)
				if err != nil {
					return nil, err
				}

				var to []string
				for _, u := range users {
					if u.IsAdmin() && !u.Disabled && u.Email != "" {
						to = append(to, u.Email)
					}
				}

				return to, nil
			},
			Log: log,
		}

		rt.Cron.Register(cron.Job{
			Name: "certificate-expiry",

			// Six-hourly rather than daily: a certificate that enters
			// the window at noon should not first be reported the
			// following morning. The mail itself is capped at one a
			// day inside the checker.
			Schedule: cron.EveryInterval(6 * time.Hour),
			Run:      expiry.Run,
		})

		// The same sweep for sender signing certificates, aimed at the
		// project that owns the address: renewing one is its errand.
		keys := &keyexpiry.Checker{
			Store:      st.Sender,
			Mail:       rt.SystemMail,
			Recipients: st.AlertRecipients.ProjectAlert,
			Notify:     rt.Notify,
			Log:        log,
		}

		rt.Cron.Register(cron.Job{
			Name:     "signing-key-expiry",
			Schedule: cron.EveryInterval(6 * time.Hour),
			Run:      keys.Run,
		})
	}

	if r.worker {
		// Partition maintenance runs before the retention job in the
		// day, and far more often than it strictly needs to. Creating
		// a partition that already exists costs one catalog lookup,
		// while failing to create one costs every INSERT for that week
		// - so the cheap direction is to try hourly and let almost
		// every run be a no-op.
		parts := &partition.Maintainer{DB: db.DB(), Log: log}
		rt.Cron.Register(cron.Job{
			Name:     "partition-maintenance",
			Schedule: cron.EveryInterval(time.Hour),
			Run: func(ctx context.Context) error {
				_, err := parts.EnsureAhead(ctx)

				return err
			},
		})

		// Run it once at boot too. A node starting after a long outage
		// must not wait an hour to find out it has nowhere to write.
		if _, err := parts.EnsureAhead(ctx); err != nil {
			log.Error("partition maintenance failed at startup", "err", err)
		}

		sweeper := &retention.Sweeper{
			Store: st, Settings: rt.Settings, Blob: rt.Blob, Log: log,
			Partitions: parts,
		}

		rt.Cron.Register(cron.Job{
			Name:     "retention-cleanup",
			Schedule: cron.DailyAt(3, 0),
			Run:      sweeper.Run,
		})

		// The partition ceiling, which only an installation keeping
		// everything forever can reach - and which it reaches without
		// anything going wrong until delivery stops. Daily rather than
		// hourly: the count moves by one a day at most, and the mail
		// collapses to one a day anyway.
		//
		// PlatformAdmins rather than a list built here: who hears about
		// an installation-wide condition is already answered once, in
		// the store the alert path uses.
		rt.Cron.Register(cron.Job{
			Name:     "partition-ceiling",
			Schedule: cron.DailyAt(3, 30),
			Run: (&partitionalert.Checker{
				Counter: parts,
				Mail:    rt.SystemMail,
				Admins:  st.AlertRecipients.PlatformAdmins,
				Log:     log,
			}).Run,
		})

		// The trend rollup. The window is the widest range the chart
		// offers plus a day, so a bar is at most one run stale.
		// Worker-only like the other sweeps - every node running it
		// would be the same scan several times for one answer.
		rt.Cron.Register(cron.Job{
			Name:     "email-stats",
			Schedule: cron.EveryInterval(10 * time.Minute),
			Run: func(ctx context.Context) error {
				return st.Analytics.RecomputeDaily(ctx, statsRollupDays)
			},
		})

		// Bounce rate is a property of a window, so it is judged on a
		// timer rather than from the delivery path - see the package
		// comment. Fifteen minutes is frequent enough to catch a bad
		// campaign early and rare enough that the sweep is free.
		bounceAlerter := &notify.BounceAlerter{
			Store: st, Settings: rt.Settings, Raiser: rt.Notify, Log: log,
		}

		rt.Cron.Register(cron.Job{
			Name:     "bounce-alert",
			Schedule: cron.EveryInterval(15 * time.Minute),
			Run:      bounceAlerter.Run,
		})
	}
}
