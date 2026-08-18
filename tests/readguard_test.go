// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readHelpers are the Base methods that may reach a follower.
var readHelpers = map[string]bool{"ReadQuery": true, "ReadQueryRow": true}

// TestReadHelpersOnlyServeReads checks every query routed to a
// replica.
//
// Two ways a statement can be wrong here, and they fail differently.
// A write is the loud one: a follower is read-only, so an INSERT sent
// there errors at runtime, in production, on whichever request drew
// that replica from the round-robin. `SELECT ... FOR UPDATE` is the
// quiet one - it parses as a read, takes a lock on a machine nothing
// will ever commit against, and the caller believes it holds
// something it does not.
//
// The comment on Base.ReadQuery lists the judgement calls this test
// CANNOT make: whether a write later in the same request depends on
// what came back, whether the caller just wrote the row it is reading
// again, whether the answer authenticates somebody. Those need a
// person. This catches the mechanical half so the review can spend
// itself on the rest.
func TestReadHelpersOnlyServeReads(t *testing.T) {
	var bad []string

	err := filepath.WalkDir(goTree(t), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			if d.Name() == "node_modules" {
				return filepath.SkipDir
			}

			return nil
		}

		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil
		}

		consts := constStrings(filepath.Dir(path))
		excused := excusedReadLines(fset, path)
		// Per function, because resolving the query means looking at
		// what the ENCLOSING function assigned to it. Almost no read
		// here is an inline literal: the house pattern is a constant
		// SELECT with optional clauses appended to a local, which is
		// exactly what TestNoDynamicSQL is built to permit.
		for _, decl := range file.Decls {
			fn, isFn := decl.(*ast.FuncDecl)
			if !isFn || fn.Body == nil {
				continue
			}

			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}

				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || !readHelpers[sel.Sel.Name] {
					return true
				}

				// Arg 0 is ctx, arg 1 is the query.
				if len(call.Args) < 2 {
					return true
				}

				text, ok := queryText(call.Args[1], fn.Body, consts)
				if !ok {
					if excused[fset.Position(call.Pos()).Line] {
						return true
					}

					// Not readable even after resolving constants and
					// the local it was built in. Say so rather than
					// pass it silently: an unreadable query is one this
					// test is not checking.
					bad = append(bad, fset.Position(call.Pos()).String()+
						": "+sel.Sel.Name+" with a query this test cannot read - "+
						"inline the text, move the call to Query if it is not a plain read, "+
						"or mark it //replicaread:allow <reason> if the text lives at the call sites")

					return true
				}

				if why := notAPlainRead(text); why != "" {
					bad = append(bad, fset.Position(call.Pos()).String()+
						": "+sel.Sel.Name+" "+why)
				}

				return true
			})
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	if len(bad) > 0 {
		t.Errorf("%d query(ies) routed to a read replica are not plain reads:\n  %s",
			len(bad), strings.Join(bad, "\n  "))
	}
}

// notAPlainRead returns why a statement must not go to a follower, or
// "" when it may.
func notAPlainRead(q string) string {
	upper := strings.ToUpper(q)
	trimmed := strings.TrimSpace(upper)
	// Leading comments and parentheses are common in these constants.
	trimmed = strings.TrimLeft(trimmed, "(\n\t ")
	if !strings.HasPrefix(trimmed, "SELECT") && !strings.HasPrefix(trimmed, "WITH") {
		return "does not start with SELECT or WITH"
	}

	for _, verb := range []string{"INSERT ", "UPDATE ", "DELETE ", "TRUNCATE ", "ALTER ", "CREATE ", "DROP "} {
		if strings.Contains(upper, verb) {
			return "contains " + strings.TrimSpace(verb) + ", which a read-only follower will refuse"
		}
	}

	// The quiet one. It reads, so nothing rejects it, and the lock it
	// takes is on a machine that will never commit the write it was
	// taken for.
	if strings.Contains(upper, "FOR UPDATE") || strings.Contains(upper, "FOR SHARE") {
		return "takes a row lock, which is meaningless on a follower"
	}

	return ""
}

