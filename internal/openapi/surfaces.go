// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package openapi

import (
	"slices"
	"strings"

	"github.com/yousysadmin/mailyard/internal/core/apidoc"
	"github.com/yousysadmin/mailyard/internal/core/env"
)

// Which surface a documented route belongs to.
//
// The generated ConsoleDocs metadata records a path RELATIVE to its
// mount, because it was written when there was one console mount.
// Something now has to say which mount - and this is that something,
// in one place, checked in both directions by
// TestEveryConsoleRouteIsDocumented and TestEveryV1RouteIsDocumented.
//
// It is a bridge, not a fixture. The metadata is generated from
// routes.go, so regenerating it with full paths removes this file.
type surface int

const (
	// surfaceProduct is /api/v1 - everything usable remotely.
	surfaceProduct surface = iota

	// surfaceAdmin is /api/v1/admin - platform administration. Same
	// prefix as the product, one segment deeper.
	surfaceAdmin

	// surfaceConsoleOwn is /app/api - what only a browser does.
	surfaceConsoleOwn

	// surfaceEnrol is /api/relay-nodes - the routes a relay node
	// reaches with its enrolment token and afterwards with its control
	// token. Deliberately unversioned: it is a handshake between our
	// own binaries, not an API anybody builds against.
	surfaceEnrol
)

// consoleOwnPaths are the browser ceremonies and the caller's own
// account activity. Nothing here is usable remotely.
var consoleOwnPaths = []string{"/auth/", "/events/", "/health", "/security-log"}

// enrolPaths are the public relay-node handshake, which SHARES the
// /relay-nodes prefix with the admin group and so has to be named
// exactly rather than matched.
var enrolPaths = []string{
	"/relay-nodes/register", "/relay-nodes/heartbeat",
	"/relay-nodes/renew", "/relay-nodes/report",
	"/relay-nodes/inbound", "/relay-nodes/claim",
}

// adminPaths are platform administration.
//
// `/projects/:id/plan` is listed and must be tested before
// `/projects`: assigning a plan is an operator act on somebody else's
// project, while the rest of /projects is the caller's own membership
// surface. Two neighbouring paths, two different tiers.
var adminPaths = []string{
	"/projects/:id/plan",
	"/settings", "/jobs", "/system-mail", "/plans",
	"/oauth-providers", "/users", "/relay-nodes", "/shared-smtp-servers",
}

func surfaceOf(path string) surface {
	if slices.Contains(enrolPaths, path) {
		return surfaceEnrol
	}

	for _, p := range adminPaths {
		if strings.HasPrefix(path, p) {
			return surfaceAdmin
		}
	}

	for _, p := range consoleOwnPaths {
		if strings.HasPrefix(path, p) {
			return surfaceConsoleOwn
		}
	}

	return surfaceProduct
}

// v1Path is where a route sits RELATIVE to /api/v1, which is the base
// the machine document is served from.
func v1Path(path string) string {
	if surfaceOf(path) == surfaceAdmin {
		return "/admin" + path
	}

	return path
}

// fullPath is the absolute path, for documents not served from a base.
func fullPath(path string) string {
	switch surfaceOf(path) {
	case surfaceConsoleOwn:
		return env.ConsolePath + "/api" + path
	case surfaceEnrol:
		return "/api" + path
	default:
		return "/api/v1" + v1Path(path)
	}
}

// productDocs are the generated entries that belong to the machine
// document, with paths relative to /api/v1.
func productDocs() []apidoc.Route {
	var out []apidoc.Route
	for _, r := range allConsoleDocs() {
		switch surfaceOf(r.Path) {
		case surfaceProduct, surfaceAdmin:
			r.Path = v1Path(r.Path)
			out = append(out, r)
		}
	}

	return out
}

// consoleDocs are the generated entries that did not move to the
// product surface: the browser ceremonies, and the public relay-node
// handshake. Absolute paths, since they span two prefixes.
func consoleDocs() []apidoc.Route {
	var out []apidoc.Route
	for _, r := range allConsoleDocs() {
		switch surfaceOf(r.Path) {
		case surfaceConsoleOwn, surfaceEnrol:
			r.Path = fullPath(r.Path)
			out = append(out, r)
		}
	}

	return out
}

// mergeDocs folds the generated entries in behind the hand-written
// ones, which win on collision.
//
// Handwritten first because those descriptions were written for
// somebody integrating against this API - they explain refusals and
// name the failure modes. The generated ones say what a route is and
// which permission it needs, which is worth having for the routes
// nobody has written prose for yet, and worse than the prose where it
// exists.
func mergeDocs(handWritten, generated []apidoc.Route) []apidoc.Route {
	seen := make(map[string]bool, len(handWritten))
	out := make([]apidoc.Route, 0, len(handWritten)+len(generated))
	for _, r := range handWritten {
		seen[r.Method+" "+apidoc.NormalizePath(r.Path)] = true
		out = append(out, r)
	}

	for _, r := range generated {
		if seen[r.Method+" "+apidoc.NormalizePath(r.Path)] {
			continue
		}

		out = append(out, r)
	}

	return out
}
