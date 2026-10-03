// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/pquerna/otp/totp"

	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain/session"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	"github.com/yousysadmin/mailyard/internal/domain/user"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
)

// An identity provider sign-in to an account with two-factor auth on
// opens no session until the code arrives, and the code is checked like
// the one on a password sign-in.
func TestAnIdentityProviderSignInOwesTheSecondFactor(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)

	rt := &env.Runtime{Config: &env.Config{}}
	rt.Config.Auth.JWTSecret = "jwt-secret-jwt-secret-jwt-secret-jwt"
	rt.Crypto = crypto.New("0123456789abcdef0123456789abcdef")
	rt.Store = &store.Store{User: user.NewStore(db), Session: session.NewStore(db)}
	h := &Handler{Runtime: rt}

	key, err := totp.Generate(totp.GenerateOpts{Issuer: "Mailyard", AccountName: "sso@example.com"})
	if err != nil {
		t.Fatal(err)
	}

	sealed, err := rt.Crypto.Encrypt(key.Secret())
	if err != nil {
		t.Fatal(err)
	}

	u := &usermodel.User{ID: ids.New(), Email: "sso@example.com", EmailVerified: true,
		TOTPEnabled: true, TOTPSecret: sealed}
	if err := rt.Store.User.Put(t.Context(), u); err != nil {
		t.Fatal(err)
	}

	app := fiber.New()
	app.Get("/pending", func(c fiber.Ctx) error {
		return h.setPendingSecondFactor(c, pendingSecondFactor{
			UserID: u.ID, ProviderSlug: "idp", Expires: time.Now().Add(time.Minute),
		})
	})
	app.Post("/2fa", h.OAuthSecondFactor)

	res, err := app.Test(httptest.NewRequest(http.MethodGet, "/pending", nil))
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	var pending *http.Cookie
	for _, ck := range res.Cookies() {
		if ck.Name == oidcSecondFactorCookie {
			pending = ck
		}
	}

	if pending == nil {
		t.Fatal("no pending cookie was set")
	}

	submit := func(code string, withCookie bool) (int, bool) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/2fa", strings.NewReader(`{"totp_code":"`+code+`"}`))
		req.Header.Set("Content-Type", "application/json")
		if withCookie {
			req.AddCookie(pending)
		}

		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}

		_ = res.Body.Close()

		signedIn := false
		for _, ck := range res.Cookies() {
			if strings.HasSuffix(ck.Name, SessionCookie) && ck.Value != "" {
				signedIn = true
			}
		}

		return res.StatusCode, signedIn
	}

	if status, signedIn := submit("000000", false); status != fiber.StatusUnauthorized || signedIn {
		t.Fatalf("no pending sign-in: %d signed in %v", status, signedIn)
	}

	if status, signedIn := submit("12345a", true); status != fiber.StatusUnauthorized || signedIn {
		t.Fatalf("wrong code: %d signed in %v", status, signedIn)
	}

	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatal(err)
	}

	if status, signedIn := submit(code, true); status != fiber.StatusOK || !signedIn {
		t.Fatalf("right code: %d signed in %v", status, signedIn)
	}
}
