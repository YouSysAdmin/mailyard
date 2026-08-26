// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package audit

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	amodel "github.com/yousysadmin/mailyard/internal/models/audit"
)

// TestAStampedEventOutlivesItsRequest is the reason Stamp copies.
//
// The trail is written on another goroutine, so an event is read long
// after fasthttp has put the request back in its pool and reused the
// buffers behind it. A field that merely pointed at a header read as
// whatever request came next - measured before the fix, where the FIRST
// event reported the THIRD request's user agent.
//
// Three requests over one app is what reproduces it: app.Test serves them
// through the same pooled context, which is what a keep-alive connection
// does in production.
func TestAStampedEventOutlivesItsRequest(t *testing.T) {
	// Different lengths on purpose: the failure was not only a wrong
	// value but a truncated view of a longer one.
	agents := []string{
		"Mozilla/5.0 (Macintosh) AAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"curl/8.4.0",
		"Mozilla/5.0 (X11) CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC",
	}

	var kept []*amodel.Event
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		e := &amodel.Event{Type: "test.event"}
		Stamp(e, c)
		kept = append(kept, e)

		return c.SendString("ok")
	})

	for _, ua := range agents {
		req := httptest.NewRequest(fiber.MethodGet, "/", nil)
		req.Header.Set(fiber.HeaderUserAgent, ua)
		if _, err := app.Test(req); err != nil {
			t.Fatalf("Test: %v", err)
		}
	}

	for i, want := range agents {
		if kept[i].UserAgent != want {
			t.Errorf("event %d recorded user agent %q, want %q - the field is a view of a reused buffer",
				i, kept[i].UserAgent, want)
		}

		if kept[i].ClientIP != "0.0.0.0" {
			t.Errorf("event %d recorded client ip %q, want the peer address", i, kept[i].ClientIP)
		}
	}
}

// TestAStampedPathOutlivesItsRequest is the same property one field
// over. The route middleware fills Path from c.Path(), which Fiber
// answers as an unsafe view of the pooled context's path buffer.
func TestAStampedPathOutlivesItsRequest(t *testing.T) {
	paths := []string{
		"/api/v1/templates/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"/api/v1/x",
		"/api/v1/smtp-servers/cccccccccccccccccccccccccccccccccccc/test",
	}

	var kept []*amodel.Event
	app := fiber.New()
	app.Get("/*", func(c fiber.Ctx) error {
		e := &amodel.Event{Type: "test.event", Path: c.Path()}
		Stamp(e, c)
		kept = append(kept, e)

		return c.SendString("ok")
	})

	for _, p := range paths {
		if _, err := app.Test(httptest.NewRequest(fiber.MethodGet, p, nil)); err != nil {
			t.Fatalf("Test: %v", err)
		}
	}

	for i, want := range paths {
		if kept[i].Path != want {
			t.Errorf("event %d recorded path %q, want %q - the field is a view of a reused buffer",
				i, kept[i].Path, want)
		}
	}
}

// TestAStampedUserAgentIsBoundedAndStorable keeps the two things the clamp
// is there for: a caller cannot write an unbounded column, and cannot
// write bytes Postgres refuses.
func TestAStampedUserAgentIsBoundedAndStorable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		agent string
		check func(t *testing.T, got string)
	}{{
		name:  "longer than the cap",
		agent: strings.Repeat("a", 900),
		check: func(t *testing.T, got string) {
			if len([]rune(got)) != 400 {
				t.Errorf("kept %d runes, want the 400-rune cap", len([]rune(got)))
			}
		},
	}, {
		name:  "invalid utf-8, which failed the insert",
		agent: "curl/8.4.0 \xff\xfe",
		check: func(t *testing.T, got string) {
			if !strings.HasPrefix(got, "curl/8.4.0") || strings.ContainsRune(got, '�') {
				t.Errorf("kept %q, want the valid text with the bad bytes removed", got)
			}
		},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			app := fiber.New()
			app.Get("/", func(c fiber.Ctx) error {
				e := &amodel.Event{Type: "test.event"}
				Stamp(e, c)
				got = e.UserAgent

				return c.SendString("ok")
			})

			req := httptest.NewRequest(fiber.MethodGet, "/", nil)
			req.Header.Set(fiber.HeaderUserAgent, tc.agent)
			if _, err := app.Test(req); err != nil {
				t.Fatalf("Test: %v", err)
			}

			tc.check(t, got)
		})
	}
}
