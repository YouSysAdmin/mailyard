// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package cli

import (
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	"github.com/yousysadmin/mailyard/internal/domain/user"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
)

// SSO-only boots once an account exists, and refuses an empty users
// table, where nobody could ever configure a provider.
func TestSSOOnlyNeedsAnAccount(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	rt := &env.Runtime{Store: &store.Store{User: user.NewStore(db)}}

	if err := requireAnAccount(t.Context(), rt); err == nil {
		t.Fatal("an empty users table was accepted")
	}

	if err := rt.Store.User.Put(t.Context(), &usermodel.User{
		ID: ids.New(), Email: "admin@example.com", Admin: true, EmailVerified: true,
	}); err != nil {
		t.Fatal(err)
	}

	if err := requireAnAccount(t.Context(), rt); err != nil {
		t.Fatalf("an installation with an account was refused: %v", err)
	}
}
