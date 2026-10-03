// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package campaign

import (
	"context"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	cmodel "github.com/yousysadmin/mailyard/internal/models/campaign"
	whmodel "github.com/yousysadmin/mailyard/internal/models/webhook"
)

// A campaign email cancelled before a worker took it settles its
// campaign message, and the campaign finishes once, as it would had the
// worker delivered it.
func TestACancelledEmailSettlesItsCampaign(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	st := &store.Store{Campaign: s}
	ctx := t.Context()

	c := seedCampaign(t, s, ctx, cmodel.StatusSent)
	emailID := ids.New()
	m := &cmodel.Message{ID: ids.New(), CampaignID: c.ID, SubscriberID: ids.New(), Status: cmodel.MsgPending}
	if err := s.BulkCreateMessages(ctx, []*cmodel.Message{m}); err != nil {
		t.Fatalf("seed message: %v", err)
	}

	if ok, err := s.QueueMessage(ctx, m.ID, emailID); err != nil || !ok {
		t.Fatalf("queue: %v %v", ok, err)
	}

	var events []string
	emit := func(_ context.Context, _, event, _ string, _ any) { events = append(events, event) }
	for range 2 {
		if err := Settle(ctx, st, emit, nil, c.ProjectID, emailID, cmodel.MsgSkipped, "email cancelled"); err != nil {
			t.Fatalf("settle: %v", err)
		}
	}

	got, err := s.GetMessageAny(ctx, m.ID)
	if err != nil || got == nil {
		t.Fatalf("read message: %v", err)
	}

	if got.Status != cmodel.MsgSkipped {
		t.Errorf("message status = %q, want skipped", got.Status)
	}

	if len(events) != 1 || events[0] != whmodel.EventCampaignCompleted {
		t.Errorf("events = %v, want campaign.completed once", events)
	}
}

// The runner links a message to its email only while the message is
// still pending: a cancel that skipped it meanwhile is not overwritten.
func TestQueueingDoesNotOverwriteACancel(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := t.Context()

	c := seedCampaign(t, s, ctx, cmodel.StatusSending)
	m := &cmodel.Message{ID: ids.New(), CampaignID: c.ID, SubscriberID: ids.New(), Status: cmodel.MsgPending}
	if err := s.BulkCreateMessages(ctx, []*cmodel.Message{m}); err != nil {
		t.Fatalf("seed message: %v", err)
	}

	if _, err := s.SkipPending(ctx, c.ID, "campaign cancelled"); err != nil {
		t.Fatalf("skip: %v", err)
	}

	ok, err := s.QueueMessage(ctx, m.ID, ids.New())
	if err != nil {
		t.Fatalf("queue: %v", err)
	}

	if ok {
		t.Error("a skipped message was queued")
	}

	got, _ := s.GetMessageAny(ctx, m.ID)
	if got == nil || got.Status != cmodel.MsgSkipped {
		t.Errorf("message = %+v, want it left skipped", got)
	}
}

// Launch keeps the offset scheduled_at was written with, and a local
// time campaign is promoted once its wall clock arrives at UTC+14.
func TestALocalTimeCampaignKeepsItsWallClock(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := t.Context()

	c := seedCampaign(t, s, ctx, cmodel.StatusDraft)
	c.SendAtLocalTime = true
	if err := s.Put(ctx, c); err != nil {
		t.Fatalf("put: %v", err)
	}

	// 09:00 at +01:00, a day ahead.
	written := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Hour)
	at := written.In(time.FixedZone("", 3600))
	offset := 3600
	if ok, err := s.Launch(ctx, c.ProjectID, c.ID, cmodel.StatusScheduled, &written, &offset, nil, nil); err != nil || !ok {
		t.Fatalf("launch: %v %v", ok, err)
	}

	got, err := s.Get(ctx, c.ProjectID, c.ID)
	if err != nil || got == nil {
		t.Fatalf("get: %v", err)
	}

	if got.ScheduledOffset == nil || *got.ScheduledOffset != offset {
		t.Fatalf("offset = %v, want %d", got.ScheduledOffset, offset)
	}

	// Not yet: the wall clock has not arrived anywhere.
	wallAsUTC := time.Date(at.Year(), at.Month(), at.Day(), at.Hour(), at.Minute(), 0, 0, time.UTC)
	if n, err := s.PromoteScheduled(ctx, wallAsUTC.Add(-14*time.Hour-time.Minute)); err != nil || n != 0 {
		t.Fatalf("promoted %d (%v) before the wall clock reached UTC+14", n, err)
	}

	if n, err := s.PromoteScheduled(ctx, wallAsUTC.Add(-14*time.Hour)); err != nil || n != 1 {
		t.Fatalf("promoted %d (%v), want the campaign once the wall clock reached UTC+14", n, err)
	}
}
