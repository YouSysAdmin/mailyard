// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Go says "" where the database says NULL, and the translation lives
// at the two edges: database.NullStr writing, database.Str scanning.
// A nullable UUID column is where forgetting it stops being cosmetic -
// "" is not a uuid, so Postgres refuses the whole statement and the
// caller gets a 500 with "invalid input syntax for type uuid" and no
// hint about which value.
//
// It cost four live bugs before this test existed:
//
//   - suppressions.unsubscribe_list_id, on the read side, which meant
//     every ordinary send failed at the suppression check
//   - tracked_links.campaign_id, both sides, so click tracking on
//     transactional mail could not work at all
//   - bounces.email_id, on a route whose own validation says the id
//     is optional
//   - project_invitations.role_id, so inviting somebody without
//     naming a role answered 500
//
// Every one of them worked when the value happened to be present,
// which is why nothing caught them: the tests, the fixtures and the
// console all had a real id to hand.
//
// This reads the INSERT column list and the argument list positionally
// - they are one-to-one by construction here - and asks that a
// nullable uuid column be written through a wrapper.
func TestNullableUUIDColumnsAreWrittenAsNull(t *testing.T) {
	nullable := nullableUUIDColumns(t)

	var findings, unparsed []string
	checked := 0
	walkGoFiles(t, func(path, src string) {
		for _, ins := range insertsIn(src) {
			if len(ins.args) != len(ins.cols) {
				// Cannot be lined up. REPORTED, never skipped
				// silently - see the count below.
				unparsed = append(unparsed, path+":"+strconv.Itoa(ins.line)+"  "+ins.table+
					"  ("+strconv.Itoa(len(ins.args))+" args, "+strconv.Itoa(len(ins.cols))+" columns)")
				continue
			}

			checked++
			for i, col := range ins.cols {
				if !nullable[ins.table+"."+col] {
					continue
				}

				arg := ins.args[i]
				if strings.Contains(arg, "NullStr") || strings.Contains(arg, "nullPtr") ||
					strings.Contains(arg, "nil") {
					continue
				}

				findings = append(findings, path+":"+strconv.Itoa(ins.line)+
					"  "+ins.table+"."+col+" <- "+arg)
			}
		}
	})

	// A floor, because the first cut of this test lined up ZERO
	// statements and passed: an off-by-one made every INSERT
	// unparseable, and nothing said so.
	if checked < 40 {
		t.Fatalf("only %d INSERT(s) were checked - the parser has stopped "+
			"lining them up, so this test is passing vacuously", checked)
	}

	if len(unparsed) > 0 {
		sort.Strings(unparsed)
		t.Errorf("%d INSERT(s) this test cannot read, so nothing checks them:\n  %s\n\n"+
			"Either give the statement a flat one-placeholder-per-column VALUES with a\n"+
			"matching argument list, or teach the parser the shape.",
			len(unparsed), strings.Join(unparsed, "\n  "))
	}

	sort.Strings(findings)
	if len(findings) > 0 {
		t.Errorf("%d write(s) into a nullable uuid column that do not translate \"\" to NULL:\n  %s\n\n"+
			"Wrap the argument in database.NullStr. Postgres refuses \"\" as a uuid,\n"+
			"so the whole statement fails the first time the value is absent.",
			len(findings), strings.Join(findings, "\n  "))
	}
}

