// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	authdomain "github.com/yousysadmin/mailyard/internal/domain/auth"
)

// The cookie is refused from another origin and nowhere else: our own
// origin, a request-host match, a cors-listed origin, a bearer, a read,
// and a client that is not a browser all pass.
func TestACookieRequestFromAnotherOriginIsRefused(t *testing.T) {
	rt := &env.Runtime{Config: &env.Config{}}
	rt.Config.Server.PublicURL = "https://mail.example.com"
	rt.Config.CORS.Enabled = true
	rt.Config.CORS.AllowedOrigins = []string{"https://app.partner.example"}

	app := fiber.New()
	app.Use(refuseCrossSite(rt))
	ok := func(c fiber.Ctx) error { return c.SendString("ok") }
	app.Post("/x", ok)
	app.Get("/x", ok)

	for _, tc := range []struct {
		name    string
		method  string
		cookie  bool
		headers map[string]string
		want    int
	}{
		{"sibling subdomain form post", "POST", true, map[string]string{"Origin": "https://blog.example.com"}, 403},
		{"foreign origin", "POST", true, map[string]string{"Origin": "https://evil.example"}, 403},
		{"no origin but the browser says same-site", "POST", true, map[string]string{"Sec-Fetch-Site": "same-site"}, 403},
		{"no origin but the browser says cross-site", "POST", true, map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},

		{"our public origin", "POST", true, map[string]string{"Origin": "https://mail.example.com"}, 200},
		{"our public origin, other case", "POST", true, map[string]string{"Origin": "HTTPS://Mail.Example.com"}, 200},
		{"the host the request arrived on", "POST", true, map[string]string{"Origin": "http://example.com"}, 200},
		{"a cors-listed origin", "POST", true, map[string]string{"Origin": "https://app.partner.example"}, 200},
		{"same-origin per the browser", "POST", true, map[string]string{"Sec-Fetch-Site": "same-origin"}, 200},
		{"not a browser", "POST", true, nil, 200},
		{"a bearer from anywhere", "POST", false, map[string]string{"Origin": "https://evil.example", "Authorization": "Bearer myk_x"}, 200},
		{"no cookie to forge with", "POST", false, map[string]string{"Origin": "https://evil.example"}, 200},
		{"a read", "GET", true, map[string]string{"Origin": "https://evil.example"}, 200},
	} {
		req := httptest.NewRequest(tc.method, "http://example.com/x", nil)
		if tc.cookie {
			req.AddCookie(&http.Cookie{Name: authdomain.SessionCookie, Value: "s"})
		}

		for k, v := range tc.headers {
			req.Header.Set(k, v)
		}

		res, err := app.Test(req)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}

		_ = res.Body.Close()
		if res.StatusCode != tc.want {
			t.Errorf("%s: answered %d, want %d", tc.name, res.StatusCode, tc.want)
		}
	}
}
