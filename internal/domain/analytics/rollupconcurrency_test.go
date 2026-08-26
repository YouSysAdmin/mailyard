// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package analytics

import (
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
)

// Every node runs every job, so two of them recompute at once.
//
// Each run DELETEs the window and INSERTs the aggregate. A DELETE takes
// its snapshot when the statement starts, so the second run removed rows
// the first had already replaced and then hit the primary key putting
// them back: the job logged an error, the transaction rolled back, and
// two nodes could deadlock on the same rows. Nothing about the chart said
// so - it was simply not being rebuilt.
//
// A transaction-scoped advisory lock decides it, and the loser SKIPS
// rather than waiting: it would recompute the same numbers from the same
// source, so there is nothing to defer.
//
// dbtest.Peer is what makes this visible at all - dbtest.Open pins itself
// to one connection, which makes concurrency invisible.
func TestTwoNodesRecomputingAtOnceDoNotFail(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	peer := dbtest.Peer(t)
	first := NewStore(db)
	second := NewStore(peer)
	ctx := t.Context()

	projID := ids.New()
	if _, err := db.ExecContext(ctx, `
        INSERT INTO projects (id, name, slug, owner_id, default_language, created_at)
        VALUES ($1, 'rollup', 'rollup', $2, 'en', now())`, projID, ids.New()); err != nil {
		t.Fatalf("project: %v", err)
	}

	now := time.Now().UTC()
	for i := range 6 {
		if _, err := db.ExecContext(ctx, `
            INSERT INTO emails (id, project_id, sender, recipients, subject, status, created_at)
            VALUES ($1, $2, 'a@b.test', '["c@d.test"]', 's', 'sent', $3)`,
			ids.New(), projID, now.AddDate(0, 0, -(i%3))); err != nil {
			t.Fatalf("plant: %v", err)
		}
	}

	// Both at once, twice over, so the loser is sometimes each of them.
	for round := range 2 {
		errs := make(chan error, 2)
		go func() { errs <- first.RecomputeDaily(ctx, 15) }()
		go func() { errs <- second.RecomputeDaily(ctx, 15) }()
		for range 2 {
			if err := <-errs; err != nil {
				t.Fatalf("round %d: a concurrent recompute failed: %v", round, err)
			}
		}
	}

	// And the answer is still the right one, not a doubled or missing
	// window - two runs of the same GROUP BY over the same rows.
	var total int
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(n), 0) FROM email_daily WHERE project_id = $1`, projID).Scan(&total); err != nil {
		t.Fatalf("read the rollup: %v", err)
	}

	if total != 6 {
		t.Errorf("rollup totals %d, want 6 - a concurrent run doubled or dropped the window", total)
	}

	var days int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM email_daily WHERE project_id = $1`, projID).Scan(&days); err != nil {
		t.Fatalf("count rows: %v", err)
	}

	if days != 3 {
		t.Errorf("%d rollup rows, want one per day per status (3)", days)
	}
}