// The read side is deliberately not guarded statically here, and that
// is a measured decision rather than an omission.
//
// The rule would be "a nullable uuid column compared to a parameter
// must cast it or ask IS NOT DISTINCT FROM". Run against this tree it
// reports nineteen sites, eighteen of which pass an id that is always
// present - a campaign id inside campaign analytics, a plan id being
// deleted - and one of which is not a uuid at all
// (user_passkeys.credential_id is TEXT, and shares its name with
// sandbox_emails.credential_id, which is a uuid). Telling those apart
// needs the TABLE a one-line fragment belongs to, which is not there
// to be read.
//
// An allowlist of eighteen entries reading "this id is always set" is
// friction on every future query and evidence of nothing. What
// actually caught the two read-side bugs was a store test that took
// the empty path - TestAnUnscopedSendConsultsGlobalBlocksOnly and
// TestATransactionalTrackedLinkHasNoCampaign. Both worked with a real
// id, which is exactly why nothing else noticed.
//
// So: writes are checked mechanically below, reads are checked by
// exercising the case where the value is absent.
//
// A LATER AUDIT FOUND THE HARDER HALF, and it is worth naming because it
// is outside this test's model entirely: a NON-nullable uuid column
// compared to a parameter some caller passes empty. `id <> ?` on a
// primary key, where the empty id means "no exception". This test skips
// NOT NULL columns and primary keys by construction, and
// TestEveryQueryMatchesTheSchema PREPAREs without ever BINDING, so the
// statement looks perfect to both - it fails only on EXECUTE, with the
// same 22P02 that MalformedID then disguises as a 404.
//
// Two of the three worst bugs that audit found were this exact shape, so
// the same discipline is applied to it and the empty path is now exercised
// at every site of it:
//
//	TestSlugTakenWorksWithNoExceptionID          smtp server groups, every CREATE answered 404
//	TestMarkingAMessageWithNoEmailIDSucceeds     campaign_messages, campaigns never finished
//	TestRevokingOtherSessionsWithNoSessionToKeep sessions, a jti-less token could not sign out elsewhere
//	TestProviderSlugTakenWorksWithNoExceptionID  oauth providers, the same shape, not reachable today
//	TestALiftedListOptOutTakesOnlyThatList       suppressions, an empty list id means the global row
//
// Still no static rule, for the reason above: which parameters can be
// empty is a fact about callers, not about SQL. The seam that holds is a
// test per site that passes nothing where an id would go.

// nullableUUIDColumns reads the migrations, keyed table.column so a
// name that is TEXT in one table and UUID in another is not confused
// for the other - user_passkeys.credential_id is a WebAuthn
// credential and deliberately TEXT, while sandbox_emails.credential_id
// is a uuid.
//
// project_id is excluded: it is nullable in the baseline but every
// tenant row carries one, and including it would bury the columns this
// is about under thirty entries that never see an empty value.
func nullableUUIDColumns(t *testing.T) map[string]bool {
	t.Helper()
	dir := filepath.Join(repoRoot(t), "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}

	out := map[string]bool{}
	colDef := regexp.MustCompile(`^\s*([a-z_]+)\s+UUID\b(.*)$`)
	createTable := regexp.MustCompile(`(?i)^\s*CREATE TABLE (?:IF NOT EXISTS )?(\w+)`)
	addColumn := regexp.MustCompile(`(?i)^\s*ALTER TABLE (\w+) ADD COLUMN ([a-z_]+) UUID\b(.*)$`)

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}

		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}

		table := ""
		for line := range strings.SplitSeq(string(body), "\n") {
			if m := createTable.FindStringSubmatch(line); m != nil {
				table = m[1]
				continue
			}

			if m := addColumn.FindStringSubmatch(line); m != nil {
				if !strings.Contains(strings.ToUpper(m[3]), "NOT NULL") {
					out[m[1]+"."+m[2]] = true
				}

				continue
			}

			if strings.HasPrefix(strings.TrimSpace(line), ")") {
				table = ""
				continue
			}

			if table == "" {
				continue
			}

			if m := colDef.FindStringSubmatch(line); m != nil {
				rest := strings.ToUpper(m[2])
				if strings.Contains(rest, "NOT NULL") || strings.Contains(rest, "PRIMARY KEY") {
					continue
				}

				if m[1] == "project_id" {
					continue
				}

				out[table+"."+m[1]] = true
			}
		}
	}

	if len(out) == 0 {
		t.Fatal("no nullable uuid columns found - this test would pass vacuously")
	}

	return out
}

