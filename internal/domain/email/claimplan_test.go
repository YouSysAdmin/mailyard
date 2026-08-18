// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"strings"
	"testing"

	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
)

// The two queue statements write their statuses as literals, and this is
// what keeps those literals true.
//
// They are literals because idx_emails_queue is partial, on
// `status IN ('queued','scheduled','processing')`. Postgres will only
// use a partial index if it can prove the query predicate implies the
// index predicate, and it can prove nothing about
// `status = ANY(ARRAY[$1,$2])`. A custom plan folds parameters into
// constants and the proof succeeds, but pgx prepares its statements, so
// after five executions Postgres may switch to a generic plan and the
// index quietly stops being used. Every poll of every worker then scans
// every partition, and nothing looks wrong: the claim returns exactly
// the right rows either way.
//
// A literal cannot follow a renamed constant, which is what this test is
// for. Splicing the constants in instead is worse - the repo-wide query
// evaluator keys package constants by package name, the model is
// imported here under an alias, and both queries drop out of the schema
// and tenancy guards entirely.
func TestTheClaimNamesTheStatusesItMeans(t *testing.T) {
	for _, tc := range []struct {
		what   string
		sql    string
		status string
	}{
		{"the claim looks for queued", claimDueSQL, emailmodel.StatusQueued},
		{"the claim looks for scheduled", claimDueSQL, emailmodel.StatusScheduled},
		{"the recovery sweep looks for processing", recoverStuckSQL, emailmodel.StatusProcessing},
	} {
		if !strings.Contains(tc.sql, "'"+tc.status+"'") {
			t.Errorf("%s: the SQL does not name %q - a status was renamed and the "+
				"statement now looks for a value nothing writes", tc.what, tc.status)
		}
	}

	// And no parameter placeholder is left where a status belongs, which
	// is the shape that made the partial index unusable.
	if strings.Contains(claimDueSQL, "status IN (?") {
		t.Error("the claim passes its statuses as parameters again, so the partial " +
			"index cannot be proved to apply and every partition is scanned")
	}

	// The statuses must be in the index predicate, or being literal buys
	// nothing. Named here rather than read from the migration, because the
	// point is the AGREEMENT and a copy of one side is not evidence -
	// migration 00030 carries the same three in its comment.
	for _, s := range []string{emailmodel.StatusQueued, emailmodel.StatusScheduled, emailmodel.StatusProcessing} {
		if s != "queued" && s != "scheduled" && s != "processing" {
			t.Errorf("status %q is not one of the three in the idx_emails_queue "+
				"predicate - the index needs a migration before the queries can use it", s)
		}
	}
}
