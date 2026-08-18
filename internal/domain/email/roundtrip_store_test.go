// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
)

// Put then Get, with every field set to something distinctive.
//
// This exists because the emails table is written by a 28-column
// INSERT, read by a 32-column SELECT and scanned positionally, and
// those three lists are kept in step by hand. Adding a column has
// silently broken them three times: a missing placeholder, a
// placeholder with no argument, and a scan destination in the wrong
// position. The first two surface as a 500 on the send path, the
// third as a value quietly landing in the wrong field - which no
// error would ever report.
//
// A round trip catches all three at once, so the next column only has
// to survive this test rather than somebody's attention.
func TestEmailSurvivesARoundTrip(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	dbtest.Schema(t, db, `
        INSERT INTO projects (id, name, slug, default_language, created_at)
        VALUES ('e66e7a4d-9e6c-4884-869a-cf9ffcf22181', 'Test', 'test', 'en', now())`)
	s := &Store{Base: database.NewBase(db)}

	scheduled := time.Now().UTC().Add(time.Hour).Truncate(time.Millisecond)
	next := time.Now().UTC().Add(time.Minute).Truncate(time.Millisecond)
	want := &emailmodel.Email{
		ID:                    "9e2f6f11-cdd3-4058-86f2-29f3ad60b06a",
		ProjectID:             "e66e7a4d-9e6c-4884-869a-cf9ffcf22181",
		CreatedBy:             "user-1",
		APIKeyID:              ids.New(),
		SMTPServerID:          ids.New(),
		SMTPGroupID:           ids.New(),
		Sender:                "from@example.com",
		Recipients:            []string{"a@example.com", "b@example.com"},
		Subject:               "the subject",
		TemplateName:          "welcome",
		HTMLBody:              "<p>html</p>",
		TextBody:              "text",
		Headers:               map[string]string{"X-Thing": "value"},
		ListUnsubscribeURL:    "https://example.com/u",
		ListUnsubscribeMailto: "mailto:unsub@example.com",
		ListUnsubscribePost:   true,
		UnsubscribeListID:     ids.New(),
		Tracked:               true,
		Status:                emailmodel.StatusScheduled,
		ErrorMessage:          "an error",
		Attempts:              2,
		MaxAttempts:           7,
		NextAttemptAt:         &next,
		ScheduledAt:           &scheduled,
		CreatedAt:             time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := s.Put(t.Context(), want); err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := s.Get(t.Context(), "e66e7a4d-9e6c-4884-869a-cf9ffcf22181", "9e2f6f11-cdd3-4058-86f2-29f3ad60b06a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if got == nil {
		t.Fatal("the row came back nil")
	}

	// Compared field by field rather than with a struct equality, so a
	// failure names the column that drifted.
	checks := []struct {
		field     string
		want, got any
	}{
		{"project_id", want.ProjectID, got.ProjectID},
		{"created_by", want.CreatedBy, got.CreatedBy},
		{"api_key_id", want.APIKeyID, got.APIKeyID},
		{"smtp_server_id", want.SMTPServerID, got.SMTPServerID},
		{"smtp_group_id", want.SMTPGroupID, got.SMTPGroupID},
		{"sender", want.Sender, got.Sender},
		{"subject", want.Subject, got.Subject},
		{"template_name", want.TemplateName, got.TemplateName},
		{"html_body", want.HTMLBody, got.HTMLBody},
		{"text_body", want.TextBody, got.TextBody},
		{"list_unsubscribe_url", want.ListUnsubscribeURL, got.ListUnsubscribeURL},
		{"list_unsubscribe_mailto", want.ListUnsubscribeMailto, got.ListUnsubscribeMailto},
		{"list_unsubscribe_post", want.ListUnsubscribePost, got.ListUnsubscribePost},
		{"unsubscribe_list_id", want.UnsubscribeListID, got.UnsubscribeListID},
		{"tracked", want.Tracked, got.Tracked},
		{"status", want.Status, got.Status},
		{"error_message", want.ErrorMessage, got.ErrorMessage},
		{"attempts", want.Attempts, got.Attempts},
		{"max_attempts", want.MaxAttempts, got.MaxAttempts},
		{"recipient count", len(want.Recipients), len(got.Recipients)},
		{"first recipient", want.Recipients[0], got.Recipients[0]},
		{"header", want.Headers["X-Thing"], got.Headers["X-Thing"]},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s came back %v, want %v", c.field, c.got, c.want)
		}
	}

	if got.NextAttemptAt == nil || !got.NextAttemptAt.Equal(next) {
		t.Errorf("next_attempt_at came back %v, want %v", got.NextAttemptAt, next)
	}

	if got.ScheduledAt == nil || !got.ScheduledAt.Equal(scheduled) {
		t.Errorf("scheduled_at came back %v, want %v", got.ScheduledAt, scheduled)
	}
}

