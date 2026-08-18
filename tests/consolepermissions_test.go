// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/internal/models/permission"
)

// The console tree is walked in full. A fixed file list was tried first
// and fell behind twice in one day - every view that gains a can()
// call would need remembering, and a forgotten file is a permission
// string nothing validates.

// permInConsole matches `permission: 'emails:read'` in the nav and
// router, and every can('...') call in a view or store.
var permInConsole = regexp.MustCompile(`(?:permission: |can\()'([a-z]+:[a-z]+)'`)

// TestConsolePermissionsExist stops the menu and the catalogue from
// drifting apart.
//
// The console decides what to show from permission strings it receives
// as opaque text - `permissions.includes('emails:read')`. A typo there
// is not a compile error in TypeScript and not a failure at runtime
// either: `includes` simply returns false, the item never appears, and
// the report is "the Domains page vanished for everyone" weeks later,
// with nothing in any log.
//
// The reverse mistake is quieter still. Rename a resource in the
// catalogue, update every Go call site because the compiler insists,
// and the console keeps asking for the old string and hiding a page
// that everybody is entitled to.
//
// Go owns the catalogue, so the check lives here rather than in the
// frontend test suite, which does not know what a resource is.
func TestConsolePermissionsExist(t *testing.T) {
	// Built from the declared actions, not from all three. A console
	// asking for contacts:write would otherwise pass this check and
	// hide its control from everybody, which is the exact failure the
	// test exists to catch.
	known := map[string]bool{}
	for _, d := range permission.Registry {
		for _, a := range d.Actions {
			known[string(permission.Of(d.Resource, a))] = true
		}
	}

	seen := map[string]bool{}
	var unknown []string
	err := filepath.WalkDir(consoleSrc(t), func(path string, d os.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}

		if d.IsDir() {
			if d.Name() == "node_modules" || d.Name() == "dist" {
				return filepath.SkipDir
			}

			return nil
		}

		if !strings.HasSuffix(path, ".vue") && !strings.HasSuffix(path, ".ts") {
			return nil
		}

		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}

		for _, m := range permInConsole.FindAllStringSubmatch(string(body), -1) {
			seen[m[1]] = true
			if !known[m[1]] {
				unknown = append(unknown, m[1]+" ("+path+")")
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", consoleSrc(t), err)
	}

	sort.Strings(unknown)

	if len(seen) < 15 {
		t.Fatalf("only found %d permission strings in the console, the scan is broken", len(seen))
	}

	if len(unknown) > 0 {
		t.Errorf("the console asks for %d permission(s) this catalogue does not define:\n  %s\n"+
			"A permission nothing grants hides its menu item from everybody, silently and "+
			"forever - includes() just returns false.",
			len(unknown), strings.Join(unknown, "\n  "))
	}
}
