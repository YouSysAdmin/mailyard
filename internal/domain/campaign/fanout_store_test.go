// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package campaign

import (
	"database/sql"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	cmodel "github.com/yousysadmin/mailyard/internal/models/campaign"
)

// A fan-out past one chunk lands whole: every row, the ids it was given
// or minted, deliver_at as set or NULL, and a repeated subscriber kept
// once.
func TestAFanOutPastOneChunkLandsWhole(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := t.Context()

	c := seedCampaign(t, s, ctx, cmodel.StatusSending)
	at := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	n := fanOutChunk + 3
	msgs := make([]*cmodel.Message, n)
	for i := range n {
		m := &cmodel.Message{CampaignID: c.ID, SubscriberID: ids.New(), Status: cmodel.MsgPending}
		if i%2 == 0 {
			m.ID = ids.New()
		}

		if i == n-1 {
			m.DeliverAt = &at
		}

		msgs[i] = m
	}

	if err := s.BulkCreateMessages(ctx, msgs); err != nil {
		t.Fatalf("fan out: %v", err)
	}

	for _, m := range msgs {
		if m.ID == "" {
			t.Fatal("a message without an id was not given one")
		}
	}

	// The same subscribers again, plus one new one: only the new one lands.
	again := []*cmodel.Message{
		{CampaignID: c.ID, SubscriberID: msgs[0].SubscriberID, Status: cmodel.MsgPending},
		{CampaignID: c.ID, SubscriberID: msgs[fanOutChunk].SubscriberID, Status: cmodel.MsgPending},
		{CampaignID: c.ID, SubscriberID: ids.New(), Status: cmodel.MsgSkipped, ErrorMessage: "opted out"},
	}
	if err := s.BulkCreateMessages(ctx, again); err != nil {
		t.Fatalf("fan out again: %v", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM campaign_messages WHERE campaign_id = $1`, c.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}

	if count != n+1 {
		t.Fatalf("%d rows, expected %d", count, n+1)
	}

	var deliverAt sql.NullTime
	read := `SELECT deliver_at FROM campaign_messages WHERE id = $1`
	if err := db.QueryRowContext(ctx, read, msgs[n-1].ID).Scan(&deliverAt); err != nil {
		t.Fatal(err)
	}

	if !deliverAt.Valid || !deliverAt.Time.Equal(at) {
		t.Fatalf("deliver_at %v, expected %v", deliverAt, at)
	}

	if err := db.QueryRowContext(ctx, read, msgs[0].ID).Scan(&deliverAt); err != nil {
		t.Fatal(err)
	}

	if deliverAt.Valid {
		t.Fatalf("deliver_at %v, expected NULL", deliverAt.Time)
	}

	var reason string
	if err := db.QueryRowContext(ctx, `SELECT error_message FROM campaign_messages WHERE subscriber_id = $1`,
		again[2].SubscriberID).Scan(&reason); err != nil {
		t.Fatal(err)
	}

	if reason != "opted out" {
		t.Fatalf("error_message %q, expected the one given", reason)
	}
}
