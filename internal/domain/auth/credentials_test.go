// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Four places decide whether an account may manage its own sign-in:
// the password change, the password reset request, TOTP setup and
// passkey enrolment. They must ask the same question.
//
// Three of them did not exist or asked a different one. Passkeys
// tested PasswordHash == "" directly, which is the derivation
// account_type replaced, and it disagrees with the others the moment a
// row carries both a hash and an identity provider. The reset request
// tested the same thing. TOTP tested nothing at all, so an account the
// IdP owns could enrol a second factor here. And the password change
// had no handler, which is the bug that started this: an admin created
// an ordinary local account and its owner was told to ask an
// administrator.
func TestOneAnswerToWhoManagesTheirOwnCredentials(t *testing.T) {
	// The gate and the file that must ask it.
	const gate = "ManagesOwnCredentials()"
	sites := map[string]string{
		"passwordreset.go": "the password change and the reset request",
		"totp.go":          "two-factor setup",
		"passkey.go":       "passkey enrolment",
	}

	for file, what := range sites {
		body, err := os.ReadFile(filepath.Join(".", file))
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}

		// Code only. The comments explain what the old check was, and
		// a check that reads its own explanation as a violation is one
		// people delete.
		var code []string
		for line := range strings.SplitSeq(string(body), "\n") {
			if t := strings.TrimSpace(line); !strings.HasPrefix(t, "//") {
				code = append(code, line)
			}
		}

		src := strings.Join(code, "\n")
		if !strings.Contains(src, gate) {
			t.Errorf("%s gates %s without calling %s", file, what, gate)
		}

		// The derivation it replaced must not come back alongside it.
		if strings.Contains(src, `PasswordHash == ""`) {
			t.Errorf(`%s still tests PasswordHash == "" - that is the check `+
				`account_type replaced, and the two disagree on an account `+
				`that has both a hash and a provider`, file)
		}
	}
}
