// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/openapi"
)

// hugoFS is the built documentation stripped to its shape: a page is a
// DIRECTORY holding index.html, plus the site root, one fingerprinted
// asset and the 404 page.
func hugoFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":                            {Data: []byte("<h1>Mailyard Documentation</h1>")},
		"404.html":                              {Data: []byte("<h1>Page not found</h1>")},
		"email-sending/index.html":              {Data: []byte("<h1>Email Sending</h1>")},
		"email-sending/single-email/index.html": {Data: []byte("<h1>Single Email</h1>")},
		"css/app.min.abc123.css":                {Data: []byte(".cs-doc{}")},
		"js/app.min.def456.js":                  {Data: []byte("void 0")},
		"fonts/inter-tight-400.woff2":           {Data: []byte("wOF2")},
		"assets/logo/favicon.svg":               {Data: []byte("<svg/>")},
		"vendor/scalar/standalone.js":           {Data: []byte("void 0")},
		"index.json":                            {Data: []byte(`[{"title":"Single Email"}]`)},
	}
}

// EVERY LINK IN THESE PAGES IS WRITTEN WITHOUT A TRAILING SLASH
// (/docs/email-sending/single-email), and the site builder writes a
// directory per page. So the whole link scheme rests on a request for a
// directory being answered with its index.html - if that stops happening,
// every internal link 404s at once, and nothing asserts the prose that
// carries them.
//
// The 404 page is served with a 404, not the 200 a static host would give
// it: the gate in front of this returns a redirect to the console login,
// so a 200 here is the one thing that could make a missing page look like
// a working one.
func TestADocumentationPageAnswersWithAndWithoutATrailingSlash(t *testing.T) {
	app := fiber.New()
	// No gate: what is being asked here is what the filesystem answers.
	// requirePageAuth needs a runtime, and is exercised where it lives.
	mountDocs(app, hugoFS(), func(c fiber.Ctx) error { return c.Next() })

	for _, tc := range []struct {
		path   string
		status int
		body   string
	}{
		// The shape every page links to, and the shape the builder emits.
		{"/docs/email-sending/single-email", 200, "<h1>Single Email</h1>"},
		{"/docs/email-sending/single-email/", 200, "<h1>Single Email</h1>"},

		// A section landing page, and the site root.
		{"/docs/email-sending", 200, "<h1>Email Sending</h1>"},
		{"/docs/", 200, "<h1>Mailyard Documentation</h1>"},

		// Typed rather than clicked: the console links to /docs/.
		{"/docs", 302, ""},

		// Assets and the search index sit under the same prefix, so the
		// session cookie is sent with them.
		{"/docs/css/app.min.abc123.css", 200, ".cs-doc{}"},
		{"/docs/index.json", 200, `"Single Email"`},

		// A moved or misspelled page.
		{"/docs/email-sending/no-such-page", 404, "<h1>Page not found</h1>"},
		{"/docs/nope/", 404, "<h1>Page not found</h1>"},

		// The OpenAPI document the reference page fetches, answered by
		// the server rather than found in the site, under the same gate.
		{"/docs/openapi/api.json", 200, `"openapi":"3.0.3"`},
	} {
		req := httptest.NewRequest("GET", tc.path, nil)
		res, err := app.Test(req)
		if err != nil {
			t.Fatalf("%s: %v", tc.path, err)
		}

		body, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()

		if res.StatusCode != tc.status {
			t.Errorf("%s answered %d, want %d", tc.path, res.StatusCode, tc.status)
			continue
		}

		if !strings.Contains(string(body), tc.body) {
			t.Errorf("%s answered %q, want it to contain %q", tc.path, body, tc.body)
		}
	}
}

