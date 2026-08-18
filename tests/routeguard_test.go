// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/internal/models/permission"
)

// httpVerbs are the route-registration methods on a fiber.Router.
var httpVerbs = map[string]bool{
	"Get": true, "Post": true, "Put": true, "Patch": true, "Delete": true,
	"Head": true, "Options": true, "All": true,
}

// TestEveryProjectRouteDeclaresAPermission is what the whole scheme
// rests on.
//
// Authorization is declared in two halves - a resource on the group,
// an action on the route - and neither half is enforced by the type
// system. A group registered without permOn resolves a project and
// then admits any member to everything under it. A route registered
// without permRead, permWrite or permDelete inherits only the group's "may touch
// this resource at all" check, so a viewer holding emails:read would
// reach a send.
//
// Both mistakes read as working code, both produce a route that
// serves 200, and neither shows up in a test of the handler. This is
// the only thing that catches them, so it is deliberately strict: it
// walks the real routes.go, not a list somebody maintains.
//
// It replaces TestOnlySandboxSkipsTheViewerFloor, which guarded the
// single allowlisted prefix permitted to skip the old viewer floor.
// There is no floor now - the permission IS the floor, and it differs
// per resource, which is the point.
// handlerEnforcedGroups are the v1 groups and receivers that decide
// authorization inside the handler, and why.
//
// Every entry addresses a project by path id, so it cannot use the
// project the credential resolved - the caller is asking about a
// different one, or about no project at all. They ask the same
// catalogue through project.can(), from the same membership read, and
// TestRouteResourcesExistInTheCatalogue records the resources they
// enforce.
//
// A group reaches this map by being unable to express its check as
// permOn, not by being awkward to annotate.
var handlerEnforcedGroups = map[string]string{
	"v1":   "the surface itself, not a resource group - its own routes are exempted by name below",
	"proj": "/projects addresses a project by path id, so the header-resolved one is the wrong project to check",
}

// handlerEnforcedRoutes are individual routes on the v1 group with the
// same excuse.
var handlerEnforcedRoutes = map[string]string{
	"GET /permissions":                 "the static catalogue - no project, and every authenticated caller may read it",
	"POST /invitations/:token/accept":  "the token names the project, and the caller is not a member yet",
	"POST /invitations/:token/decline": "same",
}

func TestEveryProjectRouteDeclaresAPermission(t *testing.T) {
	fset, files := parseRouter(t, parser.ParseComments)

	var undeclared, missingAction []string
	// Groups counted across the package, but RESOLVED per file: a group
	// variable is local to the function that opened it, so two files may
	// each have a `v1` meaning different mounts.
	var groups int
	for _, file := range files {
		tenantGroup, u, m := projectGroupsIn(t, fset, file)
		groups += len(tenantGroup)
		undeclared = append(undeclared, u...)
		missingAction = append(missingAction, m...)
	}

	if groups < 15 {
		t.Fatalf("only found %d project route groups, the router parse is broken", groups)
	}

	sort.Strings(undeclared)
	sort.Strings(missingAction)

	reportRouteProblems(t, undeclared, missingAction)
}

