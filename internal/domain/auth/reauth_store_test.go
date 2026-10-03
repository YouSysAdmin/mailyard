// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package auth

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/authenticator"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	userdomain "github.com/yousysadmin/mailyard/internal/domain/user"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
)

// Confirming a password inside a session spends the sign-in lockout.
// Without it a stolen session was an unlimited password oracle.
func TestReauthenticationSpendsTheSignInLockout(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	users := userdomain.NewStore(db)
	h := &Handler{Runtime: &env.Runtime{Store: &store.Store{User: users}}}

	hash, err := authenticator.HashPassword("the-right-password")
	if err != nil {
		t.Fatal(err)
	}

	u := &usermodel.User{ID: ids.New(), Email: "reauth@example.invalid", PasswordHash: hash}
	if err := users.Put(t.Context(), u); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	if !h.reauthenticated(t.Context(), u, "the-right-password") {
		t.Fatal("the right password was refused")
	}

	for i := range loginMaxFailures {
		if h.reauthenticated(t.Context(), u, "a-wrong-guess") {
			t.Fatalf("wrong guess %d was accepted", i+1)
		}
	}

	if !h.loginLocked(t.Context(), u.ID) {
		t.Fatalf("%d wrong passwords inside a session did not lock the account", loginMaxFailures)
	}

	if h.reauthenticated(t.Context(), u, "the-right-password") {
		t.Error("a locked account confirmed its password - the lockout is not consulted")
	}
}

// A refused confirmation is a 403, never the 401 the console reads as
// an expired session, and a lock says it is a lock.
func TestARefusedConfirmationSaysWhy(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	users := userdomain.NewStore(db)
	h := &Handler{Runtime: &env.Runtime{Store: &store.Store{User: users}}}

	hash, err := authenticator.HashPassword("the-right-password")
	if err != nil {
		t.Fatal(err)
	}

	u := &usermodel.User{ID: ids.New(), Email: "why@example.invalid", PasswordHash: hash}
	if err := users.Put(t.Context(), u); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	app := fiber.New()
	app.Post("/", func(c fiber.Ctx) error {
		if refusal, refused := h.refuseReauth(c, u, c.Query("pw"), "login_failed", "a test"); refused {
			return refusal
		}

		return c.SendStatus(fiber.StatusNoContent)
	})

	ask := func(pw string) (int, string) {
		t.Helper()
		res, err := app.Test(httptest.NewRequest(fiber.MethodPost, "/?pw="+pw, nil))
		if err != nil {
			t.Fatal(err)
		}

		defer func() { _ = res.Body.Close() }()

		body, _ := io.ReadAll(res.Body)

		return res.StatusCode, string(body)
	}

	if status, body := ask("a-wrong-guess"); status != fiber.StatusForbidden || !strings.Contains(body, "current password is incorrect") {
		t.Fatalf("wrong password: %d %s", status, body)
	}

	for range loginMaxFailures {
		h.recordPasswordFailure(t.Context(), u.ID)
	}

	if status, body := ask("the-right-password"); status != fiber.StatusForbidden || !strings.Contains(body, "temporarily locked") {
		t.Fatalf("locked account: %d %s", status, body)
	}
}
