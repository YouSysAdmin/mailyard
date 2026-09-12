// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
)

// consoleFS is a build stripped to what these rules are about: a shell,
// one hashed chunk, and nothing else.
func consoleFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":                    {Data: []byte("<!doctype html><div id=app>")},
		"assets/index-AAAA.js":          {Data: []byte("export const a = 1")},
		"assets/SmtpServers-BBBB.js":    {Data: []byte("export const b = 2")},
		"assets/SmtpServers-BBBB.js.br": {Data: []byte("compressed")},
		"favicon.svg":                   {Data: []byte("<svg/>")},
	}
}

// A tab left open across an upgrade asks for the chunk hashes its own
// shell names, and after a rebuild those files are gone. If the SPA
// fallback answers, the browser gets 200 text/html for something it is
// about to evaluate as a module and reports "Importing a module script
// failed" - a message naming neither the file nor the reason, and the
// page simply does not switch.
//
// So: a miss under /assets is a 404. A miss anywhere else is the
// history-mode fallback, which is what that fallback is for.
func TestAMissingChunkIsNotTheHTMLShell(t *testing.T) {
	app := fiber.New()
	mountConsole(app, consoleFS())

	for _, tc := range []struct {
		path   string
		status int
		body   string
	}{
		{env.ConsolePath + "/assets/SmtpServers-BBBB.js", 200, "export const b = 2"},
		{env.ConsolePath + "/assets/SmtpServers-STALE.js", 404, ""},
		{env.ConsolePath + "/assets/", 404, ""},
		{env.ConsolePath + "/assets/../index.html", 404, ""},

		// Not an asset: a deep link the router resolves client-side.
		{env.ConsolePath + "/smtp-servers", 200, "<div id=app>"},
		{env.ConsolePath + "/", 200, "<div id=app>"},

		// The icon lives at the console root, and that is the only place
		// it is a file. Asked for under a deep link's own path it is the
		// fallback shell, which is why the shell links it absolutely
		// (TestTheShellLinksItsIconsAbsolutely).
		{env.ConsolePath + "/favicon.svg", 200, "<svg/>"},
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

		if tc.body != "" && !strings.Contains(string(body), tc.body) {
			t.Errorf("%s answered %q, want it to contain %q", tc.path, body, tc.body)
		}

		if tc.status == 404 && strings.Contains(string(body), "<div id=app>") {
			t.Errorf("%s answered the HTML shell - this is the exact failure "+
				"the browser reports as an unimportable module script", tc.path)
		}
	}
}

// A CONSOLE PAGE WHOSE PATH STARTS WITH THE API PREFIX IS STILL A PAGE.
//
// Fiber's Use matches a raw string prefix, so the JSON 404 registered on
// `/app/api` also matched `/app/api-keys`, which is the API Keys page.
// Nothing looked broken from inside the console, because a click there
// is client-side routing and never asks the server - the page answered
// `{"error":"not found"}` only on a reload or a shared link.
//
// Mounted in the same order registerRoutes uses: the API miss first,
// then the console, so this is the arrangement being tested rather than
// a hopeful one.
func TestAConsolePageIsNotSwallowedByTheAPIPrefix(t *testing.T) {
	app := fiber.New()
	app.Use(env.ConsolePath+"/api", apiNotFound(env.ConsolePath+"/api"))
	mountConsole(app, consoleFS())

	for _, tc := range []struct {
		path string
		want string
	}{
		// The page. The shell, resolved client-side from there.
		{env.ConsolePath + "/api-keys", "<div id=app>"},
		// The console API itself, which has no such route in this
		// mount and must still answer in the envelope.
		{env.ConsolePath + "/api/auth/info", `"error"`},
		// The bare prefix counts as the API, not as a page.
		{env.ConsolePath + "/api", `"error"`},
	} {
		res, err := app.Test(httptest.NewRequest("GET", tc.path, nil))
		if err != nil {
			t.Fatalf("%s: %v", tc.path, err)
		}

		body, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()

		if !strings.Contains(string(body), tc.want) {
			t.Errorf("%s answered %q, want it to contain %q", tc.path, body, tc.want)
		}
	}
}

