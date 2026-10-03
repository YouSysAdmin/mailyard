// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A DELIBERATE SIGN-OUT DROPS THE CACHED PROFILE.
//
// leaveConsole loads the login page in a fresh document, and the auth
// store of that document starts from the profile cached in localStorage.
// Left there, the login route's guest guard sees a signed-in user, sends
// the tab back into the console, and the first request answers 401 - so
// signing yourself out ended on "your session has expired". Every caller
// of leaveConsole must also clear the session, through logout() or
// clearSession().
func TestEverySignOutClearsTheCachedSession(t *testing.T) {
	root := consoleSrc(t)

	var findings []string
	callers := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() ||
			(!strings.HasSuffix(path, ".vue") && !strings.HasSuffix(path, ".ts")) {
			return err
		}

		rel, _ := filepath.Rel(root, path)
		if filepath.ToSlash(rel) == "composables/session.ts" {
			return nil
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		text := string(body)
		if !strings.Contains(text, "leaveConsole()") {
			return nil
		}

		callers++
		if !strings.Contains(text, ".logout()") && !strings.Contains(text, ".clearSession()") {
			findings = append(findings, filepath.ToSlash(rel))
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if callers == 0 {
		t.Fatal("found no caller of leaveConsole - the sign-out has moved and this check reads nothing")
	}

	slices.Sort(findings)

	if len(findings) > 0 {
		t.Errorf("%d file(s) call leaveConsole without clearing the session:\n  %s",
			len(findings), strings.Join(findings, "\n  "))
	}
}
