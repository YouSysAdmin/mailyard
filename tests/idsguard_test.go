// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// mintsID matches a call to any uuid constructor. Nothing outside this
// package may mint a primary key, or the tables end up carrying two
// versions and the ordering the switch was made for is gone on
// whichever rows took the other path.
var mintsID = regexp.MustCompile(`uuid\.(NewString|New|NewV7|NewRandom|Must)\b`)

func TestNothingElseMintsAnID(t *testing.T) {
	root := repoRoot(t)
	var findings []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// A path that vanished between the walk listing it and
			// this callback. Nothing gitignored is source, and the
			// dev database churns constantly - this test failed a
			// gate once on a file Postgres had just deleted under
			// dev-data, which is a guard reporting an unrelated
			// outage as a violation.
			if os.IsNotExist(err) {
				return nil
			}

			return err
		}

		if info.IsDir() {
			switch info.Name() {
			// dev-data holds the dev Postgres and the blob store. No
			// Go source, and walking a live database is how the
			// above happens.
			case "node_modules", "vendor", ".git", "dist", "sdk", "dev-data":
				return filepath.SkipDir
			}

			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		rel, _ := filepath.Rel(root, path)
		// This package is where it is allowed, and its own test needs
		// uuid.Parse to check the version.
		if strings.HasPrefix(rel, filepath.Join("internal", "core", "ids")) {
			return nil
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		for i, line := range strings.Split(string(body), "\n") {
			if mintsID.MatchString(line) {
				findings = append(findings, rel+":"+strconv.Itoa(i+1)+" "+strings.TrimSpace(line))
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	if len(findings) > 0 {
		t.Errorf("%d place(s) mint an id outside internal/core/ids:\n  %s\n\n"+
			"Call ids.New(). A second generator means a table carrying both v4 and\n"+
			"v7 keys, and rows on the v4 side lose the insertion order the switch\n"+
			"was made for.",
			len(findings), strings.Join(findings, "\n  "))
	}
}