// The shell must revalidate or an upgraded binary never reaches a
// browser that already has one. Hashed assets are immutable by
// construction, so they get the opposite.
func TestTheShellIsNotCachedAndTheChunksAre(t *testing.T) {
	app := fiber.New()
	mountConsole(app, consoleFS())

	for path, want := range map[string]string{
		env.ConsolePath + "/":                           "no-cache",
		env.ConsolePath + "/smtp-servers":               "no-cache",
		env.ConsolePath + "/assets/SmtpServers-BBBB.js": "immutable",

		// A MISS is not immutable. The header above is keyed on the
		// path, which is all that is known before the lookup, so a
		// chunk that went in an upgrade would otherwise pin its own
		// 404 in every cache on the way for a year.
		env.ConsolePath + "/assets/Gone-CCCC.js": "no-store",
	} {
		res, err := app.Test(httptest.NewRequest("GET", path, nil))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}

		_ = res.Body.Close()
		if got := res.Header.Get("Cache-Control"); !strings.Contains(got, want) {
			t.Errorf("%s: Cache-Control %q, want it to contain %q", path, got, want)
		}
	}
}

// A conditional request must be answered from the BUILD, not from the
// Last-Modified fasthttp serves for an embedded file - which is the zero
// time, and so matches every client date. Left to fasthttp, a browser
// holding a stale shell revalidates and is told 304 forever, across
// upgrades, which is the failure the no-cache above exists to prevent.
func TestAStaleShellIsNotRevalidatedAwayByTheZeroModTime(t *testing.T) {
	app := fiber.New()
	mountConsole(app, consoleFS())

	res, err := app.Test(httptest.NewRequest("GET", env.ConsolePath+"/", nil))
	if err != nil {
		t.Fatalf("first request: %v", err)
	}

	_ = res.Body.Close()

	tag := res.Header.Get("ETag")
	if tag == "" {
		t.Fatal("the shell carries no ETag, so nothing can validate against the build")
	}

	// The tag this build did hand out: 304, with no body.
	match := httptest.NewRequest("GET", env.ConsolePath+"/", nil)
	match.Header.Set("If-None-Match", tag)

	res, err = app.Test(match)
	if err != nil {
		t.Fatalf("If-None-Match: %v", err)
	}

	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()

	if res.StatusCode != 304 {
		t.Errorf("If-None-Match with the current tag answered %d, want 304", res.StatusCode)
	}

	if len(body) != 0 {
		t.Errorf("a 304 carried %d bytes of body, want none", len(body))
	}

	// The one that used to be answered 304 whatever the build said.
	stale := httptest.NewRequest("GET", env.ConsolePath+"/", nil)
	stale.Header.Set("If-Modified-Since", "Wed, 01 Jan 2025 00:00:00 GMT")

	res, err = app.Test(stale)
	if err != nil {
		t.Fatalf("If-Modified-Since: %v", err)
	}

	body, _ = io.ReadAll(res.Body)
	_ = res.Body.Close()

	if res.StatusCode != 200 {
		t.Errorf("If-Modified-Since answered %d, want 200 - the shell was not re-sent", res.StatusCode)
	}

	if len(body) == 0 {
		t.Error("a 200 carried no body")
	}

	// A tag from another build must miss.
	other := httptest.NewRequest("GET", env.ConsolePath+"/", nil)
	other.Header.Set("If-None-Match", `"0123456789abcdef0123456789abcdef"`)

	res, err = app.Test(other)
	if err != nil {
		t.Fatalf("stale If-None-Match: %v", err)
	}

	_ = res.Body.Close()
	if res.StatusCode != 200 {
		t.Errorf("a tag from another build answered %d, want 200", res.StatusCode)
	}
}

// Two different trees are two different tags, which is the whole
// guarantee: an upgraded binary is what a browser is told about.
func TestTwoBuildsDoNotShareATag(t *testing.T) {
	one := consoleFS()

	two := consoleFS()
	two["index.html"] = &fstest.MapFile{Data: []byte("<!doctype html><title>next</title>")}

	if buildTag(one) == buildTag(two) {
		t.Error("a changed shell produced the same build tag, so a stale one would revalidate as current")
	}
}
