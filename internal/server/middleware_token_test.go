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

// A request naming a bearer is judged on the bearer, never on the
// ambient cookie, because refuseCrossSite exempts it on the strength
// of that bearer. With the cookie read first, a made-up bearer plus
// the cookie was a cookie-authenticated mutation with no origin check.
func TestABearerIsReadBeforeTheCookie(t *testing.T) {
	rt := &env.Runtime{Config: &env.Config{}}
	rt.Config.Server.PublicURL = "http://mail.example.com"

	app := fiber.New()
	app.Get("/x", func(c fiber.Ctx) error { return c.SendString(extractToken(c, rt)) })

	for _, tc := range []struct {
		name   string
		cookie string
		bearer string
		want   string
	}{
		{"both present", "from-cookie", "from-bearer", "from-bearer"},
		{"cookie only", "from-cookie", "", "from-cookie"},
		{"bearer only", "", "from-bearer", "from-bearer"},
		{"neither", "", "", ""},
	} {
		req := httptest.NewRequest(http.MethodGet, "http://mail.example.com/x", nil)
		if tc.cookie != "" {
			req.AddCookie(&http.Cookie{Name: authdomain.SessionCookie, Value: tc.cookie})
		}

		if tc.bearer != "" {
			req.Header.Set("Authorization", "Bearer "+tc.bearer)
		}

		res, err := app.Test(req)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}

		body := make([]byte, 64)
		n, _ := res.Body.Read(body)
		_ = res.Body.Close()
		if got := string(body[:n]); got != tc.want {
			t.Errorf("%s: extractToken = %q, want %q", tc.name, got, tc.want)
		}
	}
}
