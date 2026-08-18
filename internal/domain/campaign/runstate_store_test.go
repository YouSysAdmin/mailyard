// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package campaign

import (
	"context"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	cmodel "github.com/yousysadmin/mailyard/internal/models/campaign"
)

// seedCampaign puts one campaign in the given state and returns it.
func seedCampaign(t *testing.T, s *Store, ctx context.Context, status string) *cmodel.Campaign {
	t.Helper()

	proj := ids.New()
	if _, err := s.DB().ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, owner_id, created_at)
		VALUES ($1, 'acme', $2, NULL, now())`, proj, proj); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	c := &cmodel.Campaign{
		ID:         ids.New(),
		ProjectID:  proj,
		Name:       "Launch",
		Subject:    "Launch",
		FromEmail:  "a@b.test",
		Status:     status,
		TemplateID: ids.New(),
		ListID:     ids.New(),
	}
	if err := s.Put(ctx, c); err != nil {
		t.Fatalf("put campaign: %v", err)
	}

	return c
}

// A batch finishing must not undo a pause or a cancel.
//
// This is the race as it happened: the operator presses Pause while a
// batch is in flight, TransitionStatus writes `paused`, the batch then
// finishes and the runner writes its own run state. That write was
// `WHERE id = ?` with no status guard, so it put the campaign back to
// `sending` and delivery continued - the button appeared to do nothing.
func TestABatchEndingDoesNotUndoAPause(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := context.Background()

	c := seedCampaign(t, s, ctx, cmodel.StatusSending)

	// The operator, mid-batch.
	paused, err := s.TransitionStatus(ctx, c.ProjectID, c.ID, cmodel.StatusPaused, cmodel.StatusSending)
	if err != nil || !paused {
		t.Fatalf("pause: ok=%v err=%v", paused, err)
	}

	// The batch, finishing afterwards.
	moved, err := s.SetRunState(ctx, c.ID, cmodel.StatusSending, nil, nil, nil, cmodel.StatusSending)
	if err != nil {
		t.Fatalf("set run state: %v", err)
	}

	if moved {
		t.Error("the runner reported it moved a paused campaign, so the guard did not hold")
	}

	status, err := s.Status(ctx, c.ProjectID, c.ID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	if status != cmodel.StatusPaused {
		t.Errorf("status %q, want paused - the batch put it back to sending", status)
	}
}

// A cancelled campaign must not be reported as sent.
//
// Cancel skips the unsent remainder, which makes CountPending answer
// zero, which brings the runner straight to complete(). Unguarded, that
// stamped `sent` and completed_at over the cancellation and emitted
// campaign.completed to every webhook consumer.
func TestACancelledCampaignIsNotCompleted(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := context.Background()

	c := seedCampaign(t, s, ctx, cmodel.StatusSending)

	cancelled, err := s.TransitionStatus(ctx, c.ProjectID, c.ID, cmodel.StatusCancelled,
		cmodel.StatusScheduled, cmodel.StatusSending, cmodel.StatusPaused)
	if err != nil || !cancelled {
		t.Fatalf("cancel: ok=%v err=%v", cancelled, err)
	}

	moved, err := s.SetRunState(ctx, c.ID, cmodel.StatusSent, nil, nil, nil, cmodel.StatusSending)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}

	if moved {
		t.Error("complete() moved a cancelled campaign, so it would also have emitted campaign.completed")
	}

	status, err := s.Status(ctx, c.ProjectID, c.ID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	if status != cmodel.StatusCancelled {
		t.Errorf("status %q, want cancelled", status)
	}
}

// The ordinary path still works, or the guard would just have broken
// campaigns instead of fixing them.
func TestASendingCampaignStillAdvances(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := context.Background()

	c := seedCampaign(t, s, ctx, cmodel.StatusSending)

	moved, err := s.SetRunState(ctx, c.ID, cmodel.StatusSent, nil, nil, nil, cmodel.StatusSending)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}

	if !moved {
		t.Fatal("a sending campaign refused to complete")
	}

	status, err := s.Status(ctx, c.ProjectID, c.ID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	if status != cmodel.StatusSent {
		t.Errorf("status %q, want sent", status)
	}
}

// An empty from list is an error rather than an unguarded UPDATE, so
// nobody reintroduces the bug by omitting the argument.
func TestSetRunStateRefusesWithNoGuard(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := context.Background()

	c := seedCampaign(t, s, ctx, cmodel.StatusSending)

	if _, err := s.SetRunState(ctx, c.ID, cmodel.StatusSent, nil, nil, nil); err == nil {
		t.Error("an unguarded SetRunState was accepted")
	}
}

// Status answers empty for a campaign that is gone, rather than
// erroring - the delivery loop asks about a row that a concurrent
// delete may have taken.
func TestStatusOfAMissingCampaignIsEmpty(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}

	status, err := s.Status(context.Background(), ids.New(), ids.New())
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	if status != "" {
		t.Errorf("status %q, want empty", status)
	}
}
