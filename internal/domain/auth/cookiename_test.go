// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package auth

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// Over HTTPS the cookie carries the __Host- prefix, on plain HTTP the
// bare name, and a request is read under either, the prefixed one
// winning.
func TestTheSessionCookieNameFollowsTheConnection(t *testing.T) {
	if sessionCookieName(true) != SessionCookieHost || sessionCookieName(false) != SessionCookie {
		t.Fatal("cookie name does not follow Secure")
	}

	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error { return c.SendString(SessionCookieValue(c)) })
	for name, tc := range map[string]struct{ cookie, want string }{
		"bare":     {SessionCookie + "=old", "old"},
		"prefixed": {SessionCookieHost + "=new", "new"},
		"both":     {SessionCookie + "=old; " + SessionCookieHost + "=new", "new"},
		"neither":  {"", ""},
	} {
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
		if string(got[:n]) != tc.want {
			t.Errorf("%s: read %q, want %q", name, got[:n], tc.want)
		}
	}
}
