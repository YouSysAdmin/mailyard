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

// Marking a message failed or skipped carries no email id, and that is
// the path that broke.
//
// The empty path, taken deliberately - the rule tests/nulluuid_test.go
// settled on, because nothing static reaches this. email_id is a uuid
// column, the parameter feeding it is typed uuid, and a parameter is
// bound before the CASE picks a branch, so an empty string was refused
// whichever way the CASE would have gone. The statement PREPAREs, which is exactly why
// TestEveryQueryMatchesTheSchema cannot see it: that guard prepares and
// never binds.
//
// What it cost: the row stayed pending, CountPending never reached zero,
// complete() never ran, and the campaign redelivered the same failing
// message every batch forever. One recipient unsubscribing between
// fan-out and delivery was enough to start it.
func TestMarkingAMessageWithNoEmailIDSucceeds(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := context.Background()

	c := seedCampaign(t, s, ctx, cmodel.StatusSending)
	msg := &cmodel.Message{
		ID: ids.New(), CampaignID: c.ID, SubscriberID: ids.New(),
		Status: cmodel.MsgPending,
	}
	if err := s.BulkCreateMessages(ctx, []*cmodel.Message{msg}); err != nil {
		t.Fatalf("seed message: %v", err)
	}

	// The two calls the runner actually makes with no id.
	for _, tc := range []struct {
		status string
		reason string
	}{
		{cmodel.MsgSkipped, "recipient is suppressed"},
		{cmodel.MsgFailed, "no route for this send"},
	} {
		if err := s.UpdateMessage(ctx, msg.ID, tc.status, tc.reason, ""); err != nil {
			t.Fatalf("marking %s with no email id: %v", tc.status, err)
		}

		var status string
		if err := db.QueryRowContext(ctx,
			`SELECT status FROM campaign_messages WHERE id = $1`, msg.ID).Scan(&status); err != nil {
			t.Fatalf("read back: %v", err)
		}

		if status != tc.status {
			t.Fatalf("status = %q, want %q - the row stayed put, so the campaign "+
				"never finishes and redelivers this message every batch", status, tc.status)
		}
	}

	// The whole point of the campaign being able to finish.
	if n, err := s.CountPending(ctx, c.ID); err != nil || n != 0 {
		t.Errorf("pending = %d err = %v, want 0", n, err)
	}
}

// An id already recorded is not cleared by a later update that names
// none, which is what the CASE is for.
func TestAnEmptyEmailIDLeavesARecordedOneAlone(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := context.Background()

	c := seedCampaign(t, s, ctx, cmodel.StatusSending)
	msg := &cmodel.Message{
		ID: ids.New(), CampaignID: c.ID, SubscriberID: ids.New(),
		Status: cmodel.MsgPending,
	}
	if err := s.BulkCreateMessages(ctx, []*cmodel.Message{msg}); err != nil {
		t.Fatalf("seed message: %v", err)
	}

	emailID := ids.New()
	if err := s.UpdateMessage(ctx, msg.ID, cmodel.MsgSent, "", emailID); err != nil {
		t.Fatalf("record the send: %v", err)
	}

	// A later update with no id - a bounce turning it into a failure.
	if err := s.UpdateMessage(ctx, msg.ID, cmodel.MsgFailed, "bounced", ""); err != nil {
		t.Fatalf("later update: %v", err)
	}

	var got string
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(email_id::text, '') FROM campaign_messages WHERE id = $1`,
		msg.ID).Scan(&got); err != nil {
		t.Fatalf("read back: %v", err)
	}

	if got != emailID {
		t.Errorf("email_id = %q, want it kept (%s) - the message it names is the "+
			"only link from this row to the delivery log", got, emailID)
	}
}
