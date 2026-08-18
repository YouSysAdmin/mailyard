// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package oauthprovider

import (
	"context"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	opmodel "github.com/yousysadmin/mailyard/internal/models/oauthprovider"
)

// SlugTaken with no exception id, which is what a CREATE passes.
//
// The same shape that shipped the SMTP group version broken: `id <> ?` on
// a uuid column, an empty string for a create, 22P02, and MalformedID
// turning that into a 404 - so creating a provider would have answered
// "not found" before inserting anything. It happens not to be reachable
// today because the handler mints the id before asking, but that is a
// property of one call site rather than of this query, and the fix has to
// be held by something.
//
// The empty path is the only thing that catches this class: `id` is a
// non-null primary key, so the null-uuid guard does not cover it, and the
// statement PREPAREs, so the schema guard is satisfied.
func TestProviderSlugTakenWorksWithNoExceptionID(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := NewStore(db, crypto.New("0123456789abcdef0123456789abcdef"))
	ctx := context.Background()

	if taken, err := s.SlugTaken(ctx, "okta", ""); err != nil || taken {
		t.Fatalf("with no exception id on an empty table: taken=%v err=%v", taken, err)
	}

	p := &opmodel.Provider{
		ID: ids.New(), Name: "Okta", Slug: "okta", Type: "oidc",
		ClientID: "abc", ClientSecret: "shh", Issuer: "https://okta.example.com",
	}
	if err := s.Put(ctx, p); err != nil {
		t.Fatalf("put: %v", err)
	}

	if taken, err := s.SlugTaken(ctx, "okta", ""); err != nil || !taken {
		t.Errorf("after the insert: taken=%v err=%v", taken, err)
	}

	// The update path: a provider keeps its own slug.
	if taken, err := s.SlugTaken(ctx, "okta", p.ID); err != nil || taken {
		t.Errorf("excepting the holder itself: taken=%v err=%v", taken, err)
	}
}
