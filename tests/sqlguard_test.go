// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// This file is the mechanical guarantee that no SQL statement in this
// repository is built out of runtime data.
//
// Placeholders are what stop injection, and every store here uses
// them - but nothing enforced that. It rested on reviewers noticing,
// which is exactly the thing that erodes. The obvious answer, gosec's
// G201/G202, was tried first and does not work: on a textbook
// `db.Query(fmt.Sprintf("... Where email = '%s'", e))` it reports zero
// issues, both through golangci-lint and as a standalone binary. A
// linter that stays silent on the canonical case is worse than none,
// because the green report is taken for proof.
//
// So the check lives here, in the gate that already runs
// (`go test ./...`), written against the standard library only, and
// TestGuardCatchesInjection below proves it fires on the cases gosec
// missed.
//
// The rule: every SQL string reaching Q/Exec/Query/QueryRow (and the
// database/sql *Context methods) must be a compile-time constant, or
// a local variable assembled only out of compile-time constants. A
// value that came from a parameter, a field, or fmt.Sprintf is a
// finding.

// sqlSinks maps a called method name to the argument index holding
// the SQL. The database/sql *Context methods take a context first,
// everything on database.Base takes the query where it reads.
// Matching is on the method name alone, with no type information, so
// it over-matches: stmt.ExecContext(ctx, bindValue) on a prepared
// statement looks identical to db.ExecContext(ctx, query). That
// direction is chosen deliberately. A false positive is one visible
// line needing an allow marker; a false negative is an injection this
// test swears does not exist. Narrowing the match to known receiver
// names would silently stop guarding the day somebody introduces a
// new handle.
//
// Every helper on Base has to be listed. Each one carries a
// //sqlconst:allow inside base.go whose stated reason is "the query is
// checked at each call site" - leave it out of this map and those call
// sites are not checked at all, so the marker becomes a false claim and
// a textbook `s.Query(ctx, "... WHERE email = '"+email+"'")` passes.
var sqlSinks = map[string]int{
	"Q":                 0, // Base.Q, the Rebind wrapper every store uses
	"Exec":              1, // Base.Exec(ctx, query, ...)
	"Query":             1, // Base.Query(ctx, query, ...)
	"QueryRow":          1, // Base.QueryRow(ctx, query, ...)
	"ReadQuery":         1, // Base.ReadQuery, the replica read
	"ReadQueryRow":      1, // Base.ReadQueryRow
	"ExecContext":       1,
	"QueryContext":      1,
	"QueryRowContext":   1,
	"PrepareContext":    1,
	"SelectContext":     1,
	"GetContext":        1,
	"NamedExecContext":  1,
	"NamedQueryContext": 1,
	// Project helpers that take a query and hand it onward. Listing
	// them means their CALL SITES are checked, which is the only place
	// the string is still visible - inside the helper it is just a
	// parameter.
	"count":   1,
	"countBy": 1,
}

// allowMarker opts one call out, and must carry a reason. Written as
// a comment on or above the call:
//
//	//sqlconst:allow <why this cannot carry user input>
//
// Deliberately not a bare //nolint: an exception here is a claim that
// interpolated text can never reach a request, and that claim belongs
// in the source next to the code making it.
const allowMarker = "//sqlconst:allow"

func TestNoDynamicSQL(t *testing.T) {
	root := repoRoot(t)
	var findings []string

	for _, dir := range goDirs(t, root) {
		fset := token.NewFileSet()
		//nolint:staticcheck // deprecated for build-tag handling these guards do not want
		pkgs, err := parser.ParseDir(fset, dir, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", dir, err)
		}

		for _, pkg := range pkgs {
			consts := packageConsts(pkg)
			for path, file := range pkg.Files {
				allowed := allowedLines(fset, file)
				rel, _ := filepath.Rel(root, path)
				findings = append(findings, checkFile(fset, file, consts, allowed, rel)...)
			}
		}
	}

	if len(findings) > 0 {
		t.Errorf("SQL built from runtime data in %d place(s):\n  %s\n\n"+
			"Every query must be a constant or assembled only from constants, with\n"+
			"user values passed as ? placeholders. If a case is genuinely safe, mark\n"+
			"it %s <reason>.",
			len(findings), strings.Join(findings, "\n  "), allowMarker)
	}
}