// excusedReadLines collects the lines marked //replicaread:allow.
//
// A separate marker from //sqlconst:allow on purpose, even where both
// sit on the same statement. They answer different questions - one is
// "this text cannot carry injection", the other is "this text is a
// plain read that a follower may serve" - and a single marker would
// let an answer to the first be read as an answer to the second.
//
// The reason is mandatory. A bare marker is a line somebody silenced,
// and the whole value of an escape hatch is that it says who decided
// and on what grounds.
func excusedReadLines(fset *token.FileSet, path string) map[int]bool {
	out := map[int]bool{}
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return out
	}

	for _, group := range f.Comments {
		// Both the line after the marker and the line after the whole
		// group, so a marker still covers the call when the two markers
		// are stacked and this one is on top.
		after := fset.Position(group.End()).Line + 1
		for _, c := range group.List {
			rest, found := strings.CutPrefix(c.Text, "//replicaread:allow")
			if !found || strings.TrimSpace(rest) == "" {
				continue
			}

			out[fset.Position(c.Pos()).Line+1] = true
			out[after] = true
		}
	}

	return out
}

// queryText recovers the SQL a read helper was handed.
//
// Three shapes, and all three appear in the stores:
//
//	s.ReadQuery(ctx, `SELECT ...`)                  inline
//	query := supSelect + ` where ...`; query += ...  a local
//	sb.WriteString(inboundSelect); sb.WriteString(...)  a Builder
//
// The last two are what TestNoDynamicSQL explicitly permits, so a
// guard that only understood the first would have flagged every real
// call site and been switched off. What comes back is every literal
// and resolved constant that reaches the variable, joined - not the
// exact string any single call sends, which does not exist as a
// constant anyway once optional clauses are involved. That is the
// right shape for the question being asked: a FOR UPDATE or an UPDATE
// verb has to appear in one of these pieces to reach the follower at
// all.
func queryText(e ast.Expr, body *ast.BlockStmt, consts map[string]string) (string, bool) {
	if text, ok := literalText(e, consts); ok {
		return text, true
	}

	switch v := e.(type) {
	case *ast.Ident:
		return assignedText(v.Name, body, consts)
	case *ast.CallExpr:
		// sb.String() on a strings.Builder.
		sel, ok := v.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "String" {
			return "", false
		}

		recv, ok := sel.X.(*ast.Ident)
		if !ok {
			return "", false
		}

		return writtenText(recv.Name, body, consts)
	}

	return "", false
}

// assignedText joins every string that reaches local name in body,
// through := , = and +=.
func assignedText(name string, body *ast.BlockStmt, consts map[string]string) (string, bool) {
	var parts []string
	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}

		for i, lhs := range as.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok || id.Name != name || i >= len(as.Rhs) {
				continue
			}

			if text, ok := literalText(as.Rhs[i], consts); ok {
				parts = append(parts, text)
			}
		}

		return true
	})
	if len(parts) == 0 {
		return "", false
	}

	return strings.Join(parts, "\n"), true
}

// writtenText joins every string written into a strings.Builder.
func writtenText(name string, body *ast.BlockStmt, consts map[string]string) (string, bool) {
	var parts []string
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "WriteString" {
			return true
		}

		recv, ok := sel.X.(*ast.Ident)
		if !ok || recv.Name != name {
			return true
		}

		if text, ok := literalText(call.Args[0], consts); ok {
			parts = append(parts, text)
		}

		return true
	})
	if len(parts) == 0 {
		return "", false
	}

	return strings.Join(parts, "\n"), true
}

// literalText resolves an expression to text: a string literal, a
// package constant naming one, or any concatenation of those.
func literalText(e ast.Expr, consts map[string]string) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}

		return strings.Trim(v.Value, "`\""), true
	case *ast.Ident:
		text, ok := consts[v.Name]

		return text, ok
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}

		l, okl := literalText(v.X, consts)
		r, okr := literalText(v.Y, consts)
		if !okl || !okr {
			return "", false
		}

		return l + r, true
	default:
		return "", false
	}
}

// constStrings collects the package's string constants by value.
//
// A fixpoint loop rather than one pass, because these are written in
// terms of each other - emailSelect is "SELECT " + emailColumns +
// " FROM emails", and the columns constant may be declared after it.
func constStrings(dir string) map[string]string {
	out := map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}

	var decls []*ast.ValueSpec
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}

		file, perr := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if perr != nil {
			continue
		}

		for _, d := range file.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}

			for _, spec := range gd.Specs {
				if vs, ok := spec.(*ast.ValueSpec); ok {
					decls = append(decls, vs)
				}
			}
		}
	}

	// Bounded: each pass either resolves something new or stops.
	for range 5 {
		before := len(out)
		for _, vs := range decls {
			for i, name := range vs.Names {
				if i >= len(vs.Values) || out[name.Name] != "" {
					continue
				}

				if text, ok := literalText(vs.Values[i], out); ok {
					out[name.Name] = text
				}
			}
		}

		if len(out) == before {
			break
		}
	}

	return out
}