// projectGroupsIn runs both passes over one file of the router.
func projectGroupsIn(t *testing.T, fset *token.FileSet, file *ast.File) (map[string]bool, []string, []string) {
	t.Helper()

	// Group variable -> whether its construction included permOn.
	tenantGroup := map[string]bool{}
	var undeclared []string

	// Pass one: find every group that governs a project.
	//
	// Identified by sitting under /api/v1 rather than by calling
	// requireProject, which these groups no longer do - machineAuth
	// resolves the project once, for the whole surface, from either
	// credential. Everything on that prefix is tenant surface by
	// construction, which is what makes "declares no permOn" a
	// buildable error rather than a convention.
	prefix := groupPrefixes(file)
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

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Group" {
			return true
		}

		if !strings.HasPrefix(prefix[name.Name], "/api/v1") {
			return true
		}

		// Platform administration is a different tier and says so with
		// requireAdmin on the /admin group above it. It governs the
		// INSTALLATION, not a project, so there is no resource for it
		// to name - permOn would have to invent one.
		if strings.HasPrefix(prefix[name.Name], "/api/v1/admin") {
			return true
		}

		if why, ok := handlerEnforcedGroups[name.Name]; ok {
			t.Logf("authorization enforced in handlers: %s (%s)", name.Name, why)

			return true
		}

		// permOnSending counts: it declares the resource too, it just
		// resolves it per request - emails for an ordinary credential,
		// sandbox for one whose mail is captured.
		hasPerm := callsAny(call.Args, "permOn", "permOnSending")
		tenantGroup[name.Name] = hasPerm
		if !hasPerm {
			undeclared = append(undeclared, fset.Position(call.Pos()).String()+
				": group "+name.Name+" ("+firstStringArg(call)+") is on the project surface but declares no permOn")
		}

		return true
	})

	// Pass two: every route on one of those groups needs an action,
	// and every standalone project route needs both halves inline.
	var missingAction []string
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
		if !ok {
			return true
		}

		where := fset.Position(call.Pos()).String() + ": " + recv.Name + "." + sel.Sel.Name + "(" + firstStringArg(call) + ")"

		if _, handlerEnforced := handlerEnforcedGroups[recv.Name]; handlerEnforced {
			// Its routes are the handler's business, except the ones on
			// the bare v1 group, which are checked just below.
			if recv.Name != "v1" {
				return true
			}
		}

		if _, isTenant := tenantGroup[recv.Name]; isTenant {
			if !namesAny(call.Args, "permRead", "permWrite", "permDelete") {
				missingAction = append(missingAction, where)
			}

			return true
		}

		// A route hung straight off the v1 group carries its own permOn
		// plus an action.
		if strings.HasPrefix(prefix[recv.Name], "/api/v1/admin") {
			return true
		}

		if strings.HasPrefix(prefix[recv.Name], "/api/v1") {
			key := strings.ToUpper(sel.Sel.Name) + " " + firstStringArg(call)
			if why, ok := handlerEnforcedRoutes[key]; ok {
				t.Logf("authorization enforced in handlers: %s (%s)", key, why)

				return true
			}

			if !callsAny(call.Args, "permOn", "permOnSending") ||
				!namesAny(call.Args, "permRead", "permWrite", "permDelete") {
				missingAction = append(missingAction, where+" [standalone]")
			}
		}

		return true
	})

	return tenantGroup, undeclared, missingAction
}

func reportRouteProblems(t *testing.T, undeclared, missingAction []string) {
	t.Helper()

	if len(undeclared) > 0 {
		t.Errorf("%d route group(s) resolve a project without declaring a resource:\n  %s\n"+
			"Such a group admits any member to everything under it. Add permOn(perm.Resource...) "+
			"to the Group() call, and the resource to internal/models/permission if it is new.",
			len(undeclared), strings.Join(undeclared, "\n  "))
	}

	if len(missingAction) > 0 {
		t.Errorf("%d project route(s) declare no action:\n  %s\n"+
			"Without permRead, permWrite or permDelete the route inherits only the group's "+
			"may-touch-this-resource check, so a read-only caller reaches a write. "+
			"Add exactly one of them.",
			len(missingAction), strings.Join(missingAction, "\n  "))
	}
}

