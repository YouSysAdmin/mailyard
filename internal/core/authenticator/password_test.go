// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package authenticator

import (
	"strings"
	"testing"
)

// A password longer than bcrypt reads is cut, not refused, and the
// whole of it keeps working wherever it is typed.
func TestALongPasswordIsCutNotRefused(t *testing.T) {
	cases := map[string]string{
		"ascii":                       strings.Repeat("correct-horse-", 18),
		"cyrillic, cut inside a rune": strings.Repeat("пароль", 20) + "ж",
	}

	for name, pw := range cases {
		t.Run(name, func(t *testing.T) {
			if len(pw) <= bcryptMaxBytes {
				t.Fatalf("fixture is %d bytes, not past the cut", len(pw))
			}

			hash, err := HashPassword(pw)
			if err != nil {
				t.Fatalf("a %d byte password was refused: %v", len(pw), err)
			}

			if !VerifyPassword(hash, pw) {
				t.Fatal("the whole password does not sign in")
			}

			// What bcrypt never reads does not decide anything.
			if !VerifyPassword(hash, pw[:bcryptMaxBytes]+"something else") {
				t.Fatal("the bytes past the cut changed the answer")
			}

			// What it does read still does.
			wrong := "X" + pw[1:]
			if VerifyPassword(hash, wrong) {
				t.Fatal("a password differing inside the first 72 bytes signed in")
			}
		})
	}
}