// checkFile reports every SQL sink in one file whose query argument is
// not provably constant.
func checkFile(fset *token.FileSet, file *ast.File, consts map[string]bool,
	allowed map[int]bool, rel string,
) []string {
	var out []string
	// Walk function by function: the safe-local analysis is scoped to
	// one body, so a variable cannot be laundered across functions.
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}

		safe := safeLocals(fn.Body, consts)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			idx, ok := sqlSinkArg(call)
			if !ok || idx >= len(call.Args) {
				return true
			}

			line := fset.Position(call.Pos()).Line
			if allowed[line] {
				return true
			}

			arg := call.Args[idx]
			if isConstExpr(arg, consts, safe) {
				return true
			}

			// db.ExecContext(ctx, s.Q(bad)) would otherwise be reported
			// twice, once for the outer sink and once for Q. Leave it
			// to the inner call, which names the offending expression
			// precisely.
			if inner, ok := arg.(*ast.CallExpr); ok {
				if i, isSink := sqlSinkArg(inner); isSink && i < len(inner.Args) {
					return true
				}
			}

			out = append(out, rel+":"+strconv.Itoa(line)+" "+render(arg))

			return true
		})
	}

	return out
}

// sqlSinkArg reports the index of the SQL argument when call targets a
// known sink. Matching is on the selector name alone, which is why the
// names are distinctive - c.Query(...) on a fiber context reads a URL
// parameter and is deliberately not in the map.
func sqlSinkArg(call *ast.CallExpr) (int, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return 0, false
	}

	idx, ok := sqlSinks[sel.Sel.Name]
	if !ok {
		return 0, false
	}

	// Exec, Query and QueryRow name two different signatures. On
	// database.Base the query is second, after a context - which is what
	// the table above records. On *sql.DB and *sql.Tx the same three
	// names take the query first.
	//
	// The table's index was applied to both, so `db.Exec(query)` was
	// looked up at index 1, found nothing there, and the caller's
	// `idx >= len(call.Args)` skipped the call entirely. One such call
	// already existed (dbtest's schema cleanup) and any future
	// `db.Exec("... " + userValue)` would have been invisible to this
	// test - which is the failure mode it exists to prevent.
	//
	// A context never looks like a string, so shifting to index 0 when
	// the first argument is stringy adds no false positive.
	//
	// Being stringy is the whole test, and "the call has one argument"
	// is not a usable substitute for it. Tried, and it reported two
	// calls that have nothing to do with SQL: c.count(context.Background())
	// in the metrics collector, and Fiber's own c.Query(param) reading a
	// query-string value. That is the over-matching this table's comment
	// warns about, arriving through the back door.
	if idx > 0 && len(call.Args) > 0 && looksStringy(call.Args[0]) {
		return 0, true
	}

	return idx, true
}

// looksStringy reports whether e is syntactically a string expression:
// a literal, a parenthesised or concatenated one, or a conversion of
// one. Used only to tell a query argument from a leading context.
func looksStringy(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.BasicLit:
		return v.Kind == token.STRING
	case *ast.ParenExpr:
		return looksStringy(v.X)
	case *ast.BinaryExpr:
		return v.Op == token.ADD && (looksStringy(v.X) || looksStringy(v.Y))
	}

	return false
}

