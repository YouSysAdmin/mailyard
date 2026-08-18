// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package project

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
)

// twoOwners seeds a project with two owners and returns their ids.
func twoOwners(t *testing.T, s *Store, ctx context.Context) (projID, a, b string) {
	t.Helper()

	projID, a, b = ids.New(), ids.New(), ids.New()
	if _, err := s.DB().ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, owner_id, created_at)
		VALUES ($1, 'acme', $2, NULL, now())`, projID, projID); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	for _, u := range []string{a, b} {
		if _, err := s.DB().ExecContext(ctx, `
			INSERT INTO users (id, email, account_type, created_at)
			VALUES ($1, $2, 1, now())`, u, u+"@x.test"); err != nil {
			t.Fatalf("seed user: %v", err)
		}

		if err := s.PutMember(ctx, &projmodel.Member{ProjectID: projID, UserID: u}); err != nil {
			t.Fatalf("seed member: %v", err)
		}

		if _, err := s.SetMemberOwner(ctx, projID, u, true); err != nil {
			t.Fatalf("seed owner: %v", err)
		}
	}

	return projID, a, b
}

func ownerCount(t *testing.T, s *Store, ctx context.Context, projID string) int {
	t.Helper()

	var n int
	if err := s.DB().QueryRowContext(ctx,
		`SELECT count(*) FROM project_members WHERE project_id = $1 AND owner`, projID).Scan(&n); err != nil {
		t.Fatalf("count owners: %v", err)
	}

	return n
}

// The owner rule is enforced by LOCKING the rows it counts, and that is
// what this asserts - deterministically, by holding the lock first.
//
// Firing two goroutines at it does not discriminate: both DELETEs are one
// fast statement, so they almost always serialize by luck, and such a
// test passes against the broken code too. Tried it, and it did - which
// is worse than no test, because it reads as proof.
//
// So instead: a peer transaction locks the other owner's row, and
// removing this one must then BLOCK. That is the discriminating shape,
// and getting there took two wrong turns worth recording - locking the
// TARGET row blocks both versions, because a DELETE locks its own row
// anyway. What separates them is the row the RULE counts: the old guard
// read it through the snapshot and sailed past a held lock, answering
// immediately, which is exactly how two concurrent removals could each
// believe the other owner was still there and leave the project with
// none.
func TestAnOwnerMutationWaitsForTheOwnerRows(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	peer := dbtest.Peer(t)
	s := &Store{Base: database.NewBase(db)}
	ctx := context.Background()

	projID, a, b := twoOwners(t, s, ctx)

	// Hold the other owner's row - deliberately not the one being
	// removed. Locking the target would block either version, since a
	// DELETE takes a lock on its own row: it is the rows the RULE counts
	// that the old guard read without locking.
	tx, err := peer.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin peer tx: %v", err)
	}

	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`SELECT user_id FROM project_members
		   WHERE project_id = $1 AND user_id = $2 FOR UPDATE`,
		projID, b); err != nil {
		t.Fatalf("peer lock: %v", err)
	}

	// The mutation must not be able to decide anything while they are held.
	waited, cancel := context.WithTimeout(ctx, 700*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, rerr := s.RemoveMember(waited, projID, a)
		done <- rerr
	}()

	select {
	case rerr := <-done:
		if rerr == nil {
			t.Fatal("RemoveMember completed while another session held the owner rows - " +
				"it is deciding the last-owner question off an unlocked snapshot")
		}

		if !errors.Is(rerr, context.DeadlineExceeded) {
			t.Fatalf("blocked as it should but failed for another reason: %v", rerr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RemoveMember neither blocked nor returned")
	}

	// Deliberately no "and now it succeeds" half. Cancelling a query
	// makes database/sql discard the connection, and dbtest.Open pins the
	// handle to one connection whose search_path was set at open - so the
	// replacement lands in the wrong schema and every later statement
	// reports the table missing. That is the harness, not the code, and
	// the functional path is covered sequentially below.
}

// Deleting a role and naming it the default must not both succeed.
//
// Both statements guarded themselves with a subquery over a row they do
// not write, so both read it from a snapshot: SetDefaultRole locks the
// projects row and reads project_roles, DeleteRole deletes the role and
// reads projects. Neither sees the other, both commit, and the project is
// left naming a role that is gone - which the member join reads as "no
// role", so every member without one of their own loses everything at
// once.
//
// Asserted the way the owner race above is, and for the same reason: two
// goroutines serialize by luck and prove nothing. A peer transaction
// holds the project row - the row SetDefaultRole writes - and the delete
// must then block on it.
func TestDeletingARoleWaitsForTheProjectRow(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	peer := dbtest.Peer(t)
	s := &Store{Base: database.NewBase(db)}
	ctx := context.Background()

	projID := ids.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, owner_id, created_at)
		VALUES ($1, 'acme', $2, NULL, now())`, projID, projID); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	role := &projmodel.Role{ID: ids.New(), ProjectID: projID, Name: "Editors"}
	if err := s.PutRole(ctx, role); err != nil {
		t.Fatalf("seed role: %v", err)
	}

	tx, err := peer.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin peer tx: %v", err)
	}

	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`SELECT id FROM projects WHERE id = $1 FOR UPDATE`, projID); err != nil {
		t.Fatalf("peer lock: %v", err)
	}

	waited, cancel := context.WithTimeout(ctx, 700*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, _, _, derr := s.DeleteRole(waited, projID, role.ID)
		done <- derr
	}()

	select {
	case derr := <-done:
		if derr == nil {
			t.Fatal("DeleteRole completed while another session held the project row - " +
				"it is deciding the is-it-the-default question off an unlocked snapshot")
		}

		if !errors.Is(derr, context.DeadlineExceeded) {
			t.Fatalf("blocked as it should but failed for another reason: %v", derr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("DeleteRole neither blocked nor returned")
	}

	// No "and now it succeeds" half, for the reason on the owner test
	// above: cancelling a query makes database/sql discard the connection
	// and dbtest.Open pins one whose search_path was set at open. The
	// functional paths are covered sequentially in role_store_test.go.
}

// The ordinary rules still hold, or the lock would have replaced a race
// with a refusal that never lifts.
func TestTheLastOwnerIsStillRefusedAndTheRestStillWork(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := context.Background()

	projID, a, b := twoOwners(t, s, ctx)

	// One of two may go.
	removed, err := s.RemoveMember(ctx, projID, a)
	if err != nil || !removed {
		t.Fatalf("removing one of two owners: removed=%v err=%v", removed, err)
	}

	// The last one may not, by either route.
	if removed, err := s.RemoveMember(ctx, projID, b); err != nil || removed {
		t.Errorf("removed the last owner: removed=%v err=%v", removed, err)
	}

	if changed, err := s.SetMemberOwner(ctx, projID, b, false); err != nil || changed {
		t.Errorf("demoted the last owner: changed=%v err=%v", changed, err)
	}

	if got := ownerCount(t, s, ctx, projID); got != 1 {
		t.Errorf("owners = %d, want the last one intact", got)
	}

	// Promotion is never guarded, and a member who is not there is
	// false rather than an error.
	if changed, err := s.SetMemberOwner(ctx, projID, a, true); err != nil || changed {
		t.Errorf("promoting a removed member: changed=%v err=%v", changed, err)
	}
}
