// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/env"
)

// consoleAPIDir(t) holds one module per domain, and every path the
// browser calls is a literal in one of them.

// consoleCall matches `api.get<T>('/path')` and its template-literal
// form on either axios instance. The generic is optional and never
// contains a parenthesis, so stopping at the first one is safe.
var consoleCall = regexp.MustCompile(
	"\\b(api|appApi)\\.(get|post|put|patch|delete)\\s*(?:<[^()]*>)?\\s*\\(\\s*([`'\"])([^`'\"]+)[`'\"]")

// TestTheConsoleCallsRoutesThatExist is the guard the console did not
// have, and the bug that produced it says why it was needed.
//
// The SDK has one (TestSDKCoversEveryV1Route) and the documentation
// has one for request bodies. The console had neither, so when the
// product surface moved to /api/v1 a sweep prefixed the whole of
// plans.ts with /admin - correct for the four plan-CRUD routes in it,
// wrong for the fifth, which is a tenant usage report. Nothing failed:
// TypeScript type-checks a string, the build succeeds, and the defect
// surfaces as a 404 popup on the project settings page.
//
// Only one direction is checked. A route the console never calls is
// not a defect - most of the machine surface exists for integrations -
// but a call to a path the router does not register is always one.
func TestTheConsoleCallsRoutesThatExist(t *testing.T) {
	registered := map[string]bool{}
	for r := range routesUnder(t, "", nil) {
		registered[normalizeParams(trimSlash(r))] = true
	}

	if len(registered) < 200 {
		t.Fatalf("only found %d routes in routes.go - the parse is broken", len(registered))
	}

	calls := consoleCalls(t)
	if len(calls) < 100 {
		t.Fatalf("only found %d console calls - the scan is broken", len(calls))
	}

	var broken []string
	for call, where := range calls {
		if !registered[call] {
			broken = append(broken, call+"  ("+where+")")
		}
	}

	sort.Strings(broken)
	for _, b := range broken {
		t.Errorf("the console calls a route the router does not register:\n  %s", b)
	}
}

// consoleCalls maps "method /full/path" to the file it came from.
//
// The two axios instances carry different base URLs, and which one a
// module imports is the whole difference between a browser ceremony
// and a product operation - so the prefix comes from the instance
// name rather than being assumed.
func consoleCalls(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(consoleAPIDir(t))
	if err != nil {
		t.Fatalf("read %s: %v", consoleAPIDir(t), err)
	}

	base := map[string]string{"api": "/api/v1", "appApi": env.ConsolePath + "/api"}

	out := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".ts") {
			continue
		}

		body, err := os.ReadFile(filepath.Join(consoleAPIDir(t), e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}

		for _, m := range consoleCall.FindAllStringSubmatch(string(body), -1) {
			instance, method, path := m[1], strings.ToUpper(m[2]), m[4]
			// A path assembled from a variable cannot be compared, and
			// there is nothing to check in one.
			if !strings.HasPrefix(path, "/") {
				continue
			}

			full := trimSlash(base[instance] + templateParams(path))
			out[method+" "+full] = e.Name()
		}
	}

	return out
}

// templateParams reduces `${anything}` to the "*" normalizeParams
// leaves a router `:param` as.
var templateParam = regexp.MustCompile(`\$\{[^}]*\}`)

func templateParams(path string) string { return templateParam.ReplaceAllString(path, "*") }

// trimSlash drops a trailing slash so `/projects/` and `/projects`
// compare. Fiber treats them as the same route, and the console is
// inconsistent about which it writes.
func trimSlash(s string) string {
	if len(s) > 1 && strings.HasSuffix(s, "/") {
		return s[:len(s)-1]
	}

	return s
}
