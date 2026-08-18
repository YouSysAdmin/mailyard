// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package notify

import (
	"context"
	"fmt"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/quota"
	nmodel "github.com/yousysadmin/mailyard/internal/models/notification"
)

// warnAt is the fraction of a plan's window that raises a warning.
//
// Eighty percent, because the point of the warning is to arrive while
// there is still time to do something - raise the plan, spread the
// batch - and a warning at 99% is a slower way of saying refused.
const warnAt = 80

// QuotaWorthRaising reports whether an observation would raise
// anything at all.
//
// Exported so a caller can ask BEFORE filing the work. The observer
// below is called on every accepted message and answers "nothing to
// do" for almost all of them, so a caller that hands each one to a
// goroutine first pays for the goroutine on every send to learn that.
// Three integer operations answer it instead.
//
// The observer keeps its own copy of these guards. It has to stay
// correct when called directly, and this is an optimization in front
// of it rather than a precondition on it.
func QuotaWorthRaising(used, limit int) bool {
	return limit > 0 && used*100/limit >= warnAt
}

// QuotaObserver turns what a volume check saw into a notification.
//
// nmodel.TypeQuota was DECLARED AND NEVER RAISED: the plan refused the
// send with a 429 or a 452 and nobody was told anything. An operator
// found out from their own integration's logs, or by opening the project
// settings page and reading the usage card - if they thought to.
//
// Two notifications, not one, because they mean different things and
// want different urgency:
//
//   - a warning at 80% of the window, which is news while it can still
//     be acted on
//   - the limit itself, which is mail not going out right now.
//
// Deduped per project, per window, per HOUR, so a script hammering a
// full quota files one notification rather than one per attempt. The
// hour is in the key rather than a timer, which is what the bounce-rate
// check does and for the same reason.
func (r *Raiser) QuotaObserver(ctx context.Context, projID string) quota.Observer {
	return func(window string, used, limit int, plan string) {
		if limit <= 0 {
			return
		}

		pct := used * 100 / limit
		if pct < warnAt {
			return
		}

		hour := time.Now().UTC().Format("2006-01-02T15")
		per := "per " + window
		if used >= limit {
			r.Raise(ctx, &nmodel.Notification{
				ProjectID: projID,
				Type:      nmodel.TypeQuota,
				Severity:  nmodel.SeverityError,
				Title:     fmt.Sprintf("Sending limit reached (%d %s)", limit, per),
				Body: fmt.Sprintf(
					"Plan %q allows %d emails %s and %d have been accepted in this window, so "+
						"further sends are refused until it rolls - the API answers 429 and SMTP "+
						"submission answers a temporary 452, so a sending client will retry rather "+
						"than lose the message. Raise the plan or spread the load.",
					plan, limit, per, used),
				Link:      "/usage",
				DedupeKey: "quota_reached:" + window + ":" + hour,
			})

			return
		}

		r.Raise(ctx, &nmodel.Notification{
			ProjectID: projID,
			Type:      nmodel.TypeQuota,
			Severity:  nmodel.SeverityWarning,
			Title:     fmt.Sprintf("Sending is at %d%% of the limit (%d of %d %s)", pct, used, limit, per),
			Body: fmt.Sprintf(
				"Plan %q allows %d emails %s. Sends are refused once the window is full - the "+
					"API with 429 and SMTP submission with a temporary 452.",
				plan, limit, per),
			Link:      "/usage",
			DedupeKey: "quota_warn:" + window + ":" + hour,
		})
	}
}
