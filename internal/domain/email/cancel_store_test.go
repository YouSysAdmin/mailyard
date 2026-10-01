// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"context"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
)

func seedStatus(t *testing.T, s *Store, ctx context.Context, projID, id, status string, at time.Time) {
	t.Helper()

	if _, err := s.DB().ExecContext(ctx, `
		INSERT INTO emails (id, project_id, sender, recipients, subject, status, created_at, next_attempt_at)
		VALUES ($1, $2, 'a@b.test', '["c@d.test"]', 's', $3, $4, $4)`, id, projID, status, at); err != nil {
		t.Fatalf("seed email: %v", err)
	}
}

// A message waiting in the queue can be withdrawn, one a worker holds
// or has finished cannot, and a withdrawn one is no longer anything
// the claim would take.
func TestCancelTakesOnlyWhatNoWorkerHolds(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := t.Context()

	projID := ids.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, owner_id, created_at)
		VALUES ($1, 'acme', $2, NULL, now())`, projID, projID); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	at := time.Now().UTC().Truncate(time.Microsecond)
	cases := map[string]bool{
		emailmodel.StatusScheduled:  true,
		emailmodel.StatusQueued:     true,
		emailmodel.StatusPending:    true,
		emailmodel.StatusProcessing: false,
		emailmodel.StatusSent:       false,
		emailmodel.StatusFailed:     false,
	}
	for status, want := range cases {
		id := ids.New()
		seedStatus(t, s, ctx, projID, id, status, at)
		got, err := s.Cancel(ctx, projID, id, at)
		if err != nil {
			t.Fatalf("cancel %s: %v", status, err)
		}

		if got != want {
			t.Errorf("cancelling a %s row reported %v, want %v", status, got, want)
		}

		e, err := s.Get(ctx, projID, id)
		if err != nil {
			t.Fatalf("get: %v", err)
		}

		if want && e.Status != emailmodel.StatusCancelled {
			t.Errorf("after cancel a %s row is %s, want cancelled", status, e.Status)
		}

		if !want && e.Status != status {
			t.Errorf("a refused cancel changed a %s row to %s", status, e.Status)
		}
	}

	other := ids.New()
	seedStatus(t, s, ctx, projID, other, emailmodel.StatusQueued, at)
	if ok, err := s.Cancel(ctx, ids.New(), other, at); err != nil || ok {
		t.Errorf("cancelling through another project reported %v, %v", ok, err)
	}

	claimed, err := s.ClaimDue(ctx, time.Now().UTC().Add(time.Hour), 10)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}

	for _, e := range claimed {
		if e.Status == emailmodel.StatusCancelled || e.ID != other {
			t.Errorf("the claim took %s, which is %s", e.ID, e.Status)
		}
	}
}
