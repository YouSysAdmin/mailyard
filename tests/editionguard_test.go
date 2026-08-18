// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The edition split is a build tag and a naming convention, and this is
// the only thing holding the two together.
//
// Half of the checks in this package read SOURCE - they parse the
// router, walk the console, fold SQL constants - so they decide what
// belongs to which build from the FILENAME, where the compiler decides
// from the constraint. A file carrying `//go:build enterprise` under
// any other name compiles into the enterprise binary while every guard
// here judges it as community code: the router guard demands a
// permission for a route the community build does not register, or
// worse, stays silent about one it does.
//
// So the two are pinned to each other, in both directions.
//
// Two shapes exist and both are checked. A package with a community
// half splits by FILE - x_ce.go and x_ee.go beside each other. A
// package that is enterprise in its entirety lives under internal/ee
// and says so by LOCATION, where the rule is stronger: every file there
// carries the tag, or the community build compiles code that has no
// business in it.
const editionTag = "enterprise"

func TestEveryEditionFileIsNamedForItsTag(t *testing.T) {
	root := repoRoot(t)

	var problems []string
	for _, dir := range []string{"internal", "cmd", "tests", "pkg"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}

			raw, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}

			rel, _ := filepath.Rel(root, path)
			// A test file is named for its subject, so the marker sits
			// before the _test: edition_ce_test.go is a community file.
			base := strings.TrimSuffix(filepath.Base(path), "_test.go")
			isEE := strings.HasSuffix(base, "_ee") || strings.HasSuffix(base, "_ee.go")
			isCE := strings.HasSuffix(base, "_ce") || strings.HasSuffix(base, "_ce.go")
			want, has := editionConstraint(string(raw))

			// A package under internal/ee is enterprise by LOCATION, and
			// the rule there is the strong one: every file carries the
			// tag. The exception is the doc.go each of them needs, since
			// a package whose files are all excluded is refused outright
			// rather than treated as absent.
			if strings.HasPrefix(filepath.ToSlash(rel), "internal/ee/") {
				if !has && filepath.Base(path) != "doc.go" {
					problems = append(problems, rel+" is under internal/ee but carries no "+
						"//go:build "+editionTag+", so it compiles into the community build too")
				}

				if has && !want {
					problems = append(problems, rel+" is under internal/ee but is excluded BY "+
						"the tag, which is backwards")
				}

				return nil
			}

			switch {
			case has && want && !isEE:
				problems = append(problems, rel+" builds only with -tags "+editionTag+
					" but is not named _ee.go")
			case has && !want && !isCE:
				problems = append(problems, rel+" is excluded by -tags "+editionTag+
					" but is not named _ce.go")
			case !has && (isEE || isCE):
				problems = append(problems, rel+" is named for an edition but carries no "+
					"//go:build line, so it compiles into BOTH")
			}

			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", dir, err)
		}
	}

	for _, p := range problems {
		t.Errorf("edition file naming: %s", p)
	}
}

// editionConstraint reads a file's build line and reports whether it
// names the tag, and if so on which side.
//
// Deliberately crude: this repository has exactly one build tag, and a
// constraint expression combining it with something else is a thing to
// notice rather than to parse. It is read from the header only - a
// //go:build line is not one anywhere else.
func editionConstraint(src string) (enterprise, found bool) {
	for line := range strings.SplitSeq(src, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "package ") {
			return false, false
		}

		if !strings.HasPrefix(line, "//go:build") {
			continue
		}

		expr := strings.TrimSpace(strings.TrimPrefix(line, "//go:build"))
		switch expr {
		case editionTag:
			return true, true
		case "!" + editionTag:
			return false, true
		}
	}

	return false, false
}
