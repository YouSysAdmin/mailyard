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

// seedScopes puts one global block and one opt-out per list on the same
// address, which is the shape the unique key exists to allow.
func seedScopes(t *testing.T, s *Store, ctx context.Context, email string) (proj, listA, listB string) {
	t.Helper()

	proj, listA, listB = ids.New(), ids.New(), ids.New()
	if _, err := s.DB().ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, owner_id, created_at)
		VALUES ($1, 'acme', $2, NULL, now())`, proj, proj); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	for i, l := range []string{listA, listB} {
		if _, err := s.DB().ExecContext(ctx, `
			INSERT INTO unsubscribe_lists (id, project_id, name, created_at)
			VALUES ($1, $2, $3, now())`, l, proj, []string{"News", "Offers"}[i]); err != nil {
			t.Fatalf("seed list: %v", err)
		}
	}

	if err := s.Upsert(ctx, &supmodel.Suppression{
		ProjectID: proj, Email: email, Kind: supmodel.KindHard,
		Reason: "mailbox full",
	}); err != nil {
		t.Fatalf("upsert global: %v", err)
	}

	for _, l := range []string{listA, listB} {
		if err := s.Upsert(ctx, &supmodel.Suppression{
			ProjectID: proj, Email: email, Kind: supmodel.KindListUnsubscribe,
			UnsubscribeListID: l,
		}); err != nil {
			t.Fatalf("upsert opt-out: %v", err)
		}
	}

	return proj, listA, listB
}

func scopesLeft(t *testing.T, s *Store, ctx context.Context, proj, email string) (global bool, lists int) {
	t.Helper()

	rows, err := s.DB().QueryContext(ctx, `
		SELECT unsubscribe_list_id IS NULL FROM suppressions
		WHERE project_id = $1 AND email = $2`, proj, email)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var isGlobal bool
		if err := rows.Scan(&isGlobal); err != nil {
			t.Fatalf("scan: %v", err)
		}

		if isGlobal {
			global = true
		} else {
			lists++
		}
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	return global, lists
}

// Unblocking an address must not undo what that person asked for.
//
// Delete was `WHERE project_id = ? AND email = ?` on a table whose
// unique key has three columns, so pressing Remove on a hard bounce
// deleted the global block AND every list opt-out the address had made -
// silently putting them back on lists they had left through a one-click
// RFC 8058 link. Nothing in the confirmation said anything about lists,
// because nothing in the code knew there were any.
func TestUnblockingAnAddressKeepsItsListOptOuts(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := context.Background()

	const email = "bob@x.test"
	proj, listA, listB := seedScopes(t, s, ctx, email)

	ok, err := s.Delete(ctx, proj, email)
	if err != nil || !ok {
		t.Fatalf("delete global: ok=%v err=%v", ok, err)
	}

	global, lists := scopesLeft(t, s, ctx, proj, email)
	if global {
		t.Error("the global block survived its own removal")
	}

	if lists != 2 {
		t.Errorf("%d list opt-outs left, want 2 - unblocking re-subscribed somebody who asked to leave", lists)
	}

	// The send path must agree: unrelated mail flows again, the two lists
	// stay closed.
	for _, tc := range []struct {
		what string
		list string
		want bool
	}{
		{"unscoped send", "", false},
		{"list A", listA, true},
		{"list B", listB, true},
	} {
		got, err := s.IsSuppressedForList(ctx, proj, email, tc.list)
		if err != nil {
			t.Errorf("%s: %v", tc.what, err)
			continue
		}

		if got != tc.want {
			t.Errorf("%s: suppressed = %v, want %v", tc.what, got, tc.want)
		}
	}
}

// One list opt-out is lifted on its own, and an empty list id means the
// global row rather than a 22P02 on a uuid column.
func TestALiftedListOptOutTakesOnlyThatList(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := context.Background()

	const email = "bob@x.test"
	proj, listA, listB := seedScopes(t, s, ctx, email)

	ok, err := s.DeleteForList(ctx, proj, email, listA)
	if err != nil || !ok {
		t.Fatalf("delete list A: ok=%v err=%v", ok, err)
	}

	global, lists := scopesLeft(t, s, ctx, proj, email)
	if !global || lists != 1 {
		t.Errorf("global=%v lists=%d, want the global block and list B intact", global, lists)
	}

	if got, err := s.IsSuppressedForList(ctx, proj, email, listB); err != nil || !got {
		t.Errorf("list B: suppressed = %v err = %v, want it still closed", got, err)
	}

	// An empty list id addresses the global row here, the same one
	// Delete takes. Without NullStr this was `''::uuid` and answered
	// 22P02, which MalformedID softens into a 404 - a caller would read
	// "no such opt-out" for a statement that never ran.
	ok, err = s.DeleteForList(ctx, proj, email, "")
	if err != nil || !ok {
		t.Fatalf("delete with an empty list id: ok=%v err=%v", ok, err)
	}

	if global, _ := scopesLeft(t, s, ctx, proj, email); global {
		t.Error("an empty list id did not address the global row")
	}
}

// Erasure is the one caller that wants every scope, and it says so.
func TestErasureTakesEveryScope(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := context.Background()

	const email = "bob@x.test"
	proj, _, _ := seedScopes(t, s, ctx, email)

	// Somebody else's rows, to prove the address is the bound.
	if err := s.Upsert(ctx, &supmodel.Suppression{
		ProjectID: proj, Email: "other@x.test", Kind: supmodel.KindManual,
	}); err != nil {
		t.Fatalf("upsert other: %v", err)
	}

	n, err := s.PurgeForAddress(ctx, proj, email)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}

	if n != 3 {
		t.Errorf("purged %d rows, want 3 - erasure left a record of somebody we were asked to forget", n)
	}

	if global, lists := scopesLeft(t, s, ctx, proj, email); global || lists != 0 {
		t.Errorf("global=%v lists=%d, want nothing", global, lists)
	}

	if global, _ := scopesLeft(t, s, ctx, proj, "other@x.test"); !global {
		t.Error("erasure took another address's suppression")
	}
}
