// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package apikey

import (
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	akmodel "github.com/yousysadmin/mailyard/internal/models/apikey"
)

// An edit written from a copy read before a revoke must not bring the
// key back, on either kind of key.
func TestAStaleKeyEditDoesNotUndoARevoke(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	ctx := t.Context()

	const projID = "e66e7a4d-9e6c-4884-869a-cf9ffcf22181"
	dbtest.Schema(t, db, `
        INSERT INTO projects (id, name, slug, default_language, created_at)
        VALUES ('`+projID+`', 'Test', 'test', 'en', now())`)

	s := NewStore(db)
	k := &akmodel.Key{
		ID: ids.New(), ProjectID: projID, Name: "ci", KeyHash: "h1", KeyPrefix: "myk_aaaaaaaa",
		Permissions: []string{"emails:write"}, AllowedIPs: []string{},
	}
	if err := s.Put(ctx, k); err != nil {
		t.Fatalf("put: %v", err)
	}

	stale, err := s.Get(ctx, projID, k.ID)
	if err != nil || stale == nil {
		t.Fatalf("get: %v", err)
	}

	if err := s.Revoke(ctx, projID, k.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	stale.Name = "renamed"
	if err := s.Put(ctx, stale); err != nil {
		t.Fatalf("stale put: %v", err)
	}

	if got, err := s.Get(ctx, projID, k.ID); err != nil || got == nil || !got.Revoked || got.Name != "renamed" {
		t.Errorf("project key after a stale edit = %+v, %v, want revoked and renamed", got, err)
	}

	as := NewAdminStore(db)
	a := &akmodel.Admin{ID: ids.New(), Name: "ops", KeyHash: "h2", KeyPrefix: "mya_bbbbbbbb", AllowedIPs: []string{}}
	if err := as.Put(ctx, a); err != nil {
		t.Fatalf("put admin: %v", err)
	}

	staleAdmin, err := as.Get(ctx, a.ID)
	if err != nil || staleAdmin == nil {
		t.Fatalf("get admin: %v", err)
	}

	if err := as.Revoke(ctx, a.ID); err != nil {
		t.Fatalf("revoke admin: %v", err)
	}

	if err := as.Put(ctx, staleAdmin); err != nil {
		t.Fatalf("stale admin put: %v", err)
	}

	if got, err := as.Get(ctx, a.ID); err != nil || got == nil || !got.Revoked {
		t.Errorf("admin key after a stale edit = %+v, %v, want revoked", got, err)
	}
}