// TestRouteResourcesExistInTheCatalogue keeps routes.go and the
// catalogue from drifting apart in the direction the other test
// cannot see: a permOn naming a resource nobody defined.
//
// permOn panics at boot on an unknown resource, which is the right
// runtime behaviour and a poor way to find out.
func TestRouteResourcesExistInTheCatalogue(t *testing.T) {
	fset, files := parseRouter(t, 0)

	// Constant name (ResourceEmails) -> resource value, read out of
	// the permission package's own source.
	//
	// Derived, not guessed. Title-casing the resource string to recover
	// the constant name gets ResourceEmails right and ResourceSMTP,
	// ResourceAPIKeys and ResourceData wrong -
	// and a guard that mis-resolves a name reports a drift that is not
	// there, which is worse than not checking.
	byConst := resourceConstants(t)

	used := map[permission.Resource]bool{}
	var unknown []string
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			id, ok := call.Fun.(*ast.Ident)
			if !ok || id.Name != "permOn" || len(call.Args) != 1 {
				return true
			}

			sel, ok := call.Args[0].(*ast.SelectorExpr)
			if !ok {
				unknown = append(unknown, fset.Position(call.Pos()).String()+": permOn with a non-constant resource")

				return true
			}

			r, ok := byConst[sel.Sel.Name]
			if !ok {
				unknown = append(unknown, fset.Position(call.Pos()).String()+": permOn(perm."+sel.Sel.Name+") names no catalogue entry")

				return true
			}

			used[r] = true

			return true
		})
	}

	if len(unknown) > 0 {
		t.Errorf("the router names %d resource(s) the catalogue does not define:\n  %s",
			len(unknown), strings.Join(unknown, "\n  "))
	}

	// And the other direction: a catalogue entry no route enforces is
	// a permission the console offers and nothing honours - the same
	// rule the settings registry keeps ("a setting nothing reads is a
	// lie to the operator").
	//
	// Two are enforced elsewhere and say so here. The /api/projects
	// routes address a project by path id, so they cannot go through
	// requireProject - which resolves it from a header - and decide
	// authorization inside the handler instead, through the same
	// catalogue via project.can(). Anything added to this map needs
	// that kind of reason, not a shrug.
	enforcedInHandlers := map[permission.Resource]string{
		permission.ResourceMembers:  "internal/domain/project/endpoint.go and group_endpoint.go - member, invitation and custom-group handlers",
		permission.ResourceSettings: "internal/domain/project/endpoint.go, Update and GetSSO",
		// Relay enrolment is the one route a node reaches before it has
		// any identity: POST /api/relay-nodes/register is public and
		// authenticates the api key itself, so there is no middleware
		// chain to hang permOn from. The check is one line in
		// relaynode/endpoint.go and asks this same catalogue.
		permission.ResourceRelay: "internal/domain/relaynode/endpoint.go, the public register route authenticating its own key",
	}
	var unenforced []string
	for _, d := range permission.Registry {
		if !used[d.Resource] && enforcedInHandlers[d.Resource] == "" {
			unenforced = append(unenforced, string(d.Resource))
		}
	}

	sort.Strings(unenforced)
	if len(unenforced) > 0 {
		t.Errorf("catalogue defines %v, which no route group enforces.\n"+
			"A permission nothing honours is a promise to whoever reads the grid. "+
			"Remove it, or declare it on the group that should have had it.", unenforced)
	}
}

// resourceConstants reads `ResourceX Resource = "y"` out of the
// permission package.
func resourceConstants(t *testing.T) map[string]permission.Resource {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(repoRoot(t), "internal", "models", "permission", "permission.go"), nil, 0)
	if err != nil {
		t.Fatalf("parse the permission catalogue: %v", err)
	}

	out := map[string]permission.Resource{}
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

			id, ok := vs.Type.(*ast.Ident)
			if !ok || id.Name != "Resource" {
				continue
			}

			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}

				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}

				v, uerr := strconv.Unquote(lit.Value)
				if uerr != nil {
					continue
				}

				out[name.Name] = permission.Resource(v)
			}
		}
	}

	if len(out) < 10 {
		t.Fatalf("only found %d resource constants, the catalogue parse is broken", len(out))
	}

	return out
}

// callsAny reports whether any argument is a call to one of names.
func callsAny(args []ast.Expr, names ...string) bool {
	for _, arg := range args {
		call, ok := arg.(*ast.CallExpr)
		if !ok {
			continue
		}

		id, ok := call.Fun.(*ast.Ident)
		if !ok {
			continue
		}

		if slices.Contains(names, id.Name) {
			return true
		}
	}

	return false
}

// namesAny reports whether any argument IS one of the identifiers -
// permRead, permWrite and permDelete are handlers passed by name,
// not called.
func namesAny(args []ast.Expr, names ...string) bool {
	for _, arg := range args {
		id, ok := arg.(*ast.Ident)
		if !ok {
			continue
		}

		if slices.Contains(names, id.Name) {
			return true
		}
	}

	return false
}

