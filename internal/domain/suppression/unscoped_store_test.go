// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package suppression

import (
	"context"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	supmodel "github.com/yousysadmin/mailyard/internal/models/suppression"
)

// An UNSCOPED send passes no list, and unsubscribe_list_id is a uuid
// column - so "" reached Postgres as a uuid literal and it refused the
// whole statement.
//
// This runs on every send, so getting it wrong is the loudest possible
// version of the bug: every ordinary send answers 500 at the
// suppression check, with "invalid input syntax for type uuid" in the
// log and nothing in it naming the send. Seen from the console
// before it was fixed.
//
// A list-scoped send worked throughout, which is why the tests that
// existed stayed green.
func TestAnUnscopedSendConsultsGlobalBlocksOnly(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := context.Background()

	proj := ids.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, owner_id, created_at)
		VALUES ($1, 'acme', $2, NULL, now())`, proj, proj); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	list := ids.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO unsubscribe_lists (id, project_id, name, created_at)
		VALUES ($1, $2, 'News', now())`, list, proj); err != nil {
		t.Fatalf("seed list: %v", err)
	}

	// A global block, and an opt-out from one list.
	if err := s.Upsert(ctx, &supmodel.Suppression{
		ProjectID: proj, Email: "blocked@example.com", Kind: "manual",
	}); err != nil {
		t.Fatalf("upsert global: %v", err)
	}

	if err := s.Upsert(ctx, &supmodel.Suppression{
		ProjectID: proj, Email: "optout@example.com", Kind: "manual",
		UnsubscribeListID: list,
	}); err != nil {
		t.Fatalf("upsert list opt-out: %v", err)
	}

	for _, tc := range []struct {
		name  string
		email string
		list  string
		want  bool
	}{
		{"global block, unscoped send", "blocked@example.com", "", true},
		{"global block, scoped send", "blocked@example.com", list, true},
		// The one that matters: a list opt-out must not stop unrelated
		// mail, and asking that question must not crash.
		{"list opt-out, unscoped send", "optout@example.com", "", false},
		{"list opt-out, its own list", "optout@example.com", list, true},
		{"nobody, unscoped send", "fine@example.com", "", false},
	} {
		got, err := s.IsSuppressedForList(ctx, proj, tc.email, tc.list)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}

		if got != tc.want {
			t.Errorf("%s: suppressed = %v, want %v", tc.name, got, tc.want)
		}
	}

	// IsSuppressed is the same question with no list at all - it is
	// what the send path calls.
	if _, err := s.IsSuppressed(ctx, proj, "anyone@example.com"); err != nil {
		t.Errorf("IsSuppressed: %v", err)
	}
}
