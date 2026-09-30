// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/authenticator"
	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain/session"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	"github.com/yousysadmin/mailyard/internal/domain/user"
	sessmodel "github.com/yousysadmin/mailyard/internal/models/session"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
)

// Logout resolves the session the gates would: a request naming a
// bearer is judged on it, and a made-up bearer beside a live cookie
// resolves nothing rather than the cookie's session - which is what
// the cross-site check exempted it on.
func TestLogoutJudgesABearerBeforeTheCookie(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	users := user.NewStore(db)
	u := &usermodel.User{ID: "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5c1", Email: "a@example.com"}
	if err := users.Put(t.Context(), u); err != nil {
		t.Fatal(err)
	}

	sessions := session.NewStore(db)
	sess := &sessmodel.Session{ID: "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5c2", UserID: u.ID,
		ExpiresAt: time.Now().Add(time.Hour)}
	if err := sessions.Put(t.Context(), sess); err != nil {
		t.Fatal(err)
	}

	rt := &env.Runtime{Config: &env.Config{}, Store: &store.Store{Session: sessions, User: users}}
	rt.Config.Auth.JWTSecret = "jwt-secret-jwt-secret-jwt-secret-jwt"
	rt.Config.Server.PublicURL = "http://mail.example.com"
	token, err := authenticator.CreateToken(
		crypto.DeriveKey(rt.Config.Auth.JWTSecret, crypto.KeySession), u.ID, u.Email, sess.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	h := &Handler{Runtime: rt}
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		s, _ := h.currentSession(c)
		if s == nil {
			return c.SendString("none")
		}

		return c.SendString(s.ID)
	})

	for _, tc := range []struct {
		name, cookie, bearer, want string
	}{
		{"cookie alone", token, "", sess.ID},
		{"bearer alone", "", token, sess.ID},
		{"made-up bearer beside the cookie", token, "garbage", "none"},
	} {
		req := httptest.NewRequest(http.MethodGet, "http://mail.example.com/", nil)
		if tc.cookie != "" {
			req.AddCookie(&http.Cookie{Name: SessionCookie, Value: tc.cookie})
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
			t.Errorf("%s: resolved %q, want %q", tc.name, got, tc.want)
		}
	}
}
