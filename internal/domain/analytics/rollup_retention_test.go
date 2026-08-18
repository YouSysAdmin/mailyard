// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package analytics

import (
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
)

// The rollup outlives the rows it was built from, and that is the
// point of it: the chart offers fourteen days, and an operator keeping
// only seven days of email log should still see fourteen days of
// trend.
//
// But RecomputeDaily DELETEs its whole window and rebuilds from the
// emails table. Any day inside the window that retention has already
// purged is therefore deleted and not put back - and since the source
// rows are gone too, it can never come back. Every run of the
// email-stats job eats another slice of history, silently, and the
// only symptom is a chart that trails off to zero.
//
// So the window that gets cleared must never be wider than the window
// the source still covers.
func TestTheRollupSurvivesANarrowerRetentionWindow(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	store := NewStore(db)
	ctx := t.Context()

	projID := ids.New()
	if _, err := db.ExecContext(ctx, `
        INSERT INTO projects (id, name, slug, owner_id, default_language, created_at)
        VALUES ($1, 'rollup', 'rollup', $2, 'en', now())`, projID, ids.New()); err != nil {
		t.Fatalf("project: %v", err)
	}

	now := time.Now().UTC()

	// Twenty days of sending, one message a day.
	for d := range 20 {
		if _, err := db.ExecContext(ctx, `
            INSERT INTO emails (id, project_id, sender, recipients, subject, status, created_at)
            VALUES ($1, $2, 'a@b.test', '["c@d.test"]', 's', 'sent', $3)`,
			ids.New(), projID, now.AddDate(0, 0, -d)); err != nil {
			t.Fatalf("plant: %v", err)
		}
	}

	// Build the rollup while every row is still there. This is the
	// history the chart is meant to keep.
	if err := store.RecomputeDaily(ctx, 20); err != nil {
		t.Fatalf("initial recompute: %v", err)
	}

	before := countDays(t, store)
	if before != 20 {
		t.Fatalf("the rollup holds %d days, want 20 before anything is purged", before)
	}

	// Retention runs with a SEVEN day window, which an operator is
	// free to set - the setting takes any non-negative number and
	// defaults to 30.
	if _, err := db.ExecContext(ctx, `
        DELETE FROM emails WHERE created_at < $1`, now.AddDate(0, 0, -7)); err != nil {
		t.Fatalf("purge: %v", err)
	}

	// Now the stats job runs on its ten minute tick, with the
	// fifteen-day window the caller passes.
	if err := store.RecomputeDaily(ctx, 15); err != nil {
		t.Fatalf("recompute after purge: %v", err)
	}

	after := countDays(t, store)

	// Days 8 to 14 were inside the cleared window and outside what the
	// source still holds. They must not have been thrown away.
	if after < before {
		t.Errorf("the rollup lost %d days of history to a recompute it could not rebuild (%d -> %d)",
			before-after, before, after)
	}
}

func countDays(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.QueryRow(t.Context(), `SELECT COUNT(DISTINCT day) FROM email_daily`).Scan(&n); err != nil {
		t.Fatalf("count days: %v", err)
	}

	return n
}

// An installation that has not sent for longer than the recompute
// window must not lose the history from before it went quiet. This is
// the branch where the source has nothing in the window at all, which
// would otherwise clear the window and insert nothing.
func TestAQuietInstallationKeepsItsHistory(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	store := NewStore(db)
	ctx := t.Context()

	projID := ids.New()
	if _, err := db.ExecContext(ctx, `
        INSERT INTO projects (id, name, slug, owner_id, default_language, created_at)
        VALUES ($1, 'quiet', 'quiet', $2, 'en', now())`, projID, ids.New()); err != nil {
		t.Fatalf("project: %v", err)
	}

	now := time.Now().UTC()

	// Sending that stopped a month ago.
	for d := 30; d < 35; d++ {
		if _, err := db.ExecContext(ctx, `
            INSERT INTO emails (id, project_id, sender, recipients, subject, status, created_at)
            VALUES ($1, $2, 'a@b.test', '["c@d.test"]', 's', 'sent', $3)`,
			ids.New(), projID, now.AddDate(0, 0, -d)); err != nil {
			t.Fatalf("plant: %v", err)
		}
	}

	if err := store.RecomputeDaily(ctx, 40); err != nil {
		t.Fatalf("initial recompute: %v", err)
	}

	before := countDays(t, store)
	if before != 5 {
		t.Fatalf("the rollup holds %d days, want 5", before)
	}

	// The stats job keeps running on its tick with the 15 day window,
	// which now contains nothing at all.
	for range 3 {
		if err := store.RecomputeDaily(ctx, 15); err != nil {
			t.Fatalf("recompute: %v", err)
		}
	}

	if after := countDays(t, store); after != before {
		t.Errorf("a quiet fortnight cost the rollup %d days (%d -> %d)", before-after, before, after)
	}
}
