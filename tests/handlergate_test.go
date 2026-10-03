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

	"github.com/yousysadmin/mailyard/internal/core/apidoc"
	"github.com/yousysadmin/mailyard/internal/models/permission"
	"github.com/yousysadmin/mailyard/internal/openapi"
)

// TestHandlerEnforcedRoutesSayWhatTheHandlerChecks pins the sentence on
// every /projects/:id route to the gate its handler applies.
//
// Those routes address a project by path id, so routes.go names no
// permission for them and TestDocumentedPermissionsMatchTheRouter has
// nothing to compare. The handler's first check after loadWithMember is
// the authority, and the document must state it in one of three
// sentences: a permission, ownership, or membership.
func TestHandlerEnforcedRoutesSayWhatTheHandlerChecks(t *testing.T) {
	gates := projectHandlerGates(t)
	routes := projectRouteHandlers(t)

	docs := map[string]string{}
	for _, r := range openapi.Routes() {
		docs[r.Method+" "+apidoc.NormalizePath(r.Path)] = r.Description
	}

	checked := 0
	for key, handler := range routes {
		want, gated := gates[handler]
		if !gated {
			continue
		}

		checked++

		desc, documented := docs[key]
		if !documented {
			t.Errorf("%s is not in the document", key)

			continue
		}

		if !strings.Contains(desc, want) {
			t.Errorf("%s is gated by %s on %q, but its description says %q", key, handler, want, desc)
		}
	}

	if checked < 15 {
		t.Fatalf("only checked %d handler-enforced routes - the parse is broken", checked)
	}
}

// projectHandlerGates maps a project Handler method to the sentence its
// first loadWithMember guard requires.
func projectHandlerGates(t *testing.T) map[string]string {
	t.Helper()

	byConst := resourceConstants(t)
	dir := filepath.Join(repoRoot(t), "internal", "domain", "project")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	out := map[string]string{}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Body == nil {
				continue
			}

			if gate := firstGate(fn.Body.List, byConst); gate != "" {
				out[fn.Name.Name] = gate
			}
		}
	}

	return out
}

// firstGate reads the if statement right after `... := h.loadWithMember(c)`.
func firstGate(stmts []ast.Stmt, byConst map[string]permission.Resource) string {
	for i, st := range stmts {
		as, ok := st.(*ast.AssignStmt)
		if !ok || len(as.Rhs) != 1 || i+1 >= len(stmts) {
			continue
		}

		call, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok {
			continue
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "loadWithMember" {
			continue
		}

		guard, ok := stmts[i+1].(*ast.IfStmt)
		if !ok {
			return ""
		}

		gate := ""
		ast.Inspect(guard.Cond, func(n ast.Node) bool {
			switch e := n.(type) {
			case *ast.CallExpr:
				fsel, ok := e.Fun.(*ast.SelectorExpr)
				if !ok || fsel.Sel.Name != "Has" || len(e.Args) != 2 {
					return true
				}

				res, rok := e.Args[0].(*ast.SelectorExpr)
				act, aok := e.Args[1].(*ast.SelectorExpr)
				if !rok || !aok {
					return true
				}

				r, known := byConst[res.Sel.Name]
				if !known {
					return true
				}

				a := strings.ToLower(strings.TrimPrefix(act.Sel.Name, "Action"))
				gate = "Requires `" + string(r) + ":" + a + "` in the project the path names"
			case *ast.UnaryExpr:
				field, ok := e.X.(*ast.SelectorExpr)
				if !ok || e.Op != token.NOT {
					return true
				}

				switch field.Sel.Name {
				case "owner":
					gate = "Project owners only."
				case "member":
					gate = "Any member of the project"
				}
			}

			return true
		})

		return gate
	}

	return ""
}

// projectRouteHandlers maps "METHOD /path" to the project Handler
// method routes.go registers for it.
func projectRouteHandlers(t *testing.T) map[string]string {
	t.Helper()

	_, files := parseRouter(t, 0)
	out := map[string]string{}
	for _, file := range files {
		prefix := groupPrefixes(file)
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				return true
			}

			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !httpVerbs[sel.Sel.Name] {
				return true
			}

			recv, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}

			base, known := prefix[recv.Name]
			if !known || !strings.HasPrefix(base, "/api/v1/projects") {
				return true
			}

			h, ok := call.Args[len(call.Args)-1].(*ast.SelectorExpr)
			if !ok {
				return true
			}

			p, ok := groupPath(call.Args[0])
			if !ok {
				return true
			}

			full := apidoc.NormalizePath(strings.TrimPrefix(base+p, "/api/v1"))
			out[strings.ToUpper(sel.Sel.Name)+" "+full] = h.Sel.Name

			return true
		})
	}

	return out
}
