// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
)

// Every query in this repository, handed to a real PostgreSQL built
// from the real migrations, and asked one question: do the tables,
// columns and functions you name exist?
//
// Nothing else asks it. TestNoDynamicSQL proves a query is a constant,
// the store tests exercise the queries they happen to call, and the
// OpenAPI guards pin declarations to each other. None of them reads a
// query against the schema, so a SELECT can name a column that has
// never existed and the tree stays green until somebody opens the page
// and gets a 500.
//
// The technique is only available because of the constant rule: if
// every query is a compile-time constant, every query can be computed
// without running the program. This walks the same sinks
// TestNoDynamicSQL walks, evaluates the string rather than merely
// judging it, and prepares the result. Prepare parses, resolves every
// name and infers the parameter types without executing anything, so it
// costs nothing and cannot touch a row.
//
// Skips without MAILYARD_TEST_DSN, like every other store test. Run it
// with `task test-db`.
func TestEveryQueryMatchesTheSchema(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)

	var missed []string
	queries := collectQueries(t, &missed)
	if len(queries) < 200 {
		t.Fatalf("only found %d queries - the evaluator is broken, not the code", len(queries))
	}

	var (
		broken     []string
		unparsable []string
		checked    int
	)
	for _, q := range queries {
		//sqlconst:allow the text came out of the tree's own constants, never a request
		stmt, err := db.PrepareContext(t.Context(), database.Rebind(q.sql))
		if err == nil {
			_ = stmt.Close()
			checked++
			continue
		}

		switch classify(err) {
		case verdictBroken:
			broken = append(broken, q.where+"\n      "+oneLine(err.Error())+
				"\n      "+oneLine(q.sql))
		case verdictUnverified:
			// Not a schema question. A query assembled out of every
			// optional fragment at once is not the string any single
			// call site sends, so its syntax is not this test's
			// business - the names inside it were still resolved up to
			// the point the parser stopped.
			unparsable = append(unparsable, q.where+": "+oneLine(err.Error()))
		}
	}

	t.Logf("prepared %d of %d distinct queries against the migrated schema", checked, len(queries))
	if len(missed) > 0 {
		t.Logf("%d sink(s) whose SQL could not be computed:\n  %s",
			len(missed), strings.Join(missed, "\n  "))
	}

	if len(unparsable) > 0 {
		t.Logf("%d not verified (parameter types or an over-assembled variant):\n  %s",
			len(unparsable), strings.Join(unparsable, "\n  "))
	}

	if len(broken) > 0 {
		t.Errorf("%d quer(ies) name something the schema does not have:\n  %s\n\n"+
			"These parse, compile and pass every other guard. They fail when the\n"+
			"endpoint is called. Add the migration or fix the name.",
			len(broken), strings.Join(broken, "\n  "))
	}
}

type verdict int

const (
	verdictBroken verdict = iota
	verdictUnverified
)

// classify separates "this names something that does not exist" from
// everything else Prepare can complain about.
//
// The undefined_* family is the whole point: those are the errors a
// query gets for referring to a table, column or function the schema
// has not got, and there is no input that makes such a query work.
// Everything else - a parameter whose type Postgres cannot infer
// standing alone, or a syntax error in a variant assembled out of
// mutually exclusive fragments - says nothing about the schema.
func classify(err error) verdict {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return verdictUnverified
	}

	switch pg.Code {
	case "42P01", // undefined_table
		"42703", // undefined_column
		"42883", // undefined_function
		"42704", // undefined_object
		"42P10": // invalid_column_reference
		return verdictBroken
	}

	return verdictUnverified
}

type foundQuery struct {
	where string
	sql   string
}

