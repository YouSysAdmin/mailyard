// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpcredential

import (
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	scmodel "github.com/yousysadmin/mailyard/internal/models/smtpcredential"
)

// An edit written from a copy read before a revoke must not bring the
// credential back.
func TestAStaleCredentialEditDoesNotUndoARevoke(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := NewStore(db)
	ctx := t.Context()

	const projID = "e66e7a4d-9e6c-4884-869a-cf9ffcf22181"
	dbtest.Schema(t, db, `
        INSERT INTO projects (id, name, slug, default_language, created_at)
        VALUES ('`+projID+`', 'Test', 'test', 'en', now())`)

	cred := &scmodel.Credential{
		ID: ids.New(), ProjectID: projID, Name: "relay", Username: "relay-1",
		PasswordHash: "x", AllowedIPs: []string{},
	}
	if err := s.Put(ctx, cred); err != nil {
		t.Fatalf("put: %v", err)
	}

	stale, err := s.Get(ctx, projID, cred.ID)
	if err != nil || stale == nil {
		t.Fatalf("get: %v", err)
	}

	if err := s.Revoke(ctx, projID, cred.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	stale.Name = "renamed"
	if err := s.Put(ctx, stale); err != nil {
		t.Fatalf("stale put: %v", err)
	}

	got, err := s.Get(ctx, projID, cred.ID)
	if err != nil || got == nil {
		t.Fatalf("read back: %v", err)
	}

	if !got.Revoked {
		t.Error("a PATCH from a copy read before the revoke made the credential usable again")
	}

	if got.Name != "renamed" {
		t.Errorf("name = %q, want the edit kept", got.Name)
	}
}
