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
		groups := strings.Split(c, "-")
		if len(groups) != recoveryCodeLen/recoveryGroup {
			t.Errorf("code %q is not xxxx-xxxx-xxxx-xxxx", c)
		}

		for _, g := range groups {
			if len(g) != recoveryGroup {
				t.Errorf("code %q is not xxxx-xxxx-xxxx-xxxx", c)
			}
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

	// A set printed under the old length still signs in.
	if !looksLikeRecoveryCode("abcde-fghjk") {
		t.Error("a ten-symbol code from an older set was not recognised")
	}
}

// Every symbol gets exactly the same share of the byte space, which is
// what the rejection sampler is for - the modulo it replaced gave the
// first eight letters an extra chance each.
func TestRecoverySymbolsAreUnbiased(t *testing.T) {
	counts := map[byte]int{}
	rejected := 0
	for r := 0; r < 256; r++ {
		sym, ok := recoverySymbol(byte(r))
		if !ok {
			rejected++
			continue
		}

		counts[sym]++
	}

	if len(counts) != len(recoveryAlphabet) {
		t.Fatalf("%d symbols reachable, want %d", len(counts), len(recoveryAlphabet))
	}

	for sym, n := range counts {
		if n != 8 {
			t.Errorf("symbol %q has %d preimages, want 8", sym, n)
		}
	}

	if rejected != 256%len(recoveryAlphabet) {
		t.Errorf("%d bytes rejected, want %d", rejected, 256%len(recoveryAlphabet))
	}
}
