// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestReplicasGoExactlyToTheStoresThatReadThem pins the two halves of
// follower reads together.
//
// They drifted apart once already, silently and in both directions at
// the same time. Eight stores were handed replicas while NOT ONE
// query in the process called ReadQuery, so the followers held open
// connections and served nothing - and the comment above the list
// asserted the opposite, which is worse than no comment, because the
// next person greps for a Read* call, finds none, and cannot tell
// whether the code or the comment is wrong.
//
// The failure is invisible from the outside in both directions. A
// store given replicas it never reads costs a connection and a lie. A
// store with a Read* call and no replicas quietly serves that query
// from the primary, so the load an operator built a follower to move
// stays exactly where it was, and nothing anywhere says so.
func TestReplicasGoExactlyToTheStoresThatReadThem(t *testing.T) {
	root := repoRoot(t)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, providerFile(t), nil, 0)
	if err != nil {
		t.Fatalf("parse provider.go: %v", err)
	}

	// Package ident -> import path, so a store is located by what it
	// actually imports rather than by guessing that the identifier
	// matches the directory.
	paths := map[string]string{}
	for _, imp := range file.Imports {
		p := strings.Trim(imp.Path.Value, `"`)
		name := p[strings.LastIndex(p, "/")+1:]
		if imp.Name != nil {
			name = imp.Name.Name
		}

		paths[name] = p
	}

	given := map[string]bool{} // package -> handed replicas
	seen := map[string]bool{}  // package -> constructed here at all
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !strings.HasSuffix(sel.Sel.Name, "Store") {
			return true
		}

		pkg, ok := sel.X.(*ast.Ident)
		if !ok || paths[pkg.Name] == "" {
			return true
		}

		seen[pkg.Name] = true
		// The replica argument is the ro(...) call, spread.
		for _, arg := range call.Args {
			inner, ok := arg.(*ast.CallExpr)
			if !ok {
				continue
			}

			if id, ok := inner.Fun.(*ast.Ident); ok && id.Name == "ro" {
				given[pkg.Name] = true
			}
		}

		return true
	})

	if len(seen) < 20 {
		t.Fatalf("only found %d store constructions, the provider parse is broken", len(seen))
	}

	var deadPlumbing, unrouted []string
	for pkg := range seen {
		dir := filepath.Join(root, strings.TrimPrefix(paths[pkg], "github.com/yousysadmin/mailyard/"))
		reads := packageReadsFromReplica(t, dir)
		switch {
		case given[pkg] && !reads:
			deadPlumbing = append(deadPlumbing, pkg)
		case !given[pkg] && reads:
			unrouted = append(unrouted, pkg)
		}
	}

	sort.Strings(deadPlumbing)
	sort.Strings(unrouted)

	if len(deadPlumbing) > 0 {
		t.Errorf("%v are handed read replicas but never call ReadQuery or ReadQueryRow.\n"+
			"That is plumbing nobody can tell is dead: it opens connections, serves nothing, and\n"+
			"reads as though those stores use a follower. Either move a read over, or drop the\n"+
			"ro(...) argument.", deadPlumbing)
	}

	if len(unrouted) > 0 {
		t.Errorf("%v call a Read* helper but are constructed without replicas.\n"+
			"Those queries silently run on the primary, so the load a follower was built to take\n"+
			"never moves and nothing reports it. Add ro(<group>) here, with a group in\n"+
			"env.ReplicaReadsConfig.", unrouted)
	}
}

// packageReadsFromReplica reports whether any non-test file in dir
// calls a Read* helper.
func packageReadsFromReplica(t *testing.T, dir string) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}

		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}

		if strings.Contains(string(body), ".ReadQuery(") || strings.Contains(string(body), ".ReadQueryRow(") {
			return true
		}
	}

	return false
}
