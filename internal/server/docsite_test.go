// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gofiber/fiber/v2"
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
	mountDocs(app, hugoFS(), func(c *fiber.Ctx) error { return c.Next() })

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
