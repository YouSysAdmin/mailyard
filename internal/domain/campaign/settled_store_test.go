// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package campaign

import (
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	cmodel "github.com/yousysadmin/mailyard/internal/models/campaign"
)

// A campaign is claimed as settled once it is sent and no message is
// still in flight, by one caller only, and only in its own project.
func TestACampaignSettlesWhenItsLastMessageDoes(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := t.Context()

	c := seedCampaign(t, s, ctx, cmodel.StatusSent)
	emailA, emailB := ids.New(), ids.New()
	msgs := []*cmodel.Message{
		{ID: ids.New(), CampaignID: c.ID, SubscriberID: ids.New(), Status: cmodel.MsgPending},
		{ID: ids.New(), CampaignID: c.ID, SubscriberID: ids.New(), Status: cmodel.MsgPending},
	}
	if err := s.BulkCreateMessages(ctx, msgs); err != nil {
		t.Fatalf("seed messages: %v", err)
	}

	// Handed to the email queue, as the runner does.
	for i, emailID := range []string{emailA, emailB} {
		if err := s.UpdateMessage(ctx, msgs[i].ID, cmodel.MsgQueued, "", emailID); err != nil {
			t.Fatalf("queue message: %v", err)
		}
	}

	got, err := s.MarkMessageByEmail(ctx, emailA, cmodel.MsgSent, "")
	if err != nil || got != c.ID {
		t.Fatalf("MarkMessageByEmail = %q, %v, want the campaign id", got, err)
	}

	if won, err := s.ClaimSettled(ctx, c.ProjectID, c.ID); err != nil || won {
		t.Fatalf("one still queued: claimed %v err %v, want false", won, err)
	}

	if _, err := s.MarkMessageByEmail(ctx, emailB, cmodel.MsgFailed, "550"); err != nil {
		t.Fatalf("mark second: %v", err)
	}

	if won, _ := s.ClaimSettled(ctx, ids.New(), c.ID); won {
		t.Error("another project claimed the campaign")
	}

	if won, err := s.ClaimSettled(ctx, c.ProjectID, c.ID); err != nil || !won {
		t.Fatalf("all settled: claimed %v err %v, want true", won, err)
	}

	if won, _ := s.ClaimSettled(ctx, c.ProjectID, c.ID); won {
		t.Error("a second caller claimed it too, which emits campaign.completed twice")
	}

	if got, err := s.MarkMessageByEmail(ctx, ids.New(), cmodel.MsgSent, ""); err != nil || got != "" {
		t.Errorf("transactional email: %q, %v, want empty", got, err)
	}
}
