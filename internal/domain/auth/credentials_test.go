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
// passkey enrolment. They must ask the same question, because
// PasswordHash == "" disagrees with account_type the moment a row
// carries both a hash and an identity provider, and a gate that asks
// nothing lets an account the IdP owns enrol a second factor here.
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

		// Code only, so a comment naming the forbidden check is not a
		// violation.
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

		// The derivation must not appear alongside the gate.
		if strings.Contains(src, `PasswordHash == ""`) {
			t.Errorf(`%s still tests PasswordHash == "" - that is the check `+
				`account_type replaced, and the two disagree on an account `+
				`that has both a hash and a provider`, file)
		}
	}
}
