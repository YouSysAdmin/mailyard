// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// passwordCheckers are the bodies that CHECK an account password rather
// than set one. They take min=1 on purpose, so nothing leaks the policy
// to a guesser.
var passwordCheckers = map[string]bool{
	"loginInput":         true,
	"passkeyReauthInput": true,
	"totpSetupInput":     true,
}

// Every route that SETS an account password carries the same minimum
// length.
//
// A struct tag cannot name a constant, so the floor is a number written
// into each tag, and copies drift - the weakest one must not land on the
// route reachable without a credential. An account password field is
// one that sets it unless its body is named in passwordCheckers, so a
// new setter cannot slip in with min=1 unnoticed.
func TestEveryPasswordSettingRouteSharesOneFloor(t *testing.T) {
	const floor = "min=12,"
	structDecl := regexp.MustCompile(`^type (\w+) struct`)
	tag := regexp.MustCompile(`json:"password"\s+validate:"([^"]*)"`)
	setters := 0
	for _, pkg := range []string{"auth", "user"} {
		path := filepath.Join(goTree(t), "domain", pkg, "types.go")
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}

		body := ""
		for line := range strings.SplitSeq(string(src), "\n") {
			if m := structDecl.FindStringSubmatch(line); m != nil {
				body = m[1]
				continue
			}

			m := tag.FindStringSubmatch(line)
			if m == nil {
				continue
			}

			if passwordCheckers[body] {
				if !strings.Contains(m[1], "min=1,") {
					t.Errorf("%s %s checks a password and reads %q, want min=1", pkg, body, m[1])
				}

				continue
			}

			setters++
			if !strings.Contains(m[1], floor) {
				t.Errorf("%s %s sets a password and reads %q, want %s - the floor is one number everywhere",
					pkg, body, m[1], strings.TrimSuffix(floor, ","))
			}
		}
	}

	if setters < 5 {
		t.Fatalf("found %d password-setting fields, want the five this repository has - the walk is not reaching them", setters)
	}
}
