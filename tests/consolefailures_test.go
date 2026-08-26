// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// A rule about the console, checked by reading web/src.

package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// A failure being written to the browser's own console.
var writesToTheBrowserConsole = regexp.MustCompile(`\bconsole\.(log|debug|info|warn|error)\(`)

// A FAILURE IS REPORTED TO THE PERSON, NOT TO THE BROWSER CONSOLE.
//
// Nobody has devtools open. A `catch` whose whole body is console.error
// produces a page that looks like it finished: the request failed, the
// view kept its empty state, and the empty state is a sentence about
// there being nothing here yet.
//
// Two did exactly that. Opening a message whose request failed left
// "Not found" on screen, indistinguishable from one that had been
// deleted - and it polls every three seconds, so the log filled while
// the page said nothing. The compose form swallowed the same failure and
// showed an empty template picker, which reads as "this project has no
// templates".
//
// The exception is where the silence is the DESIGN and is written down
// beside it: the tracked-link lookup on a message detail falls back to
// rendering the body with its hrefs stripped, which is the documented
// behaviour and not something to interrupt a reader over.
func TestAFailureIsReportedToThePerson(t *testing.T) {
	root := consoleSrc(t)

	// Keyed by file, valued with why. A file listed here still has to
	// contain a console call, or the entry has outlived what it excused.
	quietOnPurpose := map[string]string{
		"views/emails/EmailDetail.vue": "the tracked-link lookup degrades to a body with its " +
			"hrefs stripped, which is the documented fallback",
	}

	used := map[string]bool{}
	var findings []string

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		if !strings.HasSuffix(path, ".vue") && !strings.HasSuffix(path, ".ts") {
			return nil
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		rel := filepath.ToSlash(strings.TrimPrefix(path, root+string(filepath.Separator)))
		for i, line := range strings.Split(string(body), "\n") {
			if !writesToTheBrowserConsole.MatchString(line) {
				continue
			}

			if _, ok := quietOnPurpose[rel]; ok {
				used[rel] = true

				continue
			}

			findings = append(findings, rel+":"+strconv.Itoa(i+1)+" "+strings.TrimSpace(line))
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	slices.Sort(findings)

	if len(findings) > 0 {
		t.Errorf("%d place(s) report a failure to the browser console:\n  %s\n"+
			"say it with notify.error(apiErrorMessage(e, '...')) - nobody has devtools open, "+
			"and a view that logs and carries on shows its empty state instead, which reads "+
			"as an answer rather than as a failure",
			len(findings), strings.Join(findings, "\n  "))
	}

	for rel, why := range quietOnPurpose {
		if !used[rel] {
			t.Errorf("%s is excused from reporting failures (%s) and no longer writes to the "+
				"console at all - drop the entry rather than leaving a licence nothing uses",
				rel, why)
		}
	}
}
