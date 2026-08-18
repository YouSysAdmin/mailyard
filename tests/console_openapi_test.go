// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"sort"
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/apidoc"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/openapi"
)

// undocumentedConsoleRoutes are console routes with no entry, and why.
//
// A route reaches this list by answering something the reflector
// cannot describe - a byte stream, a redirect, a rendered page. Naming
// them here rather than letting them be absent means the gap is a
// decision somebody wrote down.
// An entry that stops matching a real route is dead weight nothing would
// report, which is what TestEveryUndocumentedConsoleRouteStillExists
// checks - consoleRoutes does not look under /api/v1, so a route moving
// there leaves its excuse behind.
var undocumentedConsoleRoutes = map[string]string{
	"GET /app/api/events/stream": "a server-sent event stream, which OpenAPI cannot express usefully",
}

// An exemption that matches no route is a failure, exactly as an
// unmatched entry in crossProjectByDesign is.
//
// Without this the map only ever grew: an entry kept excusing a route
// that had been renamed, moved or deleted, and the next reader took the
// list as a description of the current gaps. Five of its six entries
// were in that state.
func TestEveryUndocumentedConsoleRouteStillExists(t *testing.T) {
	registered := consoleRoutes(t)
	for r, why := range undocumentedConsoleRoutes {
		if !registered[r] {
			t.Errorf("undocumentedConsoleRoutes excuses %s (%s), but routes.go registers no such route",
				r, why)
		}
	}
}

// TestEveryConsoleRouteIsDocumented pins the console document to the
// router, the same way the machine one is pinned.
//
// The console metadata is generated rather than written, which makes
// this MORE necessary rather than less: a generated file is one nobody
// reads, so a route added after the last generation would sit
// undescribed and unnoticed. Both directions fail.
func TestEveryConsoleRouteIsDocumented(t *testing.T) {
	registered := consoleRoutes(t)

	documented := map[string]bool{}
	for _, r := range openapi.ConsoleRoutes() {
		documented[r.Method+" "+apidoc.NormalizePath(r.Path)] = true
	}

	var missing, orphaned []string
	for r := range registered {
		if documented[r] {
			continue
		}

		if why, ok := undocumentedConsoleRoutes[r]; ok {
			t.Logf("undocumented by choice: %s (%s)", r, why)
			continue
		}

		missing = append(missing, r)
	}

	for d := range documented {
		if !registered[d] {
			orphaned = append(orphaned, d)
		}
	}

	sort.Strings(missing)
	sort.Strings(orphaned)

	for _, m := range missing {
		t.Errorf("console route %s has no entry in any domain's ConsoleDocs - regenerate, "+
			"or list it in undocumentedConsoleRoutes with a reason", m)
	}

	for _, o := range orphaned {
		t.Errorf("ConsoleDocs describes %s but routes.go registers no such route", o)
	}
}

// TestConsoleSpecBuilds runs the reflector over the generated metadata.
func TestConsoleSpecBuilds(t *testing.T) {
	raw, err := openapi.ConsoleSpec()
	if err != nil {
		t.Fatalf("building the console document: %v", err)
	}

	if len(raw) == 0 {
		t.Fatal("the console document is empty")
	}

	for _, ref := range refsIn(raw) {
		if !strings.HasPrefix(ref, "#/components/schemas/") {
			t.Errorf("unexpected ref form %q", ref)
		}
	}
}

// consoleRoutes parses routes.go for every registration the console
// talks to, returning "method /full/path" - both prefixes, neither
// stripped.
//
// Two prefixes because the console spans two: the browser ceremonies
// moved to /app/api while the product surface is still on /api and on
// its way to /api/v1. Keeping the full path is what lets one map hold
// both without /health from one shadowing /health from the other.
//
// It shares groupPrefixes/routesUnder with the machine checker rather
// than keeping its own walk: the two had already drifted once, when
// the machine surface grew route groups and its copy still recognised
// exactly one receiver name.
func consoleRoutes(t *testing.T) map[string]bool {
	t.Helper()
	out := routesUnder(t, "", func(full string) bool {
		if strings.HasPrefix(full, env.ConsolePath+"/api") {
			return false
		}

		// Everything else under /api except the machine surface.
		return !strings.HasPrefix(full, "/api") || strings.HasPrefix(full, "/api/v1")
	})
	if len(out) == 0 {
		t.Fatal("found no console routes in routes.go")
	}

	return out
}