// The tracking tallies are written by their own statements, so they
// get their own round trip.
func TestMarkOpenedAndClickedAccumulate(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	dbtest.Schema(t, db, `
        INSERT INTO projects (id, name, slug, default_language, created_at)
        VALUES ('e66e7a4d-9e6c-4884-869a-cf9ffcf22181', 'Test', 'test', 'en', now())`)
	s := &Store{Base: database.NewBase(db)}
	// Put stamps created_at, and the tracking marks name it: the table is
	// partitioned by that column, so an UPDATE without it opens a node on
	// every live partition. Read it back rather than assume it.
	if err := s.Put(t.Context(), &emailmodel.Email{
		ID: "9e2f6f11-cdd3-4058-86f2-29f3ad60b06a", ProjectID: "e66e7a4d-9e6c-4884-869a-cf9ffcf22181", Sender: "a@b.com",
		Recipients: []string{"c@d.com"}, Subject: "s", Status: emailmodel.StatusSent,
	}); err != nil {
		t.Fatal(err)
	}

	seeded, err := s.Get(t.Context(), "e66e7a4d-9e6c-4884-869a-cf9ffcf22181", "9e2f6f11-cdd3-4058-86f2-29f3ad60b06a")
	if err != nil || seeded == nil {
		t.Fatalf("read back the seeded row: %v", err)
	}

	created := seeded.CreatedAt

	first, opens, err := s.MarkOpened(t.Context(), "9e2f6f11-cdd3-4058-86f2-29f3ad60b06a", created, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}

	if !first {
		t.Error("the first open did not report itself as the first")
	}

	// The running count comes back with it, which is what the tracking
	// handler uses to stop writing event rows for a replayed pixel URL.
	if opens != 1 {
		t.Errorf("open_count returned %d, want 1", opens)
	}

	again, opens, err := s.MarkOpened(t.Context(), "9e2f6f11-cdd3-4058-86f2-29f3ad60b06a", created, time.Now().UTC().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	if again {
		t.Error("a second open reported itself as the first, so unique opens would overcount")
	}

	if opens != 2 {
		t.Errorf("open_count returned %d on the second open, want 2", opens)
	}

	clicks, err := s.MarkClicked(t.Context(), "9e2f6f11-cdd3-4058-86f2-29f3ad60b06a", created, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}

	if clicks != 1 {
		t.Errorf("click_count returned %d, want 1", clicks)
	}

	got, err := s.Get(t.Context(), "e66e7a4d-9e6c-4884-869a-cf9ffcf22181", "9e2f6f11-cdd3-4058-86f2-29f3ad60b06a")
	if err != nil {
		t.Fatal(err)
	}

	if got.OpenCount != 2 {
		t.Errorf("open_count is %d, want 2", got.OpenCount)
	}

	if got.ClickCount != 1 {
		t.Errorf("click_count is %d, want 1", got.ClickCount)
	}

	if got.OpenedAt == nil || got.ClickedAt == nil {
		t.Errorf("opened_at=%v clicked_at=%v, want both set", got.OpenedAt, got.ClickedAt)
	}
}

// A click means somebody read the message even when the pixel never
// fired, which is the ordinary case for a client that blocks images.
func TestClickImpliesOpen(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	dbtest.Schema(t, db, `
        INSERT INTO projects (id, name, slug, default_language, created_at)
        VALUES ('e66e7a4d-9e6c-4884-869a-cf9ffcf22181', 'Test', 'test', 'en', now())`)
	s := &Store{Base: database.NewBase(db)}
	// Put stamps created_at, and the tracking marks name it: the table is
	// partitioned by that column, so an UPDATE without it opens a node on
	// every live partition. Read it back rather than assume it.
	if err := s.Put(t.Context(), &emailmodel.Email{
		ID: "9e2f6f11-cdd3-4058-86f2-29f3ad60b06a", ProjectID: "e66e7a4d-9e6c-4884-869a-cf9ffcf22181", Sender: "a@b.com",
		Recipients: []string{"c@d.com"}, Subject: "s", Status: emailmodel.StatusSent,
	}); err != nil {
		t.Fatal(err)
	}

	seeded, err := s.Get(t.Context(), "e66e7a4d-9e6c-4884-869a-cf9ffcf22181", "9e2f6f11-cdd3-4058-86f2-29f3ad60b06a")
	if err != nil || seeded == nil {
		t.Fatalf("read back the seeded row: %v", err)
	}

	created := seeded.CreatedAt

	if _, err := s.MarkClicked(t.Context(), "9e2f6f11-cdd3-4058-86f2-29f3ad60b06a", created, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(t.Context(), "e66e7a4d-9e6c-4884-869a-cf9ffcf22181", "9e2f6f11-cdd3-4058-86f2-29f3ad60b06a")
	if err != nil {
		t.Fatal(err)
	}

	if got.OpenedAt == nil {
		t.Error("a clicked message is not marked opened, so open rates undercount image blockers")
	}
}
