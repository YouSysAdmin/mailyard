// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"go/ast"
	"go/token"
	"strconv"
	"testing"
)

// namesCall reports whether any of these route arguments is a call to
// the named plain function, as `requireAuth(rt)` is.
//
// The AST directly rather than through render(), which collapses a call
// on a bare identifier to "call(...)" - it only spells out the
// selector-based ones. Reading the Fun ident is both exact and immune to
// that helper changing for sqlguard's benefit.
func namesCall(args []ast.Expr, fn string) bool {
	for _, arg := range args {
		call, ok := arg.(*ast.CallExpr)
		if !ok {
			continue
		}

		if id, ok := call.Fun.(*ast.Ident); ok && id.Name == fn {
			return true
		}
	}

	return false
}

// consoleMutations returns every mutating registration on the console
// prefix, as "method /path", with whether it is behind the session gate
// and whether it honours maintenance mode.
type consoleMutation struct {
	what        string
	authed      bool
	maintenance bool
}

func consoleMutations(t *testing.T) []consoleMutation {
	t.Helper()

	_, files := parseRouter(t, 0)
	mutating := map[string]bool{"Post": true, "Put": true, "Patch": true, "Delete": true}

	var out []consoleMutation
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !mutating[sel.Sel.Name] {
				return true
			}

			if recv, ok := sel.X.(*ast.Ident); !ok || recv.Name != "appAPI" {
				return true
			}

			if len(call.Args) == 0 {
				return true
			}

			path := "(unreadable path)"
			if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if p, err := strconv.Unquote(lit.Value); err == nil {
					path = p
				}
			}

			rest := call.Args[1:]
			out = append(out, consoleMutation{
				what:        sel.Sel.Name + " " + path,
				authed:      namesCall(rest, "requireAuth"),
				maintenance: namesCall(rest, "maintenanceMode"),
			})

			return true
		})
	}

	return out
}

// An authenticated mutation on the console prefix carries
// maintenanceMode, or parking the platform does not park it.
//
// The mode gated /api/v1 and nothing else, so an operator who switched
// it on to run a migration still had the console writing to `users`,
// `sessions` and `user_passkeys` through the credential routes - a
// password change, a passkey enrolment, a session revocation. Those are
// exactly the racing writes the mode exists to stop, and the platform
// notes described it as refusing mutating requests, which was true of
// one surface out of two.
//
// requireAuth is the discriminator, and it is the honest one rather than
// a path list. The OPEN ceremonies on this prefix must stay open: an
// administrator signs in to reach the switch that turns the mode off, so
// gating login would lock the installation with no way back. Everything
// behind the session gate has a request context, which is what the
// platform-admin exemption inside maintenanceMode reads - so the same
// property that makes a route gateable makes it exemptible.
func TestEveryAuthenticatedConsoleMutationHonoursMaintenanceMode(t *testing.T) {
	routes := consoleMutations(t)

	// A walker that matches nothing passes, and reads exactly like a
	// repository with no violations. These routes exist, so no matches
	// means the walk is broken rather than the code clean.
	if len(routes) == 0 {
		t.Fatal("found no mutating console routes at all, so this guard is not reading routes.go")
	}

	var gated int
	for _, r := range routes {
		if !r.authed {
			// An open ceremony. Deliberately ungated - see above.
			continue
		}

		gated++

		if !r.maintenance {
			t.Errorf("%s is an authenticated mutation on the console prefix with no "+
				"maintenanceMode(rt) - add it after requireAuth(rt), or the platform "+
				"cannot actually be parked", r.what)
		}
	}

	if gated == 0 {
		t.Error("no authenticated console mutations were recognised, so this guard checks nothing")
	}
}

// The other direction: the sign-in ceremonies must not be gated.
//
// Stated as a test because it is the half that is easy to "fix" wrongly.
// Adding maintenanceMode to /auth/login looks like consistency and locks
// every administrator out of the switch that turns the mode off - the
// exemption cannot save them, because it is read from a request context
// that only exists once they are already signed in.
func TestSignInCeremoniesIgnoreMaintenanceMode(t *testing.T) {
	open := map[string]bool{
		"Post /auth/login":                  true,
		"Post /auth/logout":                 true,
		"Post /auth/password-reset/request": true,
		"Post /auth/password-reset/confirm": true,
		"Post /auth/register":               true,
		"Post /auth/verify-email":           true,
		"Post /auth/verify-email/resend":    true,
		"Post /auth/passkey/login/begin":    true,
		"Post /auth/passkey/login/finish":   true,
	}

	var seen int
	for _, r := range consoleMutations(t) {
		if !open[r.what] {
			continue
		}

		seen++

		if r.maintenance {
			t.Errorf("%s is gated by maintenanceMode - during maintenance nobody could sign in, "+
				"including the administrator who has to turn it off", r.what)
		}
	}

	// Registration is conditional on the operator opting in, so it may be
	// absent. The rest are unconditional, so a low count means the names
	// above have drifted from routes.go.
	if seen < len(open)-1 {
		t.Errorf("recognised %d of the %d open ceremonies - the path list has drifted from routes.go",
			seen, len(open))
	}
}