// isConstExpr reports whether e is a compile-time constant string, a
// concatenation of them, a package const, or a safe local.
func isConstExpr(e ast.Expr, consts, safe map[string]bool) bool {
	switch v := e.(type) {
	case *ast.BasicLit:
		return v.Kind == token.STRING
	case *ast.ParenExpr:
		return isConstExpr(v.X, consts, safe)
	case *ast.BinaryExpr:
		// Only concatenation can build a query, and both halves must
		// themselves be constant.
		return v.Op == token.ADD &&
			isConstExpr(v.X, consts, safe) && isConstExpr(v.Y, consts, safe)
	case *ast.Ident:
		return consts[v.Name] || safe[v.Name]
	case *ast.CallExpr:
		// Q(query) only rebinds placeholders, so it is as constant as
		// what it wraps. Anything else - Sprintf above all - is not.
		if idx, ok := sqlSinkArg(v); ok && idx == 0 && len(v.Args) > 0 {
			return isConstExpr(v.Args[0], consts, safe)
		}

		// b.String() on a strings.Builder that only ever received
		// constants. The filter builders in the list endpoints are
		// written this way, and rejecting the pattern outright would
		// push people toward an allow marker on code the check can
		// actually verify.
		if sel, ok := v.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "String" && len(v.Args) == 0 {
			if id, ok := sel.X.(*ast.Ident); ok {
				return safe[id.Name]
			}
		}

		return false
	}

	return false
}

// safeLocals finds local string variables assembled only out of
// constants, which is how the list endpoints build their filters:
//
//	query := rowSelect + ` WHERE project_id = ?`
//	if status != "" { query += ` AND status = ?` }
//
// A variable is safe only if every assignment to it in the function
// is constant, so one Sprintf anywhere taints it everywhere.
func safeLocals(body *ast.BlockStmt, consts map[string]bool) map[string]bool {
	candidates := map[string]bool{}
	tainted := map[string]bool{}

	record := func(lhs []ast.Expr, rhs []ast.Expr) {
		for i, l := range lhs {
			id, ok := l.(*ast.Ident)
			if !ok || id.Name == "_" {
				continue
			}

			// A multi-value assignment (v, err := f()) has no
			// per-name expression to judge, so treat it as tainted.
			if len(rhs) != len(lhs) {
				tainted[id.Name] = true
				continue
			}

			if isConstExpr(rhs[i], consts, candidates) {
				candidates[id.Name] = true
			} else {
				tainted[id.Name] = true
			}
		}
	}

	ast.Inspect(body, func(n ast.Node) bool {
		switch stmt := n.(type) {
		case *ast.AssignStmt:
			switch stmt.Tok {
			case token.DEFINE, token.ASSIGN, token.ADD_ASSIGN:
				record(stmt.Lhs, stmt.Rhs)
			default:
				// Any other compound assignment is not string building.
				for _, l := range stmt.Lhs {
					if id, ok := l.(*ast.Ident); ok {
						tainted[id.Name] = true
					}
				}
			}
		case *ast.DeclStmt:
			// `var b strings.Builder` starts out empty, so it is a
			// candidate until something non-constant is written to it.
			gd, ok := stmt.Decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				return true
			}

			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Values) > 0 {
					continue
				}

				for _, name := range vs.Names {
					candidates[name.Name] = true
				}
			}
		case *ast.ExprStmt:
			// Writes into a builder. Anything non-constant written
			// into it taints the whole builder, so String() on it
			// stops counting as constant.
			call, ok := stmt.X.(*ast.CallExpr)
			if !ok {
				return true
			}

			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}

			switch sel.Sel.Name {
			case "WriteString", "Write", "WriteByte", "WriteRune":
			default:
				return true
			}

			id, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}

			for _, a := range call.Args {
				if !isConstExpr(a, consts, candidates) {
					tainted[id.Name] = true
				}
			}
		}

		return true
	})

	// Taking the address of a local, or passing it to something that
	// could rewrite it, is out of scope: these are query strings, and
	// nothing in this codebase does that. Tainted always wins.
	for name := range tainted {
		delete(candidates, name)
	}

	return candidates
}

// packageConsts collects package-level const names, so a query
// fragment declared as `const rowSelect = ...` counts as constant.
//
//nolint:staticcheck // deprecated for build-tag handling these guards do not want
func packageConsts(pkg *ast.Package) map[string]bool {
	out := map[string]bool{}
	for _, file := range pkg.Files {
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}

			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}

				for _, name := range vs.Names {
					out[name.Name] = true
				}
			}
		}
	}

	return out
}

