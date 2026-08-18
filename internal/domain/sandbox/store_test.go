// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package sandbox

import (
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	sbmodel "github.com/yousysadmin/mailyard/internal/models/sandbox"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)

	return NewStore(db)
}

// project_id is a real foreign key, which is the tenancy guarantee, so
// the fixture makes a real project rather than working around it.
func newProject(t *testing.T, s *Store) string {
	t.Helper()
	id := ids.New()
	if _, err := s.Exec(t.Context(), `
		INSERT INTO projects (id, name, slug, owner_id, created_at, updated_at)
		VALUES (?, 'test', ?, NULL, now(), now())`, id, id); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	return id
}

func put(t *testing.T, s *Store, projID string, at time.Time, subject string, expires *time.Time) *sbmodel.Email {
	t.Helper()
	e := &sbmodel.Email{
		ID:         ids.New(),
		ProjectID:  projID,
		Source:     sbmodel.SourceSubmission,
		Sender:     "dev@localhost",
		Recipients: []string{"someone@example.com"},
		Subject:    subject,
		Raw:        []byte("Subject: " + subject + "\r\n\r\nbody\r\n"),
		ExpiresAt:  expires,
		ReceivedAt: at,
		CreatedAt:  at,
	}
	e.Size = int64(len(e.Raw))
	if err := s.Put(t.Context(), e); err != nil {
		t.Fatalf("put: %v", err)
	}

	return e
}

