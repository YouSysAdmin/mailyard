// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"bytes"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	slogfiber "github.com/samber/slog-fiber"

	"github.com/yousysadmin/mailyard/internal/core/env"
)

// redactApp builds a fiber app whose access log writes into buf, with
// routes standing in for the real sensitive surfaces. The wrapped
// logger is the REAL slog-fiber, because what this test pins is that
// requests to sensitive paths never reach it - a stand-in that logs
// less than slog-fiber does would let a leak through the real one pass.
func redactApp(buf *bytes.Buffer) *fiber.App {
	log := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	app := fiber.New()
	app.Use(redactURLs(log, slogfiber.New(log)))

	ok := func(c fiber.Ctx) error { return c.SendString("ok") }
	app.Get("/tracking/view/:token", ok)
	app.Get("/tracking/o/:id", ok)
	app.Get(env.ConsolePath+"/api/auth/oauth/:slug/callback", ok)
	app.Get(env.ConsolePath+"/reset-password", ok)
	app.Get("/api/v1/templates/", ok)

	return app
}

// The URLs these surfaces take are credentials - a password-reset token
// is account takeover for its TTL, the web-view token is 90 days of
// full message content - and each was hashed or sealed precisely so a
// database reader cannot use it. slog-fiber records the raw path, the
// query string AND every route param, so the access log was quietly
// undoing that: the log ships to an aggregator, and an aggregator
// always has more readers than the database.
func TestACapabilityURLNeverReachesTheLog(t *testing.T) {
	for _, tc := range []struct {
		name, url, secret string
	}{
		{"web-view token in the path", "/tracking/view/SECRET-VIEW-TOKEN", "SECRET-VIEW-TOKEN"},
		{"tracking signature in the query", "/tracking/o/some-id?sig=SECRET-SIG", "SECRET-SIG"},
		{"oauth code in the callback query",
			env.ConsolePath + "/api/auth/oauth/x/callback?code=SECRET-CODE&state=SECRET-STATE", "SECRET-CODE"},
		{"reset token on the SPA document", env.ConsolePath + "/reset-password?token=SECRET-RESET", "SECRET-RESET"},
		{"a case variant of the path", "/TRACKING/view/SECRET-CASED", "SECRET-CASED"},
		{"a trailing slash", "/tracking/view/SECRET-SLASHED/", "SECRET-SLASHED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			app := redactApp(&buf)
			res, err := app.Test(httptest.NewRequest("GET", tc.url, nil))
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			_ = res.Body.Close()

			got := buf.String()
			if got == "" {
				t.Fatal("the request produced no log entry at all - redaction must replace the entry, not drop it")
			}

			if strings.Contains(got, tc.secret) {
				t.Errorf("the capability landed in the log:\n%s", got)
			}
		})
	}
}

// The control: an ordinary path still goes through slog-fiber whole,
// query included - over-redacting would blind the operator to exactly
// the parameters they debug with.
func TestAnOrdinaryPathStillLogsItsQuery(t *testing.T) {
	var buf bytes.Buffer
	app := redactApp(&buf)
	res, err := app.Test(httptest.NewRequest("GET", "/api/v1/templates/?q=hello", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	_ = res.Body.Close()

	if got := buf.String(); !strings.Contains(got, "q=hello") {
		t.Errorf("the ordinary query was redacted away:\n%s", got)
	}
}

// A refused request on a sensitive path is still visible AS a refusal,
// because the public tracking surface is exactly where an operator
// investigating abuse reads the log.
func TestARedactedPathStillLogsItsStatus(t *testing.T) {
	var buf bytes.Buffer
	app := redactApp(&buf)
	res, err := app.Test(httptest.NewRequest("GET", "/tracking/nothing-here", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	_ = res.Body.Close()

	got := buf.String()
	if !strings.Contains(got, "status=404") {
		t.Errorf("the refusal is not in the log:\n%s", got)
	}
}