// allowedLines maps the lines covered by an allow marker. The marker
// applies to its own line, the line after, and the line after the
// whole comment group it belongs to.
//
// That last one is not decoration. Two markers stack on the same
// statement wherever a call is both safe to interpolate and safe on a
// replica, and with a plain marker+1 rule the upper one covered the
// LOWER COMMENT instead of the call - so the analytics helpers were
// reported despite carrying a reason, and the fix somebody reaches for
// under that pressure is to delete the marker that "does not work".
func allowedLines(fset *token.FileSet, file *ast.File) map[int]bool {
	out := map[int]bool{}
	for _, group := range file.Comments {
		after := fset.Position(group.End()).Line + 1
		for _, c := range group.List {
			if !strings.Contains(c.Text, allowMarker) {
				continue
			}

			line := fset.Position(c.Pos()).Line
			out[line] = true
			out[line+1] = true
			out[after] = true
		}
	}

	return out
}

// goDirs lists every directory holding Go source, skipping the
// reference implementation and anything vendored.
func goDirs(t *testing.T, root string) []string {
	t.Helper()
	seen := map[string]bool{}
	var dirs []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			switch info.Name() {
			case "node_modules", "vendor", ".git", "dist", "build", "dev-data":
				return filepath.SkipDir
			}

			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		dir := filepath.Dir(path)
		if !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	return dirs
}

func render(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.CallExpr:
		if sel, ok := v.Fun.(*ast.SelectorExpr); ok {
			return render(sel.X) + "." + sel.Sel.Name + "(...)"
		}

		return "call(...)"
	case *ast.SelectorExpr:
		return render(v.X) + "." + v.Sel.Name
	case *ast.BinaryExpr:
		return render(v.X) + " + " + render(v.Y)
	case *ast.BasicLit:
		return "literal"
	}

	return "expression"
}

