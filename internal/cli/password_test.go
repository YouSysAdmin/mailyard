// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package cli

import (
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/authenticator"
)

// A password set offline is one sign-in accepts: sign-in trims what it
// is given, so the stored one must not carry an edge space.
func TestSetPasswordStoresWhatSignInSends(t *testing.T) {
	got, err := newPassword("  correct-horse-battery \n")
	if err != nil {
		t.Fatal(err)
	}

	if got != "correct-horse-battery" {
		t.Fatalf("stored %q, expected the trimmed password", got)
	}

	hash, err := authenticator.HashPassword(got)
	if err != nil {
		t.Fatal(err)
	}

	// What the login body becomes after its normalize:"trim".
	typed := strings.TrimSpace(" correct-horse-battery ")
	if !authenticator.VerifyPassword(hash, typed) {
		t.Fatal("the password set offline does not sign in")
	}
}

// The floors are counted on the trimmed password.
func TestSetPasswordLimitsCountTheTrimmedPassword(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		ok   bool
	}{
		{"spaces do not make up the floor", "   short-pw    ", false},
		{"twelve characters", "  twelve-chars  ", true},
		{"72 bytes", strings.Repeat("a", 72), true},
		{"73 bytes", strings.Repeat("a", 73), false},
		{"72 bytes inside spaces", "  " + strings.Repeat("a", 72) + "  ", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newPassword(tc.raw)
			if (err == nil) != tc.ok {
				t.Fatalf("err %v, expected ok=%v", err, tc.ok)
			}
		})
	}
}
