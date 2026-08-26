// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/internal/server"
)

// Every attachment route the server exempts from the per-request body
// ceiling has to still be a route. The list is a set of strings in
// server.go and the routes are in routes.go, so a rename in one leaves
// the other pointing at nothing - and the symptom is a send carrying a
// legitimate attachment answered 413 by fasthttp before any handler
// runs, which no handler test can see.
func TestEveryLargeBodyPathIsARoute(t *testing.T) {
	const base = "/api/v1"
	routes := map[string]bool{}
	for key := range routesUnder(t, base, nil) {
		// "POST /templates/:id/attachments" -> "/templates/*/attachments",
		// the form LargeBodyPaths is written in.
		_, path, _ := strings.Cut(key, " ")
		segs := strings.Split(path, "/")
		for i, s := range segs {
			if strings.HasPrefix(s, ":") {
				segs[i] = "*"
			}
		}

		routes[strings.Join(segs, "/")] = true
	}

	for _, p := range server.LargeBodyPaths {
		rel, ok := strings.CutPrefix(p, base)
		if !ok {
			// The relay ingest route mounts outside /api/v1 and exists
			// in the enterprise build only.
			continue
		}

		if !routes[rel] {
			t.Errorf("server.LargeBodyPaths names %q, and routes.go has no such route under %s", p, base)
		}
	}
}
