// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package auth

import (
	"strings"
	"testing"
)

// A set is ten distinct codes in the spelling a person can copy from a
// printout, and the hash forgives how they typed it back.
func TestRecoveryCodesAreDistinctAndForgivinglyHashed(t *testing.T) {
	codes, hashes, err := newRecoveryCodes()
	if err != nil {
		t.Fatal(err)
	}

	if len(codes) != recoveryCodeCount || len(hashes) != recoveryCodeCount {
		t.Fatalf("got %d codes and %d hashes", len(codes), len(hashes))
	}

	seen := map[string]bool{}
	for i, c := range codes {
		if len(c) != recoveryCodeLen+1 || c[recoveryCodeLen/2] != '-' {
			t.Errorf("code %q is not xxxxx-xxxxx", c)
		}

		if !looksLikeRecoveryCode(c) {
			t.Errorf("code %q does not look like one to the sign-in check", c)
		}

		if seen[c] {
			t.Errorf("code %q minted twice", c)
		}

		seen[c] = true
		typed := " " + strings.ToUpper(strings.ReplaceAll(c, "-", "")) + " "
		if hashRecoveryCode(typed) != hashes[i] {
			t.Errorf("hash of %q typed as %q differs", c, typed)
		}
	}

	for _, not := range []string{"123456", "abcde-fghjk-m", "abcd0-12345", ""} {
		if looksLikeRecoveryCode(not) {
			t.Errorf("%q was taken for a recovery code", not)
		}
	}
}
