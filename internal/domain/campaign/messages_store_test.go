// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package campaign

import (
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/core/keyset"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	cmodel "github.com/yousysadmin/mailyard/internal/models/campaign"
)

// Paging a campaign's messages visits every row exactly once, in fan-out
// order, even when rows share a created_at, and a status filter pages
// the matching rows rather than filtering one page.
func TestCampaignMessagesPageByCursor(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := t.Context()

	c := seedCampaign(t, s, ctx, cmodel.StatusSending)
	var msgs []*cmodel.Message
	for i := range 5 {
		status := cmodel.MsgSent
		if i%2 == 1 {
			status = cmodel.MsgFailed
		}

		msgs = append(msgs, &cmodel.Message{
			ID: ids.New(), CampaignID: c.ID, SubscriberID: ids.New(), Status: status,
		})
	}

	// One insert, so the rows share a created_at and only the id orders them.
	if err := s.BulkCreateMessages(ctx, msgs); err != nil {
		t.Fatalf("seed messages: %v", err)
	}

	collect := func(status string) []string {
		var seen []string
		var cursor keyset.Cursor
		for range 10 {
			rows, err := s.ListMessages(ctx, c.ProjectID, c.ID,
				store.CampaignMessageFilter{Status: status, Limit: 3, Cursor: cursor})
			if err != nil {
				t.Fatalf("list: %v", err)
			}

			page, more := keyset.Cut(rows, 2)
			for _, m := range page {
				seen = append(seen, m.ID)
			}

			if !more {
				return seen
			}

			last := page[len(page)-1]
			cursor = keyset.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
		}

		t.Fatal("paging did not end")

		return nil
	}

	all := collect("")
	if len(all) != 5 {
		t.Fatalf("paged %d messages, want 5", len(all))
	}

	unique := map[string]bool{}
	for _, id := range all {
		unique[id] = true
	}

	if len(unique) != 5 {
		t.Errorf("a message was listed twice: %v", all)
	}

	if failed := collect(cmodel.MsgFailed); len(failed) != 2 {
		t.Errorf("paged %d failed messages, want 2", len(failed))
	}

	if other, err := s.ListMessages(ctx, ids.New(), c.ID, store.CampaignMessageFilter{Limit: 10}); err != nil || len(other) != 0 {
		t.Errorf("another project listed %d messages, err %v", len(other), err)
	}
}
