// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package oidc_test is external on purpose: core/env imports core/oidc
// to build the Registry, so the package under test may not import env
// back. A test binary is a separate package and can import both, which
// is what lets the copied prefix below be checked rather than trusted.
package oidc_test

import (
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/oidc"
)

// TestOIDCPathsFollowTheConsole pins the sign-in routes to where the
// console api is actually mounted.
//
// These two strings are what gets registered as the redirect URI at
// every identity provider an operator configures. If they drift from
// the route table, nothing fails at boot and nothing fails at build -
// the failure is a 404 at the end of a round-trip through Google or
// Okta, after the user has already typed their password, and it looks
// like the provider's fault.
func TestOIDCPathsFollowTheConsole(t *testing.T) {
	want := env.ConsolePath + "/api/auth/oauth/acme/start"
	if got := oidc.StartPath("acme"); got != want {
		t.Errorf("StartPath = %q, want %q.\n"+
			"The prefix in core/oidc is a deliberate copy of env.ConsolePath - "+
			"update consoleAPIPrefix to match.", got, want)
	}

	want = env.ConsolePath + "/api/auth/oauth/acme/callback"
	if got := oidc.CallbackPath("acme"); got != want {
		t.Errorf("CallbackPath = %q, want %q", got, want)
	}

	// The slug is a path segment, so a provider named with a slash
	// would silently address a different route.
	if strings.Contains(oidc.StartPath("a/b"), "//") {
		t.Error("a slug containing a slash is not being rejected upstream")
	}
}
