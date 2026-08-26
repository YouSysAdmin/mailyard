// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package campaign

import (
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	cmodel "github.com/yousysadmin/mailyard/internal/models/campaign"
)

// TestTrackingReadsRunAgainstTheRealSchema exercises every tracking
// query the campaign analytics page issues.
//
// One of them joined `tracking_events.message_id`, a column that has
// never existed - it is `campaign_message_id`, which the INSERT thirty
// lines below it always got right. Nothing failed until somebody
// opened a campaign that existed: no test called it, and a query is
// only checked by Postgres when it runs. The symptom was a 500 on the
// campaign detail page.
//
// So the point here is COVERAGE of the read path, not the numbers. A
// query naming a column that is not there fails on the first call
// whatever the data is.
func TestTrackingReadsRunAgainstTheRealSchema(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := t.Context()

	proj := ids.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, owner_id, created_at)
		VALUES ($1, 'acme', $2, NULL, now())`, proj, proj); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	camp := &cmodel.Campaign{
		ID:        ids.New(),
		ProjectID: proj,
		Name:      "Launch",
		Subject:   "Launch",
		FromEmail: "a@b.test",
		Status:    cmodel.StatusDraft,
		// NOT NULL in the schema, so the fixture has to carry them.
		TemplateID: ids.New(),
		ListID:     ids.New(),
	}
	if err := s.Put(ctx, camp); err != nil {
		t.Fatalf("put campaign: %v", err)
	}

	msg := &cmodel.Message{
		ID:           ids.New(),
		CampaignID:   camp.ID,
		SubscriberID: ids.New(),
		Status:       "sent",
		CreatedAt:    time.Now().UTC(),
	}
	if err := s.BulkCreateMessages(ctx, []*cmodel.Message{msg}); err != nil {
		t.Fatalf("create message: %v", err)
	}

	link := &cmodel.TrackedLink{
		ProjectID:   proj,
		CampaignID:  camp.ID,
		OriginalURL: "https://example.invalid/x",
		Hash:        "abc123",
	}
	if err := s.UpsertTrackedLink(ctx, link); err != nil {
		t.Fatalf("upsert link: %v", err)
	}

	for _, kind := range []string{"open", "click"} {
		if err := s.InsertTrackingEvent(ctx, &cmodel.TrackingEvent{
			EmailID:           ids.New(),
			CampaignMessageID: msg.ID,
			EventType:         kind,
			CreatedAt:         time.Now().UTC(),
		}); err != nil {
			t.Fatalf("insert %s event: %v", kind, err)
		}
	}

	if _, err := s.MarkOpened(ctx, msg.ID, time.Now().UTC()); err != nil {
		t.Fatalf("mark opened: %v", err)
	}

	if err := s.MarkClicked(ctx, msg.ID, time.Now().UTC()); err != nil {
		t.Fatalf("mark clicked: %v", err)
	}

	// The reads the analytics endpoint makes, in the order it makes
	// them. Each one is a separate hand-written query over a schema no
	// compiler checks.
	if _, err := s.ListTrackedLinks(ctx, camp.ID); err != nil {
		t.Errorf("ListTrackedLinks: %v", err)
	}

	for _, kind := range []string{"open", "click"} {
		series, err := s.EventSeries(ctx, camp.ID, kind)
		if err != nil {
			t.Errorf("EventSeries(%s): %v", kind, err)
			continue
		}

		if len(series) != 1 || series[0].Count != 1 {
			t.Errorf("EventSeries(%s) = %+v, want one day with one event", kind, series)
		}
	}

	opened, clicked, err := s.EngagementStats(ctx, camp.ID)
	if err != nil {
		t.Errorf("EngagementStats: %v", err)
	} else if opened != 1 || clicked != 1 {
		t.Errorf("EngagementStats = opened %d, clicked %d, want 1 and 1", opened, clicked)
	}

	if _, err := s.GetMessageByEmail(ctx, ids.New()); err != nil {
		t.Errorf("GetMessageByEmail: %v", err)
	}

	if _, err := s.PurgeTrackingEventsOlderThan(ctx, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Errorf("PurgeTrackingEventsOlderThan: %v", err)
	}
}

// A TRANSACTIONAL tracked link has no campaign, so campaign_id is
// NULL - and "" is not a uuid.
//
// Both halves were passing the empty string straight through, so
// Postgres refused the statement outright and every project with
// click tracking on logged an error per rewritten link, then answered
// 404 on the click. The unique key also had to learn NULLS NOT
// DISTINCT, or the upsert could never fire and each send inserted
// another row for the same URL, splitting the tally.
//
// The empty scope is the whole point of this test: with a campaign id
// present, all three worked.
func TestATransactionalTrackedLinkHasNoCampaign(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := t.Context()

	proj := ids.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, owner_id, created_at)
		VALUES ($1, 'acme', $2, NULL, now())`, proj, proj); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	link := &cmodel.TrackedLink{
		ID: ids.New(), ProjectID: proj, CampaignID: "",
		OriginalURL: "https://example.org/a", Hash: "deadbeef",
	}
	if err := s.UpsertTrackedLink(ctx, link); err != nil {
		t.Fatalf("UpsertTrackedLink with no campaign: %v", err)
	}

	got, err := s.GetTrackedLink(ctx, proj, "", "deadbeef")
	if err != nil {
		t.Fatalf("GetTrackedLink with no campaign: %v", err)
	}

	if got == nil {
		t.Fatal("the link was written and cannot be read back")
	}

	if got.CampaignID != "" {
		t.Errorf("CampaignID = %q, want empty", got.CampaignID)
	}

	if got.OriginalURL != link.OriginalURL {
		t.Errorf("OriginalURL = %q", got.OriginalURL)
	}

	// The second send of the same URL must land on the same row.
	again := &cmodel.TrackedLink{
		ID: ids.New(), ProjectID: proj, CampaignID: "",
		OriginalURL: "https://example.org/a", Hash: "deadbeef",
	}
	if err := s.UpsertTrackedLink(ctx, again); err != nil {
		t.Fatalf("second UpsertTrackedLink: %v", err)
	}

	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tracked_links WHERE project_id = $1 AND hash = 'deadbeef'`,
		proj).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}

	if n != 1 {
		t.Errorf("%d rows for one URL - the upsert did not fire, so the click tally splits", n)
	}
}
