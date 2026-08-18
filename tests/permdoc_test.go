// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"go/ast"
	"sort"
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/apidoc"
	"github.com/yousysadmin/mailyard/internal/models/permission"
	"github.com/yousysadmin/mailyard/internal/openapi"
)

// TestDocumentedPermissionsMatchTheRouter checks the AUTHORIZATION the
// document promises against the one routes.go enforces.
//
// This is the gap the rest of the guards left. TestEveryV1RouteIsDocumented
// pins paths and the reflector pins body SHAPES - and between them, 35
// operations went on publishing "requires an API key with scope
// `read`" for a year after scopes were deleted, because nothing read
// the sentence.
//
// The one sentence in this document worth asserting. It is not
// documentation prose - it is the authorization contract, and
// `task audit-perms` provisions a credential from it on every run, so
// a route whose sentence is missing or wrong is a route the live audit
// silently skips.
//
// Wrong authorization in a document is expensive in a specific way: a
// reader provisions a credential from it, the credential is refused,
// and the API looks broken rather than the description.
func TestDocumentedPermissionsMatchTheRouter(t *testing.T) {
	enforced := routePermissions(t)
	if len(enforced) < 100 {
		t.Fatalf("only extracted %d route permissions - the routes.go parse is broken", len(enforced))
	}

	// both documents. This walked openapi.Routes() only, so the console
	// document - /app/api and /api/relay-nodes - was never checked.
	//
	// WHAT THIS DOES NOT COVER, and it is worth stating because looking
	// for it is how the limit was found: declaredPermission reads the
	// Permission FIELD first and only falls back to the sentence. So a
	// route whose field is right and whose PROSE contradicts it passes.
	// GET /data/export did exactly that - field `data:read`, description
	// opening "Behind `admin` rather than `read`", which is the deleted
	// scope vocabulary this guard was written after. Recognising that
	// needs a regex over every way of writing a permission in English,
	// which is the argument the case below already makes for why a gated
	// route must name its permission rather than describe it.
	//
	// Seven wrong sentences in the per-domain ConsoleDocs files were found
	// at the same time and are not published: consoleDocs() keeps only the
	// console-own and enrolment surfaces, and mergeDocs lets the
	// hand-written /api/v1 entry win on collision, so those strings are
	// shadowed. They were corrected in place because a reader of the
	// source reads them, and because a route that later loses its
	// hand-written entry would start publishing whatever is left there.
	documented := append(openapi.Routes(), openapi.ConsoleRoutes()...)

	var problems []string
	for _, r := range documented {
		key := r.Method + " " + apidoc.NormalizePath(r.Path)
		want, gated := enforced[key]
		got := declaredPermission(r)

		switch {
		case got == "" && gated:
			// Silence is not allowed here. A route that says nothing
			// looks merely quiet, but the alternative is prose like "any
			// signed-in member" saying the same wrong thing, and no
			// regex will recognise every way of writing one.
			//
			// So every gated route names its permission. That also
			// makes the document a complete list of what to probe,
			// which is what scripts/audit-permissions.py drives from -
			// a route with no sentence is a route nothing presses the
			// button on.
			problems = append(problems, key+" is gated on "+want+
				" but its description names no permission - say so, or the live audit skips it")
		case got != "" && !gated:
			problems = append(problems, key+" claims "+got+
				" but routes.go gates it on nothing (admin tier, or enforced in the handler)")
		case got != "" && got != want:
			problems = append(problems, key+" claims "+got+" but routes.go enforces "+want)
		}
	}

	sort.Strings(problems)
	for _, p := range problems {
		t.Errorf("the document describes an authorization the router does not apply:\n  %s", p)
	}
}

// declaredPermission reads the permission a route claims, from either
// the field or the sentence the generator writes.
func declaredPermission(r apidoc.Route) string {
	if r.Permission != "" {
		return r.Permission
	}

	// Generated entries say it in prose: "Needs the `x:y` permission."
	_, after, found := strings.Cut(r.Description, "Needs the `")
	if !found {
		return ""
	}

	perm, _, found := strings.Cut(after, "`")
	if !found {
		return ""
	}

	return perm
}

// routePermissions maps "method /path" to the "resource:action"
// routes.go actually enforces, by reading permOn on the group and
// permRead/permWrite on the route.
func routePermissions(t *testing.T) map[string]string {
	t.Helper()
	_, files := parseRouter(t, 0)
	byConst := resourceConstants(t)

	out := map[string]string{}
	for _, file := range files {
		prefix := groupPrefixes(file)

		// Group variable -> the resource its permOn names. Per file, like
		// the prefixes: a group variable is local to its function.
		groupResource := map[string]string{}
		ast.Inspect(file, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
				return true
			}

			name, ok := as.Lhs[0].(*ast.Ident)
			if !ok {
				return true
			}

			call, ok := as.Rhs[0].(*ast.CallExpr)
			if !ok {
				return true
			}

			if sel, ok := call.Fun.(*ast.SelectorExpr); !ok || sel.Sel.Name != "Group" {
				return true
			}

			if r, ok := permOnResource(call.Args, byConst); ok {
				groupResource[name.Name] = r
			}

			return true
		})

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !httpVerbs[sel.Sel.Name] {
				return true
			}

			recv, ok := sel.X.(*ast.Ident)
			if !ok || len(call.Args) == 0 {
				return true
			}

			base, known := prefix[recv.Name]
			if !known || !strings.HasPrefix(base, "/api/v1") {
				return true
			}

			p, ok := groupPath(call.Args[0])
			if !ok {
				return true
			}

			// A route may carry its own permOn instead of inheriting one.
			resource, has := permOnResource(call.Args, byConst)
			if !has {
				resource, has = groupResource[recv.Name], groupResource[recv.Name] != ""
			}
			if !has {
				return true
			}

			action := ""
			switch {
			case namesAny(call.Args, "permDelete"):
				action = "delete"
			case namesAny(call.Args, "permWrite"):
				action = "write"
			case namesAny(call.Args, "permRead"):
				action = "read"
			default:
				return true
			}

			full := apidoc.NormalizePath(strings.TrimPrefix(base+p, "/api/v1"))
			out[strings.ToUpper(sel.Sel.Name)+" "+full] = resource + ":" + action

			return true
		})
	}

	return out
}

// permOnResource pulls the resource out of a permOn(...) argument.
//
// permOnSending() takes none: it resolves emails or sandbox per
// request, from the credential. The document names the ordinary case,
// which is what an integrator reading it holds, and the prose says
// what a sandbox key is judged on instead.
func permOnResource(args []ast.Expr, byConst map[string]permission.Resource) (string, bool) {
	for _, a := range args {
		call, ok := a.(*ast.CallExpr)
		if !ok {
			continue
		}

		id, ok := call.Fun.(*ast.Ident)
		if !ok {
			continue
		}

		if id.Name == "permOnSending" {
			return string(permission.ResourceEmails), true
		}

		if id.Name != "permOn" || len(call.Args) != 1 {
			continue
		}

		sel, ok := call.Args[0].(*ast.SelectorExpr)
		if !ok {
			continue
		}

		if r, ok := byConst[sel.Sel.Name]; ok {
			return string(r), true
		}
	}

	return "", false
}
