// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package contact

import (
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/database/dbtest"
)

// newTestStore builds just the schema this package touches. The
// UNIQUE constraint is the load-bearing part - RecordOutcome relies
// on it for its upsert.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	db := dbtest.Open(t)
	dbtest.Schema(t, db, `
        CREATE TABLE contacts (
            id             TEXT PRIMARY KEY,
            project_id   TEXT NOT NULL,
            email          TEXT NOT NULL,
            name           TEXT NOT NULL DEFAULT '',
            sent_count     INTEGER NOT NULL DEFAULT 0,
            fail_count     INTEGER NOT NULL DEFAULT 0,
            last_sent_at   TIMESTAMPTZ,
            last_failed_at TIMESTAMPTZ,
            created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
            updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
            UNIQUE (project_id, email)
        )`)

	return NewStore(db)
}

func TestRecordOutcomeCreatesThenAccumulates(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Second)

	for range 3 {
		if err := s.RecordOutcome(ctx, "proj-1", "alice@example.com", "", true, now); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.RecordOutcome(ctx, "proj-1", "alice@example.com", "", false, now); err != nil {
		t.Fatal(err)
	}

	list, err := s.List(ctx, "proj-1", "", 10, 0)
	if err != nil {
		t.Fatal(err)
	}

	if len(list) != 1 {
		t.Fatalf("contacts = %d, want 1 row accumulating four outcomes", len(list))
	}

	c := list[0]
	if c.SentCount != 3 || c.FailCount != 1 {
		t.Errorf("tallies = sent %d fail %d, want 3 and 1", c.SentCount, c.FailCount)
	}

	if c.LastSentAt == nil || c.LastFailedAt == nil {
		t.Error("both timestamps should be set after a send and a failure")
	}
}

func TestRecordOutcomeKeepsAKnownName(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	now := time.Now().UTC()

	if err := s.RecordOutcome(ctx, "proj-1", "alice@example.com", "Alice", true, now); err != nil {
		t.Fatal(err)
	}

	// A later send with a bare address must not erase the name we
	// already learned.
	if err := s.RecordOutcome(ctx, "proj-1", "alice@example.com", "", true, now); err != nil {
		t.Fatal(err)
	}

	list, _ := s.List(ctx, "proj-1", "", 10, 0)
	if list[0].Name != "Alice" {
		t.Errorf("name = %q, want it preserved", list[0].Name)
	}

	// A new name does replace the old one.
	if err := s.RecordOutcome(ctx, "proj-1", "alice@example.com", "Alice Smith", true, now); err != nil {
		t.Fatal(err)
	}

	list, _ = s.List(ctx, "proj-1", "", 10, 0)
	if list[0].Name != "Alice Smith" {
		t.Errorf("name = %q, want the newer one", list[0].Name)
	}
}

func TestAddressesAreNormalized(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	now := time.Now().UTC()

	for _, addr := range []string{"Alice@Example.com", "  alice@example.com  ", "ALICE@EXAMPLE.COM"} {
		if err := s.RecordOutcome(ctx, "proj-1", addr, "", true, now); err != nil {
			t.Fatal(err)
		}
	}

	list, _ := s.List(ctx, "proj-1", "", 10, 0)
	if len(list) != 1 {
		t.Fatalf("contacts = %d, want the three spellings folded into one", len(list))
	}

	if list[0].Email != "alice@example.com" || list[0].SentCount != 3 {
		t.Errorf("got %q with %d sends", list[0].Email, list[0].SentCount)
	}
}

func TestEmptyAddressIsIgnored(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	if err := s.RecordOutcome(ctx, "proj-1", "   ", "", true, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	n, _ := s.Count(ctx, "proj-1", "")
	if n != 0 {
		t.Errorf("count = %d, want no row for a blank address", n)
	}
}

func TestProjectIsolation(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	now := time.Now().UTC()
	_ = s.RecordOutcome(ctx, "proj-1", "alice@example.com", "", true, now)
	_ = s.RecordOutcome(ctx, "proj-2", "alice@example.com", "", true, now)

	for _, proj := range []string{"proj-1", "proj-2"} {
		list, _ := s.List(ctx, proj, "", 10, 0)
		if len(list) != 1 {
			t.Errorf("%s sees %d contacts, want its own only", proj, len(list))
		}
	}

	// The same address in another project must be invisible here.
	other, _ := s.List(ctx, "proj-2", "", 10, 0)
	got, err := s.Get(ctx, "proj-1", other[0].ID)
	if err != nil {
		t.Fatal(err)
	}

	if got != nil {
		t.Error("a foreign project's contact must read as missing")
	}
}

func TestSearchMatchesEmailAndName(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	now := time.Now().UTC()
	_ = s.RecordOutcome(ctx, "proj-1", "alice@example.com", "Alice Smith", true, now)
	_ = s.RecordOutcome(ctx, "proj-1", "bob@other.test", "Bob Jones", true, now)

	cases := map[string]int{
		"alice":       1,
		"ALICE":       1, // case-insensitive
		"example.com": 1,
		"jones":       1,
		"":            2,
		"nobody":      0,
	}
	for term, want := range cases {
		list, err := s.List(ctx, "proj-1", term, 10, 0)
		if err != nil {
			t.Fatal(err)
		}

		if len(list) != want {
			t.Errorf("search %q returned %d, want %d", term, len(list), want)
		}

		n, _ := s.Count(ctx, "proj-1", term)
		if n != want {
			t.Errorf("count %q returned %d, want %d", term, n, want)
		}
	}
}

func TestListOrdersByMostRecentActivity(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	base := time.Now().UTC().Truncate(time.Second)

	_ = s.RecordOutcome(ctx, "proj-1", "old@example.com", "", true, base.Add(-48*time.Hour))
	_ = s.RecordOutcome(ctx, "proj-1", "recent@example.com", "", true, base)

	// Only ever failed - must still sort by that activity, not fall
	// to the bottom for having no send.
	_ = s.RecordOutcome(ctx, "proj-1", "failed@example.com", "", false, base.Add(-time.Hour))

	list, _ := s.List(ctx, "proj-1", "", 10, 0)
	got := []string{list[0].Email, list[1].Email, list[2].Email}
	want := []string{"recent@example.com", "failed@example.com", "old@example.com"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("order = %v, want %v", got, want)
			break
		}
	}
}

func TestPurgeForEmail(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	now := time.Now().UTC()
	_ = s.RecordOutcome(ctx, "proj-1", "alice@example.com", "", true, now)
	_ = s.RecordOutcome(ctx, "proj-2", "alice@example.com", "", true, now)

	n, err := s.PurgeForEmail(ctx, "proj-1", "ALICE@example.com  ")
	if err != nil {
		t.Fatal(err)
	}

	if n != 1 {
		t.Errorf("purged %d, want 1", n)
	}

	if c, _ := s.Count(ctx, "proj-1", ""); c != 0 {
		t.Errorf("proj-1 still has %d contacts", c)
	}

	// The other project's row for the same address survives.
	if c, _ := s.Count(ctx, "proj-2", ""); c != 1 {
		t.Errorf("proj-2 count = %d, want its row untouched", c)
	}
}
