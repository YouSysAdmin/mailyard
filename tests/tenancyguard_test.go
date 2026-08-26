// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/internal/database/dbtest"
)

// "Every tenant resource has project_id and every store query scopes
// on it first. Cross-project access must look like a missing
// resource." That is the first rule in the house style and the one
// whose failure is worst - a leak between customers - and until this
// file existed nothing checked it. It rested on the same thing the
// placeholder rule rested on before TestNoDynamicSQL: a reviewer
// noticing.
//
// The check reads the SCHEMA for which tables carry project_id, and
// the evaluated query text (see schemaguard_test.go) for whether a
// query touching one of them names the column at all. Naming it is a
// weak test - it cannot tell where the predicate sits - but the
// failure being guarded against is a query that forgot tenancy
// entirely, and that one it catches exactly.
//
// Skips without MAILYARD_TEST_DSN, since the list of scoped tables
// comes from a migrated database rather than a hand-kept copy.
func TestEveryTenantQueryNamesTheProject(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)

	rows, err := db.QueryContext(t.Context(), `
        SELECT table_name FROM information_schema.columns
        WHERE table_schema = current_schema() AND column_name = 'project_id'`)
	if err != nil {
		t.Fatalf("read the scoped tables: %v", err)
	}

	defer func() { _ = rows.Close() }()
	scoped := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}

		scoped[name] = true
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("read the scoped tables: %v", err)
	}

	if len(scoped) < 20 {
		t.Fatalf("only %d tables carry project_id - the migrations did not apply", len(scoped))
	}

	var missed []string
	var findings []string
	used := map[string]bool{}

	for _, q := range collectQueries(t, &missed) {
		var hit []string
		for _, m := range tableRef.FindAllStringSubmatch(q.sql, -1) {
			if name := strings.ToLower(m[1]); scoped[name] {
				hit = append(hit, name)
			}
		}

		if len(hit) == 0 || strings.Contains(strings.ToLower(q.sql), "project_id") {
			continue
		}

		key := siteKey(q.where)
		if _, ok := crossProjectByDesign[key]; ok {
			used[key] = true
			continue
		}

		slices.Sort(hit)
		findings = append(findings, key+" touches "+strings.Join(dedupe(hit), ", ")+
			" with no project_id anywhere in the statement\n        "+
			clip(oneLine(q.sql), 160))
	}

	if len(findings) > 0 {
		t.Errorf("%d quer(ies) read or write tenant data without naming the project:\n  %s\n\n"+
			"Scope it, or - if it genuinely runs installation-wide - add it to\n"+
			"crossProjectByDesign with the reason it is safe.",
			len(findings), strings.Join(findings, "\n  "))
	}

	// An exception nobody needs any more is an exception nobody
	// re-reads. The list is small enough to keep honest.
	var stale []string
	for key := range crossProjectByDesign {
		if !used[key] {
			stale = append(stale, key)
		}
	}

	slices.Sort(stale)
	if len(stale) > 0 {
		t.Errorf("crossProjectByDesign lists %d entr(ies) that no longer match any query:\n  %s",
			len(stale), strings.Join(stale, "\n  "))
	}
}

// tableRef finds the tables a statement names. Crude on purpose: it
// over-matches rather than under-matches, and an extra name simply
// means one more query gets asked the tenancy question.
var tableRef = regexp.MustCompile(`(?is)(?:\bFROM\s+|\bJOIN\s+|\bINTO\s+|\bUPDATE\s+)([a-z_][a-z0-9_]*)`)