// collectQueries evaluates the SQL argument of every sink in the tree.
//
// Test files are excluded on purpose: they build fixtures and their
// own scratch tables, so they say nothing about whether the product's
// queries match the product's migrations.
func collectQueries(t *testing.T, missed *[]string) []foundQuery {
	t.Helper()
	root := repoRoot(t)
	seen := map[string]bool{}
	var out []foundQuery

	dirs := goDirs(t, root)
	//nolint:staticcheck // deprecated for build-tag handling these guards do not want
	parsed := make([]*ast.Package, 0, len(dirs))
	fset := token.NewFileSet()
	for _, dir := range dirs {
		//nolint:staticcheck // deprecated for build-tag handling these guards do not want
		pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
			return !strings.HasSuffix(fi.Name(), "_test.go")
		}, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", dir, err)
		}

		for _, pkg := range pkgs {
			parsed = append(parsed, pkg)
		}
	}

	qualified := constStringValues(parsed)

	for _, pkg := range parsed {
		consts := scopedConsts(qualified, pkg.Name)
		for path, file := range pkg.Files {
			rel, _ := filepath.Rel(root, path)
			found, skipped := queriesInFile(fset, file, consts, rel)
			*missed = append(*missed, skipped...)
			for _, q := range found {
				if looksLikeSQL(q.sql) && !seen[q.sql] {
					seen[q.sql] = true
					out = append(out, q)
				}
			}
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].where < out[j].where })

	return out
}

// looksLikeSQL keeps fragments and non-queries out. A sink match is by
// method name alone, so a Q(...) holding half a WHERE clause, or an
// unrelated Exec, can turn up here.
func looksLikeSQL(s string) bool {
	head := strings.ToUpper(strings.TrimSpace(s))
	for _, verb := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "WITH"} {
		if strings.HasPrefix(head, verb) {
			return true
		}
	}

	return false
}

// queriesInFile walks each function in source order, keeping a small
// environment of local string variables, and evaluates the query
// argument at every sink it reaches.
func queriesInFile(fset *token.FileSet, file *ast.File, consts map[string]string, rel string) ([]foundQuery, []string) {
	var out []foundQuery
	var unresolved []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}

		locals := map[string]string{}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch stmt := n.(type) {
			case *ast.AssignStmt:
				trackAssign(stmt, consts, locals)
			case *ast.DeclStmt:
				// `var b strings.Builder` starts empty and is filled by
				// WriteString calls below.
				gd, ok := stmt.Decl.(*ast.GenDecl)
				if ok && gd.Tok == token.VAR {
					for _, spec := range gd.Specs {
						if vs, ok := spec.(*ast.ValueSpec); ok && len(vs.Values) == 0 {
							for _, name := range vs.Names {
								locals[name.Name] = ""
							}
						}
					}
				}
			case *ast.ExprStmt:
				trackBuilderWrite(stmt, consts, locals)
			case *ast.CallExpr:
				idx, ok := sqlSinkArg(stmt)
				if !ok || idx >= len(stmt.Args) {
					return true
				}

				sql, ok := evalString(stmt.Args[idx], consts, locals)
				if !ok {
					unresolved = append(unresolved, rel+":"+
						strconv.Itoa(fset.Position(stmt.Pos()).Line)+" "+fn.Name.Name)

					return true
				}

				out = append(out, foundQuery{
					where: rel + ":" + strconv.Itoa(fset.Position(stmt.Pos()).Line) +
						" " + fn.Name.Name,
					sql: sql,
				})
			}

			return true
		})
	}

	return out, unresolved
}

// trackAssign records `q := base` and `q += " AND x = ?"`.
//
// A conditional append lands in the environment unconditionally, so
// what gets prepared is the variant carrying every optional fragment.
// That is the widest set of names to resolve, which is what this test
// is reading for, and the reason a syntax error from an impossible
// combination is not treated as a finding.
func trackAssign(stmt *ast.AssignStmt, consts, locals map[string]string) {
	if len(stmt.Lhs) != len(stmt.Rhs) {
		for _, l := range stmt.Lhs {
			if id, ok := l.(*ast.Ident); ok {
				delete(locals, id.Name)
			}
		}

		return
	}

	for i, l := range stmt.Lhs {
		id, ok := l.(*ast.Ident)
		if !ok || id.Name == "_" {
			continue
		}

		val, ok := evalString(stmt.Rhs[i], consts, locals)
		if !ok {
			delete(locals, id.Name)
			continue
		}

		switch stmt.Tok {
		case token.DEFINE, token.ASSIGN:
			locals[id.Name] = val
		case token.ADD_ASSIGN:
			locals[id.Name] += val
		default:
			delete(locals, id.Name)
		}
	}
}

