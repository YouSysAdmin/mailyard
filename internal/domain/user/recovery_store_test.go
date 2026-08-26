// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package user

import (
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
)

// A recovery code is spent by the row, not by the reader: the second
// claim of the same hash finds nothing unspent, whichever node makes
// it. Replacing the set voids what was left of the old one.
func TestARecoveryCodeIsSpentOnce(t *testing.T) {
	s, ctx := userStore(t)

	u := &usermodel.User{
		ID: ids.New(), Email: "eve@x.test", AccountType: usermodel.AccountLocal,
		PasswordHash: "hash", EmailVerified: true,
	}
	if err := s.Put(ctx, u); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := s.ReplaceRecoveryCodes(ctx, u.ID, []string{"h1", "h2", "h3"}); err != nil {
		t.Fatalf("replace: %v", err)
	}

	if n, _ := s.RemainingRecoveryCodes(ctx, u.ID); n != 3 {
		t.Fatalf("remaining = %d, want 3", n)
	}

	ok, err := s.ClaimRecoveryCode(ctx, u.ID, "h2")
	if err != nil || !ok {
		t.Fatalf("first claim: ok=%v err=%v", ok, err)
	}

	ok, err = s.ClaimRecoveryCode(ctx, u.ID, "h2")
	if err != nil || ok {
		t.Fatalf("a spent code was honoured again: ok=%v err=%v", ok, err)
	}

	if ok, _ := s.ClaimRecoveryCode(ctx, u.ID, "nope"); ok {
		t.Fatal("an unknown code was honoured")
	}

	if n, _ := s.RemainingRecoveryCodes(ctx, u.ID); n != 2 {
		t.Fatalf("remaining = %d, want 2", n)
	}

	if err := s.ReplaceRecoveryCodes(ctx, u.ID, []string{"n1"}); err != nil {
		t.Fatalf("replace again: %v", err)
	}

	if ok, _ := s.ClaimRecoveryCode(ctx, u.ID, "h1"); ok {
		t.Fatal("a code from the replaced set was honoured")
	}

	if err := s.DeleteRecoveryCodes(ctx, u.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if n, _ := s.RemainingRecoveryCodes(ctx, u.ID); n != 0 {
		t.Fatalf("remaining after delete = %d, want 0", n)
	}
}
