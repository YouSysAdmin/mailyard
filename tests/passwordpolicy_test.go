// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every route that SETS a password carries the same minimum length.
//
// A struct tag cannot name a constant, so the floor is a number written
// into each tag, and they drifted: self-signup accepted eight characters
// where reset and change required twelve, so the weakest floor sat on
// the one route reachable without a credential. The tags that set a
// password are the ones carrying bcryptlen - the ones that merely CHECK
// one (login, re-auth) are min=1 on purpose, so nothing leaks the
// policy to a guesser.
func TestEveryPasswordSettingRouteSharesOneFloor(t *testing.T) {
	const want = "min=12,"
	tag := regexp.MustCompile(`json:"password"\s+validate:"([^"]*bcryptlen[^"]*)"`)
	found := 0
	err := filepath.WalkDir(filepath.Join(goTree(t), "domain"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, "types.go") {
			return err
		}

		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		for _, m := range tag.FindAllStringSubmatch(string(src), -1) {
			found++
			if !strings.Contains(m[1], want) {
				t.Errorf("%s: a password-setting tag reads %q, want %s - the floor is one number everywhere",
					path, m[1], strings.TrimSuffix(want, ","))
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if found < 4 {
		t.Fatalf("found %d password-setting tags, want at least the four this repository has - the walk is not reaching them", found)
	}
}