// crossProjectByDesign is every query that touches a project_id table
// without scoping on it, and why that is correct. Keyed file:function,
// not by line, so ordinary edits do not churn it.
//
// A list rather than a marker at each call, following
// undocumentedConsoleRoutes: these cluster into four reasons, and
// reading them together is what makes a fifth reason look wrong.
// Every entry below was checked against its callers, not assumed.
var crossProjectByDesign = map[string]string{
	// 1. Retention and maintenance. They run on the worker with no
	// request behind them and are installation-wide by definition -
	// scoping them would mean sweeping one project and leaving the
	// rest to grow.
	"internal/domain/audit/store.go PurgeOlderThan":             "retention sweep, installation-wide",
	"internal/domain/email/store.go PurgeOlderThan":             "retention sweep",
	"internal/domain/email/store.go PruneVolumeBefore":          "retention sweep, drops volume counters no window can read",
	"internal/domain/analytics/rollup.go RecomputeDaily":        "maintenance sweep, rebuilds the trend rollup for every project in one scan",
	"internal/domain/email/store.go ClearBodiesOlderThan":       "retention sweep",
	"internal/domain/email/store.go ClearAttachmentsOlderThan":  "retention sweep",
	"internal/domain/email/store.go StorageKeysOlderThan":       "retention sweep, feeds the blob delete",
	"internal/domain/inbound/store.go PurgeOlderThan":           "retention sweep",
	"internal/domain/inbound/store.go ClearContentOlderThan":    "retention sweep",
	"internal/domain/inbound/store.go StorageKeysOlderThan":     "retention sweep, feeds the blob delete",
	"internal/domain/notification/store.go PurgeOlderThan":      "retention sweep",
	"internal/domain/webhook/store.go PurgeDeliveriesOlderThan": "retention sweep",
	"internal/domain/sandbox/store.go PurgeExpired":             "retention sweep, expiry is stamped per capture",
	"internal/core/partition/partition.go EnsureAhead":          "counts the backstop partition, no rows are read",
	"internal/domain/email/store.go RecoverStuck":               "requeues rows a dead worker left claimed, fleet-wide",
	"internal/domain/email/store.go CountAllByStatus":           "the Prometheus gauge, which is an installation metric",
	"internal/core/sestopics/sestopics.go load":                 "the SES topic allowlist is platform configuration",

	// 2. The campaign runner. It claims work across the installation
	// exactly as the email queue does, and the project comes off the
	// row it claimed.
	"internal/domain/campaign/store.go PromoteScheduled": "runner, promotes due campaigns fleet-wide",
	"internal/domain/campaign/store.go ClaimDue":         "runner claim, the project comes off the claimed row",
	"internal/domain/campaign/store.go SetRunState":      "runner-owned columns on a campaign it just claimed",

	// 3. Writes addressed by an id whose tenancy was already decided.
	// The worker holds the claimed row, the credential was resolved by
	// its own secret, and the handler compared the project before
	// calling - re-scoping here would re-ask a question already
	// answered, and for the partitioned emails table it would also
	// mean an id-only lookup across every live partition.
	"internal/domain/email/store.go Requeue":                "the worker holds the claimed row, pruned by created_at",
	"internal/domain/email/store.go Finalize":               "the worker holds the claimed row, pruned by created_at",
	"internal/domain/apikey/apikey.go TouchLastUsed":        "the key was just resolved by its own hash",
	"internal/domain/smtpcredential/store.go TouchLastUsed": "the credential was just resolved by its own hash",
	"internal/domain/relaynode/store.go Heartbeat":          "the node proved the id with its enrolment token",
	"internal/domain/relaynode/store.go TouchInbound":       "same node, same token, and a platform node has no project to scope to",
	"internal/domain/relaynode/store.go Delete":             "projectNode compares node.ProjectID to the caller first",

	// 4. Tracking. Every one of these is reached from a public
	// endpoint carrying a signed link or an opaque email id, so there
	// is no caller project to scope to - the id IS the authorization,
	// and the row's own project is what the write belongs to.
	"internal/domain/email/store.go MarkOpened":             "the open pixel, addressed by the email id in a signed link",
	"internal/domain/email/store.go MarkClicked":            "the click redirect, addressed the same way",
	"internal/domain/campaign/store.go IncrementLinkClicks": "the tallied link was resolved from the signed redirect",

	// 5. The shared pool, which has no project by construction. These
	// are flagged only because they LEFT JOIN relay_nodes for the
	// liveness rule, and that join is on a unique server_id.
	"internal/domain/smtpserver/shared_store.go Get":         "shared servers are platform-owned, the join is on a unique server_id",
	"internal/domain/smtpserver/shared_store.go List":        "shared servers are platform-owned",
	"internal/domain/smtpserver/shared_store.go ListEnabled": "shared servers are platform-owned",

	// 6. The accept list a relay node caches. Installation-wide by
	// definition - a node answers RCPT for whatever this installation
	// receives mail for, and it is handed names, never the projects
	// behind them. Tenancy is decided when the message arrives, by
	// GetVerifiedCovering, exactly as it is for the local MX.
	"internal/domain/domains/domains.go VerifiedNames": "the relay node accept list, names only, no project is disclosed",
}

// siteKey turns "path:line Func" into "path Func".
func siteKey(where string) string {
	path, rest, ok := strings.Cut(where, ":")
	if !ok {
		return where
	}

	_, fn, ok := strings.Cut(rest, " ")
	if !ok {
		return where
	}

	return path + " " + fn
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}

	return out
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return s[:n] + " ..."
}
