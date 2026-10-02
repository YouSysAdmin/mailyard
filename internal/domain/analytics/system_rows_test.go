// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package analytics_test

import (
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain/analytics"
)

// A system row - platform mail through this project - is not in the
// project's numbers: not the dashboard counts, not the breakdown, and
// (rollup_test) not the daily trend.
func TestSystemRowsAreNotCounted(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	store := analytics.NewStore(db)
	ctx := t.Context()

	projID := ids.New()
	if _, err := db.ExecContext(ctx, `
        INSERT INTO projects (id, name, slug, owner_id, default_language, created_at)
        VALUES ($1, 'sys', 'sys', $2, 'en', now())`, projID, ids.New()); err != nil {
		t.Fatalf("project: %v", err)
	}

	for _, system := range []bool{false, true} {
		if _, err := db.ExecContext(ctx, `
            INSERT INTO emails (id, project_id, sender, recipients, subject, status, tracked, created_at, system)
            VALUES ($1, $2, 'a@b.test', '["c@d.test"]', 's', 'sent', true, now(), $3)`,
			ids.New(), projID, system); err != nil {
			t.Fatalf("plant: %v", err)
		}
	}

	sum, err := store.Summary(ctx, projID)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}

	if sum.Emails["sent"] != 1 || sum.TotalEmails != 1 {
		t.Errorf("Summary counted sent=%d total=%d, want 1 and 1", sum.Emails["sent"], sum.TotalEmails)
	}

	if sum.Engagement.TrackedSent != 1 {
		t.Errorf("engagement denominator = %d, want 1", sum.Engagement.TrackedSent)
	}

	now := time.Now().UTC()
	br, err := store.StatusBreakdown(ctx, projID, now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil || br["sent"] != 1 {
		t.Errorf("StatusBreakdown = %v, %v, want sent=1", br, err)
	}
}
