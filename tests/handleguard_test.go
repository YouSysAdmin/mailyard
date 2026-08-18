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

// allowedOnTheRawHandle are the methods a store may still reach
// through DB().
//
// BeginTx only. A transaction is a sequence of statements that must
// run on one connection, so it cannot go through the per-call helpers
// - and it is always a write, so there is no routing decision to make
// about it anyway.
var allowedOnTheRawHandle = map[string]bool{"BeginTx": true}

// TestStoresDoNotReachPastTheHelpers keeps every query on one path.
//
// Base.Exec, Query and QueryRow are the only ways a store issues a
// statement. That mattered already - they are what applies Rebind, so
// a query that skipped them sends `?` at a driver that wants $1 - but
// it matters more now that Base is where read/write routing will
// live. A statement issued through DB() directly bypasses whatever
// Base decides, and it does so silently: it works perfectly against a
// single database and starts reading stale rows the day a replica is
// configured.
//
// 263 call sites had drifted onto DB() before this test existed, so
// this is not a hypothetical. Nothing was wrong with them - they just
// meant no routing added to Base could be trusted to cover the
// codebase, and no reviewer could tell which half it covered.
func TestStoresDoNotReachPastTheHelpers(t *testing.T) {
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
			return nil // not our business to fail on a parse error here
		}

		ast.Inspect(file, func(n ast.Node) bool {
			// Looking for x.DB().Something(...)
			outer, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}

			inner, ok := outer.X.(*ast.CallExpr)
			if !ok {
				return true
			}

			sel, ok := inner.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "DB" || len(inner.Args) != 0 {
				return true
			}

			if allowedOnTheRawHandle[outer.Sel.Name] {
				return true
			}

			// The health probe pings Runtime.DB, which is the Database
			// interface rather than a store - there is no query to
			// route, and a readiness check genuinely wants the primary.
			if outer.Sel.Name == "PingContext" {
				return true
			}

			bad = append(bad, fset.Position(outer.Pos()).String()+": DB()."+outer.Sel.Name)

			return true
		})

		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	if len(bad) > 0 {
		t.Errorf("%d call(s) issue a statement through the raw handle instead of "+
			"Base.Exec / Query / QueryRow. Those skip Rebind and will skip read/write "+
			"routing:\n  %s", len(bad), strings.Join(bad, "\n  "))
	}
}
