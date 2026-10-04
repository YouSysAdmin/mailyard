// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package auth

import (
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	"github.com/yousysadmin/mailyard/internal/domain/user"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
)

// A code opens the factor once. Presented again it is refused, and a
// locked factor refuses even a right code, so a guess during the lock
// learns nothing.
func TestATOTPCodeIsSingleUseAndTheLockHolds(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)

	rt := &env.Runtime{Config: &env.Config{}}
	rt.Crypto = crypto.New("0123456789abcdef0123456789abcdef")
	rt.Store = &store.Store{User: user.NewStore(db)}
	h := &Handler{Runtime: rt}
	ctx := t.Context()

	newUser := func(email string) (*usermodel.User, string) {
		key, err := totp.Generate(totp.GenerateOpts{Issuer: "Mailyard", AccountName: email})
		if err != nil {
			t.Fatal(err)
		}

		sealed, err := rt.Crypto.Encrypt(key.Secret())
		if err != nil {
			t.Fatal(err)
		}

		u := &usermodel.User{ID: ids.New(), Email: email, EmailVerified: true, TOTPEnabled: true, TOTPSecret: sealed}
		if err := rt.Store.User.Put(ctx, u); err != nil {
			t.Fatal(err)
		}

		return u, key.Secret()
	}

	u, secret := newUser("once@example.com")
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	if !h.consumeTOTP(ctx, u.ID, u.TOTPSecret, code) {
		t.Fatal("a right code was refused")
	}

	if h.consumeTOTP(ctx, u.ID, u.TOTPSecret, code) {
		t.Fatal("the same code opened the factor twice")
	}

	locked, secret := newUser("locked@example.com")
	for range totpMaxFailures {
		if h.consumeTOTP(ctx, locked.ID, locked.TOTPSecret, "000000") {
			t.Fatal("a wrong code was accepted")
		}
	}

	code, err = totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	if h.consumeTOTP(ctx, locked.ID, locked.TOTPSecret, code) {
		t.Fatal("a right code opened a locked factor")
	}
}