// The served document IS the exported one, byte for byte, so a reader of
// the reference page and a generator fed the CLI's output see the same
// API - and the route cannot drift into a summary of it.
func TestTheServedOpenAPIDocumentIsTheExportedOne(t *testing.T) {
	app := fiber.New()
	mountDocs(app, hugoFS(), func(c fiber.Ctx) error { return c.Next() })

	want, err := openapi.MachineSpecJSON()
	if err != nil {
		t.Fatal(err)
	}

	res, err := app.Test(httptest.NewRequest("GET", "/docs/openapi/api.json", nil))
	if err != nil {
		t.Fatal(err)
	}

	got, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content type %q, want application/json", ct)
	}

	if string(got) != string(want) {
		t.Errorf("served document differs from MachineSpecJSON (%d vs %d bytes)", len(got), len(want))
	}
}

// NOTHING UNDER /docs MAY BE STORED BY A SHARED CACHE. The whole tree is
// behind a session gate, so every tier is private - a proxy holding a
// page here would hand one reader's documentation to another. It used to
// carry no directive at all, which left the decision to browser
// heuristics and to whatever sat in front of the binary.
//
// Within that, the split is what the filename promises: Hugo
// fingerprints css and js with a sha256, so those are immutable. The
// vendored assets are unhashed but version-pinned. Everything else
// describes the running binary and revalidates.
func TestTheDocumentationSaysWhoMayStoreIt(t *testing.T) {
	app := fiber.New()
	mountDocs(app, hugoFS(), func(c fiber.Ctx) error { return c.Next() })

	for path, want := range map[string]string{
		"/docs/css/app.min.abc123.css":      "private, max-age=31536000, immutable",
		"/docs/js/app.min.def456.js":        "private, max-age=31536000, immutable",
		"/docs/fonts/inter-tight-400.woff2": "private, max-age=86400",
		"/docs/assets/logo/favicon.svg":     "private, max-age=86400",
		"/docs/vendor/scalar/standalone.js": "private, max-age=86400",
		"/docs/":                            "private, no-cache",
		"/docs/email-sending/single-email":  "private, no-cache",
		"/docs/index.json":                  "private, no-cache",
		"/docs/nothing-here":                "private, no-cache",
	} {
		res, err := app.Test(httptest.NewRequest("GET", path, nil))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}

		_ = res.Body.Close()

		if got := res.Header.Get("Cache-Control"); got != want {
			t.Errorf("%s: Cache-Control %q, want %q", path, got, want)
		}

		// public here would be the bug this test exists for.
		if strings.Contains(res.Header.Get("Cache-Control"), "public") {
			t.Errorf("%s: a gated page told a shared cache it could store it", path)
		}
	}
}

// The same conditional-request rule the console has, for the same
// reason: an embedded file's Last-Modified is the zero time, so
// revalidation has to run off the build tag or it answers 304 forever.
func TestADocumentationPageRevalidatesAgainstTheBuild(t *testing.T) {
	app := fiber.New()
	mountDocs(app, hugoFS(), func(c fiber.Ctx) error { return c.Next() })

	res, err := app.Test(httptest.NewRequest("GET", "/docs/", nil))
	if err != nil {
		t.Fatalf("first request: %v", err)
	}

	_ = res.Body.Close()

	tag := res.Header.Get("ETag")
	if tag == "" {
		t.Fatal("a documentation page carries no ETag")
	}

	match := httptest.NewRequest("GET", "/docs/", nil)
	match.Header.Set("If-None-Match", tag)

	res, err = app.Test(match)
	if err != nil {
		t.Fatalf("If-None-Match: %v", err)
	}

	_ = res.Body.Close()
	if res.StatusCode != 304 {
		t.Errorf("If-None-Match with the current tag answered %d, want 304", res.StatusCode)
	}

	stale := httptest.NewRequest("GET", "/docs/", nil)
	stale.Header.Set("If-Modified-Since", "Wed, 01 Jan 2025 00:00:00 GMT")

	res, err = app.Test(stale)
	if err != nil {
		t.Fatalf("If-Modified-Since: %v", err)
	}

	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()

	if res.StatusCode != 200 || len(body) == 0 {
		t.Errorf("If-Modified-Since answered %d with %d bytes, want 200 and a body",
			res.StatusCode, len(body))
	}
}