type insertStmt struct {
	table string
	cols  []string
	args  []string
	line  int
}

var insertRe = regexp.MustCompile(`(?is)INSERT INTO (\w+)\s*\(([^)]*)\)\s*VALUES\s*\(([^)]*)\)`)

// insertsIn returns every INSERT with an explicit column list and a
// flat one-placeholder-per-column VALUES, paired with the Go argument
// expressions that follow the query literal.
func insertsIn(src string) []insertStmt {
	var out []insertStmt
	for _, loc := range insertRe.FindAllStringSubmatchIndex(src, -1) {
		table := src[loc[2]:loc[3]]
		collist := src[loc[4]:loc[5]]
		vals := src[loc[6]:loc[7]]

		var cols []string
		for c := range strings.SplitSeq(strings.ReplaceAll(collist, "\n", " "), ",") {
			if c = strings.TrimSpace(c); c != "" {
				cols = append(cols, c)
			}
		}

		if strings.Count(vals, "?") != len(cols) {
			continue
		}

		tail := src[loc[1]:]
		_, after, ok := strings.Cut(tail, "`")
		if !ok {
			continue
		}

		rest := after

		// Three shapes, and reading only the first left thirteen
		// statements unchecked - among them projects, which writes
		// three nullable uuid columns.
		//
		//   Exec(ctx, `...`, a, b)          bind values follow
		//   ExecContext(ctx, s.Q(`...`), a) the literal is WRAPPED, so
		//                                   a close paren comes first
		//   PrepareContext(...); stmt.ExecContext(ctx, a, b)
		//
		// The first two are the same once the wrapper's close parens
		// are stepped over - but only when a comma is what follows
		// them, or this would read the code after an unrelated call as
		// arguments.
		args := splitArgs(afterWrapper(rest))
		if len(args) == 0 {
			if _, after0, ok0 := strings.Cut(rest, "ExecContext(ctx,"); ok0 {
				args = splitArgs(after0)
			}
		}

		out = append(out, insertStmt{
			table: table,
			cols:  cols,
			args:  args,
			line:  strings.Count(src[:loc[0]], "\n") + 1,
		})
	}

	return out
}

// afterWrapper steps over the close parens of a wrapper like s.Q(...)
// so the argument list starts at the comma. Returns the input
// unchanged when a comma is not what follows, which is how a prepared
// statement is told apart from an inline one.
func afterWrapper(s string) string {
	i := 0
	for i < len(s) && (s[i] == ')' || s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}

	if i < len(s) && s[i] == ',' {
		return s[i:]
	}

	return s
}

// splitArgs reads the comma-separated Go expressions up to the close
// of the call, ignoring commas nested inside one of them.
func splitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				if a := strings.TrimSpace(cur.String()); a != "" {
					out = append(out, a)
				}

				return out
			}

			depth--
		case ',':
			if depth == 0 {
				// Skip the empty leading element. The scan starts just
				// after the backtick closing the query, so the text
				// begins with the comma separating it from the first
				// argument - counting that as an argument made every
				// statement come out one long, every statement get
				// skipped, and the whole test pass while checking
				// nothing. Found by planting a bug and watching it not
				// fail.
				if a := strings.TrimSpace(cur.String()); a != "" {
					out = append(out, a)
				}

				cur.Reset()
				continue
			}
		}

		cur.WriteRune(r)
	}

	if a := strings.TrimSpace(cur.String()); a != "" {
		out = append(out, a)
	}

	return out
}

func walkGoFiles(t *testing.T, fn func(path, src string)) {
	t.Helper()
	root := filepath.Join(repoRoot(t), "internal")
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") {
			return err
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		// Parsed only to confirm it is Go we can trust to be shaped
		// the way the scanners assume.
		if _, err := parser.ParseFile(token.NewFileSet(), path, body, 0); err != nil {
			return nil
		}

		rel, _ := filepath.Rel(repoRoot(t), path)
		fn(rel, string(body))

		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}
