// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package analytics_test

import (
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain/analytics"
)

// The dashboard read, run against the real schema.
//
// This package had no store test at all, and it is the one place the
// repository-wide schema guard cannot reach: Summary counts fifteen
// tables whose names come out of a map literal, so the query text is
// assembled at runtime and there is no constant to evaluate. A
// misspelt table or a column that moved is therefore invisible to
// every static check, and shows up as a 500 on the first page anyone
// opens after signing in.
//
// So the check is to run it. Nothing is inserted - zero rows exercise
// every statement just as well, and this is asking whether the SQL is
// answerable, not whether arithmetic works.
func TestTheDashboardReadsRunAgainstTheRealSchema(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	store := analytics.NewStore(db)

	const projID = "00000000-0000-4000-8000-000000000000"

	summary, err := store.Summary(t.Context(), projID)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}

	// Every key in the map has to come back, or a count was silently
	// skipped rather than failing.
	for _, key := range []string{
		"domains", "verified_domains", "smtp_servers", "api_keys",
		"active_api_keys", "smtp_credentials", "senders", "templates",
		"contacts", "subscribers", "suppressions", "bounces", "webhooks",
		"campaigns", "unsubscribe_lists",
	} {
		if _, ok := summary.Resources[key]; !ok {
			t.Errorf("Summary did not report %q", key)
		}
	}

	to := time.Now().UTC()
	from := to.AddDate(0, 0, -7)

	days, err := store.DailyCounts(t.Context(), projID, from, to, "")
	if err != nil {
		t.Fatalf("DailyCounts: %v", err)
	}

	// EIGHT, not seven: from is an instant seven days ago and to is an
	// instant today, so the range touches eight calendar days - the first
	// one partially and the last one partially. The fill walks dates now
	// rather than 24 hour steps, because the query buckets by date: the
	// old loop stopped before `to` and dropped the last day's rows from
	// the chart while counting them in the query.
	//
	// The handler is unaffected - it passes midnight to tomorrow-midnight,
	// where both readings give the same points.
	if len(days) != 8 {
		t.Errorf("DailyCounts filled %d days, want 8 - a point per calendar day the range\n"+
			"touches, or the chart rescales its axis", len(days))
	}

	if _, err := store.DailyCounts(t.Context(), projID, from, to, "sent"); err != nil {
		t.Fatalf("DailyCounts with a status filter: %v", err)
	}

	if _, err := store.StatusBreakdown(t.Context(), projID, from, to); err != nil {
		t.Fatalf("StatusBreakdown: %v", err)
	}
}
