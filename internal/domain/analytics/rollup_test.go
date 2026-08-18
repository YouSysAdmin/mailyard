// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package analytics

import (
	"context"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
)

// The rollup has to say what counting the rows says.
//
// This is the guard the whole design rests on. A rollup is only worth
// having if it cannot come to mean something different from the query it
// replaced - so this counts the rows directly, recomputes, reads the
// rollup, and compares. Anything that changes one and not the other
// (bucketing, the timezone the day is cut on, a status filter) fails here
// rather than as a chart that quietly disagrees with the email log.
//
// It also covers what recomputation is for: a row whose status changes
// after the fact. An incremented counter needs a decrement at every such
// transition and drifts when one is missed. This one just runs again.
func TestTheRollupAgreesWithCountingTheRows(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	store := NewStore(db)
	ctx := context.Background()

	projID := ids.New()
	if _, err := db.ExecContext(ctx, `
        INSERT INTO projects (id, name, slug, owner_id, default_language, created_at)
        VALUES ($1, 'rollup', 'rollup', $2, 'en', now())`, projID, ids.New()); err != nil {
		t.Fatalf("project: %v", err)
	}

	// Mail across three days and three statuses, including today.
	now := time.Now().UTC()
	plant := func(status string, daysAgo int, n int) {
		for range n {
			if _, err := db.ExecContext(ctx, `
                INSERT INTO emails (id, project_id, sender, recipients, subject, status, created_at)
                VALUES ($1, $2, 'a@b.test', '["c@d.test"]', 's', $3, $4)`,
				ids.New(), projID, status, now.AddDate(0, 0, -daysAgo)); err != nil {
				t.Fatalf("plant: %v", err)
			}
		}
	}
	plant("sent", 0, 5)
	plant("failed", 0, 2)
	plant("sent", 1, 3)
	plant("queued", 2, 4)

	// live counts the rows the way the chart used to.
	live := func(status string) map[string]int {
		q := `
            SELECT to_char((created_at AT TIME ZONE 'UTC')::date, 'YYYY-MM-DD'), COUNT(*)
            FROM emails WHERE project_id = $1`
		args := []any{projID}
		if status != "" {
			q += ` AND status = $2`
			args = append(args, status)
		}

		q += ` GROUP BY 1`
		rows, err := db.QueryContext(ctx, q, args...)
		if err != nil {
			t.Fatalf("live count: %v", err)
		}

		defer func() { _ = rows.Close() }()
		out := map[string]int{}
		for rows.Next() {
			var day string
			var n int
			if err := rows.Scan(&day, &n); err != nil {
				t.Fatalf("scan: %v", err)
			}

			out[day] = n
		}

		// The whole point of this test is that the rollup agrees with
		// counting the rows, so a truncated iteration must fail rather
		// than quietly become the number being agreed with.
		if err := rows.Err(); err != nil {
			t.Fatalf("live count: %v", err)
		}

		return out
	}

	check := func(when string) {
		if err := store.RecomputeDaily(ctx, 15); err != nil {
			t.Fatalf("%s: recompute: %v", when, err)
		}

		// The same convention the handler uses: an exclusive upper bound
		// at tomorrow midnight, meaning "through today".
		midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		from, to := midnight.AddDate(0, 0, -14), midnight.AddDate(0, 0, 1)
		for _, status := range []string{"", "sent", "failed", "queued"} {
			want := live(status)
			got, err := store.DailyCounts(ctx, projID, from, to, status)
			if err != nil {
				t.Fatalf("%s: daily counts %q: %v", when, status, err)
			}

			// DailyCounts fills empty days with zero, so compare only the
			// days the rows are actually on.
			have := map[string]int{}
			for _, d := range got {
				if d.Count > 0 {
					have[d.Date] = d.Count
				}
			}

			if len(have) != len(want) {
				t.Errorf("%s: status %q: rollup has %d non-empty days, counting the rows gives %d\n  rollup: %v\n  rows:   %v",
					when, status, len(have), len(want), have, want)
				continue
			}

			for day, n := range want {
				if have[day] != n {
					t.Errorf("%s: status %q day %s: rollup says %d, counting the rows says %d",
						when, status, day, have[day], n)
				}
			}
		}
	}

	check("as planted")

	// Now change an outcome after the fact - a retry that succeeded. This
	// is the case an incremented counter has to decrement for and this one
	// does not.
	if _, err := db.ExecContext(ctx, `
        UPDATE emails SET status = 'sent' WHERE project_id = $1 AND status = 'failed'`,
		projID); err != nil {
		t.Fatalf("retry: %v", err)
	}

	check("after a status changed")
}
