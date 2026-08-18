// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package user

import (
	"context"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
)

func userStore(t *testing.T) (*Store, context.Context) {
	t.Helper()

	db := dbtest.Open(t)
	dbtest.Migrate(t, db)

	return &Store{Base: database.NewBase(db)}, context.Background()
}

// A password reset must not re-enable a disabled account.
//
// Both reset paths read the user, set PasswordHash and called Put - which
// writes the whole row. So an administrator disabling the account between
// that read and that write had the change silently undone, and the
// account they had just locked out signed straight back in with its new
// password. `admin` and `email_verified` rode along the same way.
//
// The interleaving is modelled rather than raced, because the race is not
// the point: the write is unconditional, so it loses whatever changed in
// between however wide the gap is.
func TestChangingAPasswordDoesNotUndoADisable(t *testing.T) {
	s, ctx := userStore(t)

	u := &usermodel.User{
		ID: ids.New(), Email: "bob@x.test", AccountType: usermodel.AccountLocal,
		PasswordHash: "old-hash", EmailVerified: true,
	}
	if err := s.Put(ctx, u); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// The handler has read the user by now. An administrator disables the
	// account and makes it an admin - two independent columns.
	if _, err := s.DB().ExecContext(ctx,
		`UPDATE users SET disabled = TRUE, admin = TRUE WHERE id = $1`, u.ID); err != nil {
		t.Fatalf("admin change: %v", err)
	}

	// And now the reset lands, holding the pre-change row.
	if err := s.SetPassword(ctx, u.ID, "new-hash"); err != nil {
		t.Fatalf("set password: %v", err)
	}

	got, err := s.GetByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	if got.PasswordHash != "new-hash" {
		t.Errorf("password_hash = %q, want the new hash", got.PasswordHash)
	}

	if !got.Disabled {
		t.Error("the account was re-enabled by its own password reset")
	}

	if !got.Admin {
		t.Error("the admin flag was rolled back by a password write")
	}
}

// Enrolling or clearing a second factor says nothing about the rest of
// the account either.
func TestSettingTheSecondFactorTouchesOnlyItsOwnColumns(t *testing.T) {
	s, ctx := userStore(t)

	u := &usermodel.User{
		ID: ids.New(), Email: "bob@x.test", AccountType: usermodel.AccountLocal,
		PasswordHash: "hash", EmailVerified: true,
	}
	if err := s.Put(ctx, u); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := s.DB().ExecContext(ctx,
		`UPDATE users SET disabled = TRUE WHERE id = $1`, u.ID); err != nil {
		t.Fatalf("admin change: %v", err)
	}

	if err := s.SetTOTP(ctx, u.ID, "sealed-secret", true); err != nil {
		t.Fatalf("set totp: %v", err)
	}

	got, err := s.GetByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	if got.TOTPSecret != "sealed-secret" || !got.TOTPEnabled {
		t.Errorf("totp = (%q, %v), want the enrolment stored", got.TOTPSecret, got.TOTPEnabled)
	}

	if !got.Disabled {
		t.Error("enrolling a second factor re-enabled a disabled account")
	}

	// And clearing it is the same act in reverse.
	if err := s.SetTOTP(ctx, u.ID, "", false); err != nil {
		t.Fatalf("clear totp: %v", err)
	}

	got, err = s.GetByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	if got.TOTPSecret != "" || got.TOTPEnabled {
		t.Error("the second factor was not cleared")
	}

	if !got.Disabled || got.PasswordHash != "hash" {
		t.Error("clearing the second factor rewrote something else")
	}
}
