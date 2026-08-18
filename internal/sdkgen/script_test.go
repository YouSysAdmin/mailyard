// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package sdkgen

import (
	"strings"
	"testing"
)

// The Go generator settles collisions between ROUTES, and snake() runs
// after it - so two distinct Go names can still land on one snake name,
// and the second definition silently replaces the first. Python and
// Ruby both accept that without a word: the class ends up short a
// method and calling it reaches the wrong route.
//
// The word fixes in snake() are what makes this reachable rather than
// theoretical - they rewrite whole substrings, so a new pair is one
// entry away.
func TestScriptMethodNamesAreUnique(t *testing.T) {
	seen := map[string]scriptMethod{}
	for _, m := range scriptMethods() {
		if prev, dup := seen[m.Name]; dup {
			t.Errorf("two routes generate %s():\n  %s %s\n  %s %s",
				m.Name, prev.HTTPMethod, prev.Path, m.HTTPMethod, m.Path)
			continue
		}

		seen[m.Name] = m
	}

	if len(seen) == 0 {
		t.Fatal("no methods - this test would pass vacuously")
	}
}

// A path parameter becomes an argument name in both languages, so it
// has to be an identifier - and Ruby interpolates it into the path
// string, where anything else is a syntax error rather than a bad call.
func TestPathParametersAreIdentifiers(t *testing.T) {
	for _, m := range scriptMethods() {
		for _, p := range m.PathParams {
			if p == "" {
				t.Errorf("%s: empty path parameter in %s", m.Name, m.Path)
				continue
			}

			for i, r := range p {
				ok := r == '_' || (r >= 'a' && r <= 'z') ||
					(i > 0 && r >= '0' && r <= '9')
				if !ok {
					t.Errorf("%s: path parameter %q is not an identifier (%s)", m.Name, p, m.Path)
					break
				}
			}
		}
	}
}

// Every interpolated path parameter goes through the language's escape
// helper.
//
// Dropping the value in raw, as the Go generator never did, lets an id
// of "../users/x" walk the URL up a level and reach a
// different endpoint, and one carrying "?" or "#" injected a query or
// truncated the path. The escape call in the expectations below is the
// fix, which is why it is asserted rather than left to the golden files.
func TestRubyPathInterpolates(t *testing.T) {
	for _, tc := range [][2]string{
		{"/api/v1/emails", "/api/v1/emails"},
		{"/api/v1/emails/{id}", "/api/v1/emails/#{esc(id)}"},
		{"/api/v1/templates/{id}/versions/{version_id}",
			"/api/v1/templates/#{esc(id)}/versions/#{esc(version_id)}"},
	} {
		if got := rubyPath(tc[0]); got != tc[1] {
			t.Errorf("rubyPath(%q) = %q, want %q", tc[0], got, tc[1])
		}
	}
}

func TestPythonPathInterpolates(t *testing.T) {
	for _, tc := range [][2]string{
		{"/api/v1/emails", "/api/v1/emails"},
		{"/api/v1/emails/{id}", "/api/v1/emails/{_esc(id)}"},
		{"/api/v1/templates/{id}/versions/{version_id}",
			"/api/v1/templates/{_esc(id)}/versions/{_esc(version_id)}"},
	} {
		if got := pyPath(tc[0]); got != tc[1] {
			t.Errorf("pyPath(%q) = %q, want %q", tc[0], got, tc[1])
		}
	}
}

// Neither generated client may interpolate a bare path parameter. The
// two tests above check the helper, this checks the OUTPUT - a future
// change to the renderers cannot quietly stop calling it.
func TestNoGeneratedPathParameterIsUnescaped(t *testing.T) {
	for name, src := range RenderPython() {
		for _, m := range scriptMethods() {
			for _, p := range m.PathParams {
				if strings.Contains(src, "{"+p+"}") {
					t.Errorf("%s: {%s} is interpolated without _esc", name, p)
				}
			}
		}
	}

	for name, src := range RenderRuby() {
		for _, m := range scriptMethods() {
			for _, p := range m.PathParams {
				if strings.Contains(src, "#{"+p+"}") {
					t.Errorf("%s: #{%s} is interpolated without esc", name, p)
				}
			}
		}
	}
}
