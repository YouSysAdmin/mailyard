// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	corepasskey "github.com/yousysadmin/mailyard/internal/core/passkey"
	"github.com/yousysadmin/mailyard/internal/domain/store"
)

// spentChallenges is the challenge ledger every node shares.
type spentChallenges struct {
	store.PasskeyStore
	mu    sync.Mutex
	spent map[string]bool
}

func (s *spentChallenges) SpendChallenge(_ context.Context, challenge string, _ time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.spent[challenge] {
		return false, nil
	}

	s.spent[challenge] = true

	return true, nil
}

// A ceremony is answerable once. Clearing the cookie stops a browser,
// so a captured request carrying the same cookie is refused by the
// spent challenge, and no cookie at all is no ceremony.
func TestACeremonyIsTakenOnce(t *testing.T) {
	rt := &env.Runtime{Config: &env.Config{}}
	rt.Config.Auth.JWTSecret = "jwt-secret-jwt-secret-jwt-secret-jwt"
	rt.Store = &store.Store{Passkey: &spentChallenges{spent: map[string]bool{}}}
	h := &Handler{Runtime: rt}

	const name = "test_ceremony"
	app := fiber.New()
	app.Get("/begin", func(c fiber.Ctx) error {
		return h.setCeremony(c, name, &corepasskey.SessionData{
			Challenge: "challenge-1", Expires: time.Now().Add(time.Minute),
		})
	})
	app.Get("/finish", func(c fiber.Ctx) error {
		sd, err := h.takeCeremony(c, name)
		if err != nil {
			return c.SendStatus(fiber.StatusForbidden)
		}

		return c.SendString(sd.Challenge)
	})

	res, err := app.Test(httptest.NewRequest(http.MethodGet, "/begin", nil))
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()
	var cookie *http.Cookie
	for _, ck := range res.Cookies() {
		if ck.Name == name {
			cookie = ck
		}
	}

	if cookie == nil {
		t.Fatal("no ceremony cookie was set")
	}

	finish := func(withCookie bool) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/finish", nil)
		if withCookie {
			req.AddCookie(cookie)
		}

		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}

		_ = res.Body.Close()

		return res.StatusCode
	}

	if got := finish(true); got != fiber.StatusOK {
		t.Fatalf("first answer: %d, want 200", got)
	}

	if got := finish(true); got != fiber.StatusForbidden {
		t.Fatalf("replayed answer: %d, want refused", got)
	}

	if got := finish(false); got != fiber.StatusForbidden {
		t.Fatalf("no ceremony: %d, want refused", got)
	}
}
