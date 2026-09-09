// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// A rule about the console, checked by reading web/src.

package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// dateToLocale matches a date being formatted straight to a locale
// string, which is the 12-hour default in en-US.
//
// `new Date(x).toLocaleString(...)` and `.toLocaleTimeString(...)`. Not
// toLocaleDateString, which carries no time and so no clock, and not a
// bare `n.toLocaleString()` on a number - that one is thousands
// separators on the dashboard.
var dateToLocale = regexp.MustCompile(`new Date\([^)]*\)\s*\.toLocale(String|TimeString)\(`)

// readsTheClock matches a file working out how long ago something was,
// or whether a moment has passed, from Date.now().
//
// The rule above cannot see that: it looks for a locale call and this
// is arithmetic, and two relative formatters word the same interval
// differently. So the rule is the same one, one layer down: a view does
// not read the clock. formatDate.ts does, and offers timeAgo and isPast.
var readsTheClock = regexp.MustCompile(`Date\.now\(\)|\.getTime\(\)`)

// The console shows time in one clock, and it is not the browser
// default.
//
// A view with its own toLocaleString call keeps rendering AM/PM
// whatever the shared formatter says, and nothing fails. So views do
// not format at all: formatDate for a full stamp, formatTimeParts when
// a caller wants shorter components, and both carry the clock.
func TestTheConsoleFormatsTimeInOnePlace(t *testing.T) {
	root := consoleSrc(t)
	const composable = "composables/formatDate.ts"

	// Two lists, because the two offences want different advice.
	var formatters, clockReaders []string
	seen := false
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		if !strings.HasSuffix(path, ".vue") && !strings.HasSuffix(path, ".ts") {
			return nil
		}

		rel := filepath.ToSlash(strings.TrimPrefix(path, root+string(filepath.Separator)))
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		if rel == composable {
			seen = true
			// The one file allowed to say it, and it has to still be
			// saying it. The OPTION as written in code, not the bare
			// word: this file discusses hourCycle in its comments, so
			// matching the word passes on a formatter with the option
			// deleted.
			if !strings.Contains(string(body), "hourCycle: 'h23'") {
				t.Errorf("%s no longer sets hourCycle: 'h23', so the whole console is back "+
					"to the browser default, which is 12-hour in en-US", rel)
			}

			return nil
		}

		for _, m := range dateToLocale.FindAllString(string(body), -1) {
			formatters = append(formatters, rel+": "+m)
		}

		// Composables are where the clock lives. certExpiry counts days
		// to an expiry for a badge, useAutoRefresh paces its own ticks -
		// both are time as BEHAVIOUR rather than time rendered for a
		// reader, and neither is a second formatter.
		if !strings.HasPrefix(rel, "composables/") {
			for _, m := range readsTheClock.FindAllString(string(body), -1) {
				clockReaders = append(clockReaders, rel+": "+m)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if !seen {
		t.Fatalf("%s was not found - this test would pass vacuously", composable)
	}

	for _, o := range formatters {
		t.Errorf("%s formats a date itself. Use formatDate or formatTimeParts: a local "+
			"toLocaleString takes the browser's clock, which is 12-hour in en-US, and it "+
			"will not follow when the shared formatter changes", o)
	}

	for _, o := range clockReaders {
		t.Errorf("%s reads the clock itself. Use timeAgo for how long ago something was "+
			"and isPast for whether a moment has gone by - three hand-rolled copies of "+
			"the first one worded the same interval three different ways", o)
	}
}
