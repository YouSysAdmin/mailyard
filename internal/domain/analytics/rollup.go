// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package analytics

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// RecomputeDaily rebuilds the daily rollup for every project over the
// last `days` days.
//
// A RECOMPUTATION, not an increment: these are outcomes, and queued
// becoming sent or a retry turning failed back into sent is a
// transition a counter would have to get right at every step. Running
// the same GROUP BY the chart ran means the rollup cannot come to mean
// something else. Compare email_volume, which only goes up.
//
// One transaction, so a dashboard reading mid-run sees the previous
// numbers rather than a half-empty window, and every project in one
// scan.
//
// WHAT IT DOES NOT COVER: a retry of a message older than the window
// changes that day's outcome and nothing recomputes it, so a
// historical bar can lag. The tiles are counted live, so no total is
// ever wrong.
func (s *Store) RecomputeDaily(ctx context.Context, days int) error {
	if days <= 0 {
		return fmt.Errorf("recompute window must be positive, got %d", days)
	}

	since := time.Now().UTC().AddDate(0, 0, -days)

	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// ONE NODE AT A TIME, and the rest skip rather than queue.
	//
	// Every node runs every job it registers - there is no leader
	// election - so on a three node installation this fired three times a
	// minute apart or, on a restart, together. Two runs then raced: each
	// DELETEs the window and INSERTs the aggregate, and because a DELETE
	// takes its snapshot when the statement starts, the second one removed
	// rows the first had already replaced and then hit the primary key on
	// re-inserting them. The job logged an error, the transaction rolled
	// back, and two nodes could deadlock on the same rows.
	//
	// A transaction-scoped advisory lock, so it is released on commit or
	// rollback with nothing to clean up, and TRY rather than wait: this is
	// a recomputation of the same numbers from the same source, so a
	// second node doing it a moment later adds nothing. Skipping is the
	// correct outcome, not a deferred one.
	//
	// The key is an arbitrary constant that only has to be unique among
	// this installation's advisory locks. It is the only one.
	var mine bool
	if err := tx.QueryRowContext(ctx,
		//sqlconst:allow a fixed lock key, no runtime data
		`SELECT pg_try_advisory_xact_lock(4021001)`).Scan(&mine); err != nil {
		return fmt.Errorf("take the rollup lock: %w", err)
	}

	if !mine {
		return nil
	}

	// never clear a day this cannot rebuild.
	//
	// The rollup is meant to outlive the rows it was built from: the
	// chart offers fourteen days, and an operator may keep far less email
	// log than that. But retention purges `emails` on its own window, so
	// a DELETE over the whole recompute span would drop every day the
	// source no longer covers and then fail to put it back, with nothing
	// left to count. With a seven day retention and a fifteen day window
	// that takes the rollup from twenty days to twelve on the first run,
	// and another eight on every run after. Nothing errors - the chart
	// just trails off to zero.
	//
	// So the floor is the oldest row the source still holds. Everything
	// below it is history that this table is now the only record of.
	var oldest sql.NullTime
	if err := tx.QueryRowContext(ctx, s.Q(`
        SELECT MIN(created_at) FROM emails WHERE created_at >= ?`), since).Scan(&oldest); err != nil {
		return fmt.Errorf("find the oldest row in the window: %w", err)
	}

	// Nothing in the window at all. Clearing it would delete rollup
	// rows and insert none, so leave it entirely alone - an
	// installation that has not sent for a fortnight must not lose the
	// fortnight before that.
	if !oldest.Valid {
		if err := tx.Commit(); err != nil {
			return err
		}

		committed = true

		return nil
	}

	floor := since
	if oldest.Time.After(floor) {
		floor = oldest.Time
	}

	if _, err := tx.ExecContext(ctx, s.Q(`
        DELETE FROM email_daily WHERE day >= (?::timestamptz AT TIME ZONE 'UTC')::date`), floor); err != nil {
		return fmt.Errorf("clear the window: %w", err)
	}

	if _, err := tx.ExecContext(ctx, s.Q(`
        INSERT INTO email_daily (project_id, day, status, n)
        -- ON CONFLICT as well as the DELETE above, so a run that reaches
        -- here without the lock - a hand run of the job, a future caller -
        -- lands on the same numbers instead of failing on the key. The
        -- values come from one GROUP BY over one source, so writing them
        -- twice cannot mean two different things.
        SELECT project_id, (created_at AT TIME ZONE 'UTC')::date, status, COUNT(*)
        FROM emails
        WHERE created_at >= ?
        GROUP BY project_id, (created_at AT TIME ZONE 'UTC')::date, status
        ON CONFLICT (project_id, day, status) DO UPDATE SET n = EXCLUDED.n`), since); err != nil {
		return fmt.Errorf("recompute the window: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	committed = true

	return nil
}