// The raw bytes are the record. A parsed view cannot be turned back
// into the message that produced it, so a round trip that loses them
// loses the reason the sandbox exists.
func TestAMessageSurvivesARoundTrip(t *testing.T) {
	s := testStore(t)
	proj := newProject(t, s)
	now := time.Now().UTC().Truncate(time.Second)
	expiry := now.Add(72 * time.Hour)

	orig := put(t, s, proj, now, "hello", &expiry)

	got, err := s.Get(t.Context(), proj, orig.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if got == nil {
		t.Fatal("the message was not found")
	}

	if got.Subject != "hello" || got.Sender != "dev@localhost" {
		t.Errorf("read back %+v", got)
	}

	if len(got.Recipients) != 1 || got.Recipients[0] != "someone@example.com" {
		t.Errorf("recipients came back as %v", got.Recipients)
	}

	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(expiry) {
		t.Errorf("expiry came back as %v, want %v", got.ExpiresAt, expiry)
	}

	// Get deliberately does not read raw - that is a separate call, so
	// a page of fifty messages does not drag fifty MIME trees along.
	if got.Raw != nil {
		t.Error("Get returned raw bytes, which the list query cannot afford")
	}

	raw, err := s.Raw(t.Context(), proj, orig.ID)
	if err != nil {
		t.Fatalf("raw: %v", err)
	}

	if string(raw) != string(orig.Raw) {
		t.Errorf("raw bytes came back as %q", raw)
	}
}

// Every store query scopes on project first, and a cross-project read
// has to look like a missing resource rather than a refusal.
func TestAnotherProjectSeesNothing(t *testing.T) {
	s := testStore(t)
	mine, theirs := newProject(t, s), newProject(t, s)
	e := put(t, s, mine, time.Now().UTC(), "private", nil)

	got, err := s.Get(t.Context(), theirs, e.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if got != nil {
		t.Error("a message was readable from another project")
	}

	raw, err := s.Raw(t.Context(), theirs, e.ID)
	if err != nil {
		t.Fatalf("raw: %v", err)
	}

	if raw != nil {
		t.Error("raw bytes were readable from another project")
	}

	list, err := s.List(t.Context(), theirs, 50, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if len(list) != 0 {
		t.Errorf("another project listed %d messages", len(list))
	}
}

// Trim is what actually bounds the table. A day window does nothing
// about a CI job writing ten thousand messages in a morning.
func TestTrimKeepsTheNewest(t *testing.T) {
	s := testStore(t)
	proj := newProject(t, s)
	base := time.Now().UTC().Add(-time.Hour)

	var idss []string
	for i := range 5 {
		e := put(t, s, proj, base.Add(time.Duration(i)*time.Minute), "m", nil)
		idss = append(idss, e.ID)
	}

	n, err := s.Trim(t.Context(), proj, 2)
	if err != nil {
		t.Fatalf("trim: %v", err)
	}

	if n != 3 {
		t.Errorf("trim removed %d, want 3", n)
	}

	// The two newest are ids[3] and ids[4].
	for _, gone := range idss[:3] {
		if got, _ := s.Get(t.Context(), proj, gone); got != nil {
			t.Errorf("%s survived the trim", gone)
		}
	}

	for _, kept := range idss[3:] {
		if got, _ := s.Get(t.Context(), proj, kept); got == nil {
			t.Errorf("%s was trimmed but is one of the newest", kept)
		}
	}
}

// A cap of zero means unlimited, not "delete everything". Getting
// this backwards would empty every sandbox on a default install.
func TestTrimWithNoCapKeepsEverything(t *testing.T) {
	s := testStore(t)
	proj := newProject(t, s)
	put(t, s, proj, time.Now().UTC(), "keep me", nil)

	n, err := s.Trim(t.Context(), proj, 0)
	if err != nil {
		t.Fatalf("trim: %v", err)
	}

	if n != 0 {
		t.Errorf("an unlimited cap removed %d messages", n)
	}
}

// Trim is per project. One busy tenant must not evict another's mail.
func TestTrimDoesNotReachIntoAnotherProject(t *testing.T) {
	s := testStore(t)
	busy, quiet := newProject(t, s), newProject(t, s)
	base := time.Now().UTC().Add(-time.Hour)
	for i := range 4 {
		put(t, s, busy, base.Add(time.Duration(i)*time.Minute), "noise", nil)
	}

	kept := put(t, s, quiet, base, "important", nil)

	if _, err := s.Trim(t.Context(), busy, 1); err != nil {
		t.Fatalf("trim: %v", err)
	}

	if got, _ := s.Get(t.Context(), quiet, kept.ID); got == nil {
		t.Error("trimming one project removed another project's message")
	}
}

// A message with no expiry is kept until the cap or a hand delete
// removes it. Purging those would make retention_days = 0 mean the
// opposite of what it says everywhere else in the product.
func TestPurgeExpiredLeavesMessagesWithNoExpiry(t *testing.T) {
	s := testStore(t)
	proj := newProject(t, s)
	now := time.Now().UTC()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	forever := put(t, s, proj, now, "forever", nil)
	stale := put(t, s, proj, now, "stale", &past)
	fresh := put(t, s, proj, now, "fresh", &future)

	n, err := s.PurgeExpired(t.Context(), now)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}

	if n != 1 {
		t.Errorf("purged %d messages, want 1", n)
	}

	if got, _ := s.Get(t.Context(), proj, stale.ID); got != nil {
		t.Error("an expired message survived")
	}

	for _, id := range []string{forever.ID, fresh.ID} {
		if got, _ := s.Get(t.Context(), proj, id); got == nil {
			t.Errorf("%s was purged and should not have been", id)
		}
	}
}

func TestClearEmptiesOneProject(t *testing.T) {
	s := testStore(t)
	mine, theirs := newProject(t, s), newProject(t, s)
	put(t, s, mine, time.Now().UTC(), "a", nil)
	put(t, s, mine, time.Now().UTC(), "b", nil)
	kept := put(t, s, theirs, time.Now().UTC(), "c", nil)

	n, err := s.Clear(t.Context(), mine)
	if err != nil {
		t.Fatalf("clear: %v", err)
	}

	if n != 2 {
		t.Errorf("cleared %d, want 2", n)
	}

	if got, _ := s.Get(t.Context(), theirs, kept.ID); got == nil {
		t.Error("clearing one project emptied another")
	}
}