// The guard is only worth its place if it fires. gosec's G201/G202
// reported zero on every one of these, which is why this test exists
// at all - it runs the same checker over source that is deliberately
// wrong and asserts each case is caught.
//
// Parsed from a string rather than a file so the vulnerable code
// never sits in the tree, where somebody might copy it.
func TestGuardCatchesInjection(t *testing.T) {
	const src = `package victim

import (
	"database/sql"
	"fmt"
	"strings"
)

const safeSelect = "SELECT * FROM users"

func sprintfInline(db *sql.DB, email string) (*sql.Rows, error) {
	return db.QueryContext(ctx, fmt.Sprintf("SELECT * FROM users WHERE email = '%s'", email))
}

func sprintfViaVar(db *sql.DB, email string) (*sql.Rows, error) {
	q := fmt.Sprintf("SELECT * FROM users WHERE email = '%s'", email)
	return db.QueryContext(ctx, q)
}

func concatInline(db *sql.DB, email string) (*sql.Rows, error) {
	return db.QueryContext(ctx, "SELECT * FROM users WHERE email = '"+email+"'")
}

func concatOntoConst(db *sql.DB, order string) (*sql.Rows, error) {
	q := safeSelect + " ORDER BY " + order
	return db.QueryContext(ctx, q)
}

func interpolatedTable(db *sql.DB, table string) (sql.Result, error) {
	return db.ExecContext(ctx, "DELETE FROM "+table)
}

func taintedBuilder(db *sql.DB, status string) (*sql.Rows, error) {
	var b strings.Builder
	b.WriteString(safeSelect)
	b.WriteString(" WHERE status = '" + status + "'")
	return db.QueryContext(ctx, b.String())
}

func throughQ(db *sql.DB, email string) (*sql.Rows, error) {
	return db.QueryContext(ctx, s.Q("SELECT * FROM users WHERE email = '"+email+"'"))
}

func throughQuery(email string) (*sql.Rows, error) {
	return s.Query(ctx, "SELECT * FROM users WHERE email = '"+email+"'")
}

func throughReadQuery(email string) (*sql.Rows, error) {
	return s.ReadQuery(ctx, "SELECT * FROM users WHERE email = '"+email+"'")
}

func throughReadQueryRow(email string) *sql.Row {
	return s.ReadQueryRow(ctx, "SELECT * FROM users WHERE email = '"+email+"'")
}

// These must not be reported.
func constLiteral(db *sql.DB, email string) (*sql.Rows, error) {
	return db.QueryContext(ctx, "SELECT * FROM users WHERE email = ?", email)
}

func constFragments(db *sql.DB, status string, args []any) (*sql.Rows, error) {
	q := safeSelect + " WHERE project_id = ?"
	if status != "" {
		q += " AND status = ?"
	}
	return db.QueryContext(ctx, s.Q(q), args...)
}

func cleanBuilder(db *sql.DB, status string, args []any) (*sql.Rows, error) {
	var b strings.Builder
	b.WriteString(safeSelect)
	if status != "" {
		b.WriteString(" WHERE status = ?")
	}
	return db.QueryContext(ctx, s.Q(b.String()), args...)
}
`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "victim.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	consts := map[string]bool{"safeSelect": true}
	found := map[string]bool{}
	for _, f := range checkFile(fset, file, consts, map[int]bool{}, "victim.go") {
		// Findings read "victim.go:<line> <expr>"; map them back to the
		// enclosing function so the assertions read by intent.
		line := 0
		if _, err := sscanLine(f, &line); err == nil {
			found[funcAtLine(fset, file, line)] = true
		}
	}

	mustCatch := []string{
		"sprintfInline", "sprintfViaVar", "concatInline", "concatOntoConst",
		"interpolatedTable", "taintedBuilder", "throughQ",
		// The three Base helpers that were missing from sqlSinks. Each
		// carries an allow marker in base.go claiming its call sites are
		// checked, so their absence made that claim false.
		"throughQuery", "throughReadQuery", "throughReadQueryRow",
	}
	for _, name := range mustCatch {
		if !found[name] {
			t.Errorf("%s builds SQL from a parameter and was NOT caught", name)
		}
	}

	mustPass := []string{"constLiteral", "constFragments", "cleanBuilder"}
	for _, name := range mustPass {
		if found[name] {
			t.Errorf("%s is safe but was reported - the guard would push people to blanket-allow", name)
		}
	}
}

// sscanLine pulls the line number out of a "path:line rest" finding.
func sscanLine(finding string, out *int) (int, error) {
	_, rest, ok := strings.Cut(finding, ":")
	if !ok {
		return 0, errNoLine
	}

	num, _, _ := strings.Cut(rest, " ")
	n := 0
	for _, r := range num {
		if r < '0' || r > '9' {
			return 0, errNoLine
		}

		n = n*10 + int(r-'0')
	}

	*out = n

	return 1, nil
}

var errNoLine = errors.New("no line in finding")

// funcAtLine names the function containing a line.
func funcAtLine(fset *token.FileSet, file *ast.File, line int) string {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}

		if fset.Position(fn.Pos()).Line <= line && line <= fset.Position(fn.End()).Line {
			return fn.Name.Name
		}
	}

	return ""
}

// A marker must still cover its call when a second marker sits below
// it. Both markers are legitimate on one statement - "cannot carry
// injection" and "a follower may serve this" are different claims -
// and the analytics count helpers carry both. With a marker+1 rule the
// upper one covered the lower COMMENT, so a correctly-excused call was
// reported and the obvious fix was to delete the marker.
func TestAStackedAllowMarkerStillCoversTheCall(t *testing.T) {
	const src = `package victim

func stacked(table string) (int, error) {
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
	//sqlconst:allow table is a package constant at every call site
	//replicaread:allow a plain COUNT
	return s.ReadQueryRow(ctx, query).Scan(&n)
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "victim.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	got := checkFile(fset, file, map[string]bool{}, allowedLines(fset, file), "victim.go")
	if len(got) != 0 {
		t.Errorf("an excused call was reported anyway: %v", got)
	}
}