// firstStringArg returns the route path a registration was given.
func firstStringArg(call *ast.CallExpr) string {
	if len(call.Args) == 0 {
		return ""
	}

	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}

	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return ""
	}

	return s
}

// TestBothSurfacesResolveAccessOneWay pins the shape phase 2 exists to
// create: the machine surface must not grow its own authentication or
// tenancy resolution.
//
// The two surfaces answer the same questions - who is this, which
// project, what may they do - and they answered them in two places
// before, which is how /api/v1 ended up resolving every caller to the
// owner preset while /api resolved a real membership. The guard is
// structural because the drift is structural: a second resolver
// compiles, serves 200, and differs only in what it permits.
func TestBothSurfacesResolveAccessOneWay(t *testing.T) {
	fset := token.NewFileSet()
	//nolint:staticcheck // deprecated for build-tag handling these guards do not want
	pkg, err := parser.ParseDir(fset, serverDir(t), func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package: %v", err)
	}

	// Who calls each stamp helper, by enclosing function name.
	callers := map[string][]string{}
	for _, p := range pkg {
		for _, file := range p.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				fn, ok := n.(*ast.FuncDecl)
				if !ok {
					return true
				}

				ast.Inspect(fn, func(m ast.Node) bool {
					call, ok := m.(*ast.CallExpr)
					if !ok {
						return true
					}

					id, ok := call.Fun.(*ast.Ident)
					if !ok {
						return true
					}

					switch id.Name {
					case "stampAPIKey", "stampSession", "stampProject":
						callers[id.Name] = append(callers[id.Name], fn.Name.Name)
					}

					return true
				})

				return false
			})
		}
	}

	// projectAccessOf is where a role and a permission set are decided.
	// Exactly one caller, or the two surfaces can disagree about what a
	// membership grants.
	deciders := 0
	for _, p := range pkg {
		for _, file := range p.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}

				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "projectAccessOf" {
					deciders++
				}

				return true
			})
		}
	}

	if deciders != 2 {
		t.Errorf("projectAccessOf has %d call sites, expected the 2 inside stampProject "+
			"(header path and default-project fallback). A third means somebody is "+
			"resolving a caller's permissions somewhere else.", deciders)
	}

	for _, helper := range []string{"stampAPIKey", "stampSession", "stampProject"} {
		if !slices.Contains(callers[helper], "machineAuth") {
			t.Errorf("machineAuth does not call %s - the /api/v1 gate is not resolving "+
				"callers through the shared helpers", helper)
		}
	}

	if !slices.Contains(callers["stampSession"], "requireAuth") {
		t.Error("requireAuth does not call stampSession - the console gate has its own copy")
	}

	if !slices.Contains(callers["stampProject"], "requireProject") {
		t.Error("requireProject does not call stampProject - the console gate has its own copy")
	}
}

// TestTheMachineSurfaceAcceptsBothCredentials fails if /api/v1 is
// remounted on a single-credential gate.
//
// Reverting it to a key-only gate is a one-word change that compiles
// and passes every other test, and the symptom is the console losing
// access to routes it must reach.
func TestTheMachineSurfaceAcceptsBothCredentials(t *testing.T) {
	_, files := parseRouter(t, 0)
	found := false
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Group" || len(call.Args) == 0 {
				return true
			}

			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok {
				return true
			}

			if p, _ := strconv.Unquote(lit.Value); p != "/api/v1" {
				return true
			}

			for _, arg := range call.Args[1:] {
				if c, ok := arg.(*ast.CallExpr); ok {
					if id, ok := c.Fun.(*ast.Ident); ok && id.Name == "machineAuth" {
						found = true
					}
				}
			}

			return true
		})
	}

	if !found {
		t.Error("the /api/v1 group is not mounted with machineAuth. That gate is what lets " +
			"a session reach the machine surface - without it the console can only talk " +
			"to routes that were never moved there.")
	}
}