func trackBuilderWrite(stmt *ast.ExprStmt, consts, locals map[string]string) {
	call, ok := stmt.X.(*ast.CallExpr)
	if !ok {
		return
	}

	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "WriteString" || len(call.Args) != 1 {
		return
	}

	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return
	}

	if _, tracked := locals[id.Name]; !tracked {
		return
	}

	val, ok := evalString(call.Args[0], consts, locals)
	if !ok {
		delete(locals, id.Name)

		return
	}

	locals[id.Name] += val
}

// evalString computes a string expression, or reports that it cannot.
func evalString(e ast.Expr, consts, locals map[string]string) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}

		s, err := strconv.Unquote(v.Value)
		if err != nil {
			return "", false
		}

		return s, true
	case *ast.ParenExpr:
		return evalString(v.X, consts, locals)
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}

		l, ok := evalString(v.X, consts, locals)
		if !ok {
			return "", false
		}

		r, ok := evalString(v.Y, consts, locals)
		if !ok {
			return "", false
		}

		return l + r, true
	case *ast.Ident:
		if s, ok := locals[v.Name]; ok {
			return s, true
		}

		s, ok := consts[v.Name]

		return s, ok
	case *ast.SelectorExpr:
		// A constant from another package - relaynode.FreshJoin is
		// folded into the smtp server SELECTs, which are the delivery
		// path, so leaving these uncomputed would have exempted the
		// queries that matter most.
		if id, ok := v.X.(*ast.Ident); ok {
			s, ok := consts[id.Name+"."+v.Sel.Name]

			return s, ok
		}
	case *ast.CallExpr:
		// Q(query) rebinds placeholders and is otherwise its argument.
		if idx, ok := sqlSinkArg(v); ok && idx == 0 && len(v.Args) > 0 {
			return evalString(v.Args[0], consts, locals)
		}

		// b.String() on a builder this walk has been filling.
		if sel, ok := v.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "String" && len(v.Args) == 0 {
			if id, ok := sel.X.(*ast.Ident); ok {
				s, ok := locals[id.Name]

				return s, ok
			}
		}
	}

	return "", false
}

// constStringValues resolves every package-level string constant in
// the tree to its value, keyed pkg.Name.
//
// Global rather than per-package because the constants that matter
// most cross a package boundary: serverSelect folds in
// relaynode.FreshJoin, and groupServerSelect folds in
// relaynode.FreshClause. Resolving only within a package left the four
// smtp-server reads - the delivery path - uncomputed, which is exactly
// backwards from where the checking is worth having.
//
// Repeated to a fixpoint, so a constant built out of other constants
// comes out whole however deep the chain.
//
//nolint:staticcheck // deprecated for build-tag handling these guards do not want
func constStringValues(pkgs []*ast.Package) map[string]string {
	type pending struct {
		pkg, name string
		expr      ast.Expr
	}
	var todo []pending
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok || gd.Tok != token.CONST {
					continue
				}

				for _, spec := range gd.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok || len(vs.Names) != len(vs.Values) {
						continue
					}

					for i, name := range vs.Names {
						todo = append(todo, pending{pkg.Name, name.Name, vs.Values[i]})
					}
				}
			}
		}
	}

	out := map[string]string{}
	for range len(todo) + 1 {
		progress := false
		for _, p := range todo {
			if _, done := out[p.pkg+"."+p.name]; done {
				continue
			}

			if v, ok := evalString(p.expr, scopedConsts(out, p.pkg), nil); ok {
				out[p.pkg+"."+p.name] = v
				progress = true
			}
		}

		if !progress {
			break
		}
	}

	return out
}

// scopedConsts is the qualified table as one package sees it: every
// pkg.Name key, plus its own constants under their bare names.
func scopedConsts(qualified map[string]string, pkg string) map[string]string {
	out := make(map[string]string, len(qualified)*2)
	prefix := pkg + "."
	for k, v := range qualified {
		out[k] = v
		if bare, ok := strings.CutPrefix(k, prefix); ok {
			out[bare] = v
		}
	}

	return out
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
