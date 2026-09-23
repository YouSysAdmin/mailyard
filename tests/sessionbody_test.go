// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"go/ast"
	"testing"
)

// bodilessOpenRoutes are the open console POSTs that read no body, so
// there is no form-encoded payload for requireJSONBody to refuse.
var bodilessOpenRoutes = map[string]string{
	"/auth/logout":              "clears the caller's own cookie and reads nothing",
	"/auth/passkey/login/begin": "reads nothing and answers a challenge, the session starts at finish",
}

// TestEveryOpenConsolePostRequiresAJSONBody keeps login CSRF closed.
//
// A cross-site form cannot carry the session cookie, which is also why
// refuseCrossSite does not look at it, but it can still be the request
// that SETS one: a form POSTing a text/plain body shaped like JSON to a
// route that starts a session signs the victim into the attacker's
// account. An HTML form cannot produce application/json, so the media
// type is the whole defence, and it is only worth anything on every
// open route at once.
func TestEveryOpenConsolePostRequiresAJSONBody(t *testing.T) {
	_, files := parseRouter(t, 0)

	seen := map[string]bool{}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Post" {
				return true
			}

			if recv, ok := sel.X.(*ast.Ident); !ok || recv.Name != "appAPI" {
				return true
			}

			path := firstStringArg(call)
			seen[path] = true
			if callsAny(call.Args, "requireAuth") {
				return true
			}

			if _, ok := bodilessOpenRoutes[path]; ok {
				return true
			}

			if !namesAny(call.Args, "requireJSONBody") {
				t.Errorf("POST %s is open and reads a body, so it needs requireJSONBody", path)
			}

			return true
		})
	}

	for path := range bodilessOpenRoutes {
		if !seen[path] {
			t.Errorf("bodilessOpenRoutes names %s, which is not an appAPI POST any more", path)
		}
	}

	if !seen["/auth/login"] {
		t.Fatal("found no appAPI.Post(\"/auth/login\") - the walk is not reaching the router")
	}
}

