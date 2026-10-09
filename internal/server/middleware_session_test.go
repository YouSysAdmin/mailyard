// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/authenticator"
	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/env"
)

// A correctly signed token with no jti names no session row, so nothing
// could revoke it. The gate refuses it.
func TestASessionTokenWithoutAJTIIsRefused(t *testing.T) {
	rt := &env.Runtime{Config: &env.Config{}}
	rt.Config.Server.PublicURL = "http://mail.example.com"
	rt.Config.Auth.JWTSecret = "jwt-secret-jwt-secret-jwt-secret-jwt"

	token, err := authenticator.CreateToken(
		crypto.DeriveKey(rt.Config.Auth.JWTSecret, crypto.KeySession),
		"0190f000-0000-7000-8000-0000000000aa", "a@example.com", "", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	app := fiber.New()
	app.Get("/x", func(c fiber.Ctx) error {
		if _, ok := resolveSession(c, rt); ok {
			return c.SendString("accepted")
		}

		return c.SendString("refused")
	})

	req := httptest.NewRequest(http.MethodGet, "http://mail.example.com/x", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}

	body := make([]byte, 16)
	n, _ := res.Body.Read(body)
	_ = res.Body.Close()
	if got := string(body[:n]); got != "refused" {
		t.Fatalf("a token with no jti was %s, want refused", got)
	}
}
