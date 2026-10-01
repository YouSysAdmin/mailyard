// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
)

// A key is reserved once. While its send runs a duplicate learns only
// that it must wait, once completed it learns the message, a released
// key can be reserved again, and another project's identical key is
// its own.
func TestAnIdempotencyKeyIsReservedOnce(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := t.Context()

	mine, theirs := ids.New(), ids.New()
	for _, p := range []string{mine, theirs} {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO projects (id, name, slug, owner_id, created_at)
			VALUES ($1, 'acme', $2, NULL, now())`, p, p); err != nil {
			t.Fatalf("seed project: %v", err)
		}
	}

	existing, reserved, err := s.ReserveKey(ctx, mine, "order-1")
	if err != nil || !reserved || existing != "" {
		t.Fatalf("first reserve: %q %v %v", existing, reserved, err)
	}

	existing, reserved, err = s.ReserveKey(ctx, mine, "order-1")
	if err != nil || reserved || existing != "" {
		t.Fatalf("a duplicate while in flight: %q %v %v, want not reserved and no message", existing, reserved, err)
	}

	if _, reserved, err := s.ReserveKey(ctx, theirs, "order-1"); err != nil || !reserved {
		t.Fatalf("another project's key: %v %v", reserved, err)
	}

	emailID := ids.New()
	if err := s.CompleteKey(ctx, mine, "order-1", emailID); err != nil {
		t.Fatalf("complete: %v", err)
	}

	existing, reserved, err = s.ReserveKey(ctx, mine, "order-1")
	if err != nil || reserved || existing != emailID {
		t.Fatalf("a replay: %q %v %v, want the message", existing, reserved, err)
	}

	if err := s.ReleaseKey(ctx, mine, "order-1"); err != nil {
		t.Fatalf("release: %v", err)
	}

	if existing, _, _ := s.ReserveKey(ctx, mine, "order-1"); existing != emailID {
		t.Error("releasing a completed key forgot its message")
	}

	if _, reserved, _ := s.ReserveKey(ctx, mine, "order-2"); !reserved {
		t.Fatal("reserve order-2")
	}

	if err := s.ReleaseKey(ctx, mine, "order-2"); err != nil {
		t.Fatalf("release: %v", err)
	}

	if _, reserved, _ := s.ReserveKey(ctx, mine, "order-2"); !reserved {
		t.Error("a released key could not be reserved again")
	}

	n, err := s.PruneKeysBefore(ctx, time.Now().UTC().Add(time.Hour))
	if err != nil || n != 3 {
		t.Errorf("pruned %d keys, want 3 (%v)", n, err)
	}
}
