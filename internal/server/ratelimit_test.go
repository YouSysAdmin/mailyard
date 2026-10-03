// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"encoding/json/v2"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
)

func limitedApp(h fiber.Handler) *fiber.App {
	app := fiber.New()
	app.Use(h)
	app.Get("/x", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	return app
}

// A rate-limit refusal is the same JSON envelope as every other
// refusal, and still says when to come back.
func TestARateLimitRefusalIsTheEnvelope(t *testing.T) {
	rt := &env.Runtime{Config: &env.Config{RateLimit: env.RateLimitConfig{Enabled: true}}}
	app := limitedApp(perMinute(rt, 1, nil))

	var last *httptestResult
	for range 2 {
		last = limitedGet(t, app, "/x", "")
	}

	if last.status != fiber.StatusTooManyRequests {
		t.Fatalf("status %d, want 429", last.status)
	}

	if last.retryAfter == "" {
		t.Error("no Retry-After on the refusal")
	}

	var body struct {
		Error string `json:"error"`
	}

	if err := json.Unmarshal(last.body, &body); err != nil || body.Error == "" {
		t.Errorf("body %q is not the error envelope", last.body)
	}
}

// A request whose ceiling is zero passes unlimited, which is how the
// API and session budgets of /api/v1 are switched off one at a time.
func TestAPerRequestCeilingDecidesTheBucket(t *testing.T) {
	rt := &env.Runtime{Config: &env.Config{RateLimit: env.RateLimitConfig{Enabled: true}}}
	key := func(c fiber.Ctx) string { return c.Get("X-Who") }
	app := limitedApp(perMinuteBy(rt, key, func(c fiber.Ctx) int {
		if c.Get("X-Who") == "session" {
			return 3
		}

		return 1
	}))

	statuses := map[string][]int{}
	for _, who := range []string{"key", "key", "session", "session", "session", "session"} {
		statuses[who] = append(statuses[who], limitedGet(t, app, "/x", who).status)
	}

	if got := statuses["key"]; got[0] != 200 || got[1] != 429 {
		t.Errorf("key bucket %v, want [200 429]", got)
	}

	if got := statuses["session"]; got[2] != 200 || got[3] != 429 {
		t.Errorf("session bucket %v, want three 200 then 429", got)
	}
}

type httptestResult struct {
	status     int
	retryAfter string
	body       []byte
}

func limitedGet(t *testing.T, app *fiber.App, path, who string) *httptestResult {
	t.Helper()

	req := httptest.NewRequest("GET", path, nil)
	if who != "" {
		req.Header.Set("X-Who", who)
	}

	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = res.Body.Close() }()

	body, _ := io.ReadAll(res.Body)

	return &httptestResult{status: res.StatusCode, retryAfter: res.Header.Get(fiber.HeaderRetryAfter), body: body}
}
