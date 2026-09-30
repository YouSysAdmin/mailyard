// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package auth

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
)

// Over HTTPS the cookie carries the __Host- prefix, on plain HTTP the
// bare name, and a request is read only under the name this server
// would mint for it - so over HTTPS a bare cookie, which a sibling host
// can set, is not a session.
func TestTheSessionCookieNameFollowsTheConnection(t *testing.T) {
	if sessionCookieName(true) != SessionCookieHost || sessionCookieName(false) != SessionCookie {
		t.Fatal("cookie name does not follow Secure")
	}

	both := SessionCookie + "=bare; " + SessionCookieHost + "=prefixed"
	for name, tc := range map[string]struct{ publicURL, cookie, want string }{
		"https reads the prefixed":  {"https://m.example.test", both, "prefixed"},
		"https ignores the bare":    {"https://m.example.test", SessionCookie + "=bare", ""},
		"http reads the bare":       {"http://localhost:3000", both, "bare"},
		"http ignores the prefixed": {"http://localhost:3000", SessionCookieHost + "=prefixed", ""},
		"neither":                   {"https://m.example.test", "", ""},
	} {
		rt := &env.Runtime{Config: &env.Config{}}
		rt.Config.Server.PublicURL = tc.publicURL
		app := fiber.New()
		app.Get("/", func(c fiber.Ctx) error { return c.SendString(SessionCookieValue(c, rt)) })

		req := httptest.NewRequest("GET", "/", nil)
		if tc.cookie != "" {
			req.Header.Set("Cookie", tc.cookie)
		}

		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}

		var got [64]byte
		n, _ := resp.Body.Read(got[:])
		_ = resp.Body.Close()
		if string(got[:n]) != tc.want {
			t.Errorf("%s: read %q, want %q", name, got[:n], tc.want)
		}
	}
}
