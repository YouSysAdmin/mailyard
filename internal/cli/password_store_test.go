// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package cli

import (
	"io"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/authenticator"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain/passwordreset"
	"github.com/yousysadmin/mailyard/internal/domain/session"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	"github.com/yousysadmin/mailyard/internal/domain/user"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
)

// The new password is the one that verifies afterwards, on an enabled
// account and on a disabled one, which the command also enables.
func TestSetPasswordLeavesTheNewPassword(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	st := &store.Store{
		User:          user.NewStore(db),
		PasswordReset: passwordreset.NewStore(db),
		Session:       session.NewStore(db),
	}

	oldHash, err := authenticator.HashPassword("the-old-password")
	if err != nil {
		t.Fatal(err)
	}

	newHash, err := authenticator.HashPassword("the-new-password")
	if err != nil {
		t.Fatal(err)
	}

	for _, disabled := range []bool{false, true} {
		email := "enabled@example.com"
		if disabled {
			email = "disabled@example.com"
		}

		if err := st.User.Put(t.Context(), &usermodel.User{
			ID: ids.New(), Email: email, PasswordHash: oldHash, Disabled: disabled, EmailVerified: true,
		}); err != nil {
			t.Fatal(err)
		}

		if err := applyPassword(t.Context(), st, email, newHash, io.Discard); err != nil {
			t.Fatalf("%s: %v", email, err)
		}

		u, err := st.User.Get(t.Context(), email)
		if err != nil || u == nil {
			t.Fatalf("%s: read back: %v", email, err)
		}

		if !authenticator.VerifyPassword(u.PasswordHash, "the-new-password") {
			t.Errorf("%s: the new password does not verify", email)
		}

		if authenticator.VerifyPassword(u.PasswordHash, "the-old-password") {
			t.Errorf("%s: the old password still verifies", email)
		}

		if u.Disabled {
			t.Errorf("%s: still disabled", email)
		}
	}
}
