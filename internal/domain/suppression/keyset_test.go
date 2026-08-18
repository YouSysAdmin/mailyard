// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package suppression

import (
	"fmt"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/keyset"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	supmodel "github.com/yousysadmin/mailyard/internal/models/suppression"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)

	return NewStore(db)
}

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

func add(t *testing.T, s *Store, projID, email, kind string, at time.Time) *supmodel.Suppression {
	t.Helper()
	sup := &supmodel.Suppression{
		ID: ids.New(), ProjectID: projID, Email: email,
		Kind: kind, CreatedAt: at,
	}
	if err := s.Upsert(t.Context(), sup); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	return sup
}

// walk pages the whole list through the cursor and returns every
// address it saw, in order.
func walk(t *testing.T, s *Store, projID string, f store.SuppressionFilter, pageSize int) []string {
	t.Helper()
	var seen []string
	var cur keyset.Cursor
	for range 100 { // a loop bound, so a broken cursor fails rather than hangs
		f.Limit = pageSize + 1
		f.Cursor = cur
		rows, err := s.List(t.Context(), projID, f)
		if err != nil {
			t.Fatalf("list: %v", err)
		}

		page, more := keyset.Cut(rows, pageSize)
		for _, r := range page {
			seen = append(seen, r.Email)
		}

		if !more || len(page) == 0 {
			return seen
		}

		last := page[len(page)-1]
		cur = keyset.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}

	t.Fatal("paging did not terminate - the cursor is not advancing")

	return nil
}

// The whole point: walking the pages sees every row exactly once.
func TestPagingSeesEveryRowExactlyOnce(t *testing.T) {
	s := testStore(t)
	proj := newProject(t, s)
	base := time.Now().UTC().Add(-time.Hour)

	want := make([]string, 0, 25)
	for i := range 25 {
		email := fmt.Sprintf("user%02d@example.com", i)
		add(t, s, proj, email, supmodel.KindBounce, base.Add(time.Duration(i)*time.Second))
		want = append(want, email)
	}

	// Newest first.
	for i, j := 0, len(want)-1; i < j; i, j = i+1, j-1 {
		want[i], want[j] = want[j], want[i]
	}

	got := walk(t, s, proj, store.SuppressionFilter{}, 7)
	if len(got) != len(want) {
		t.Fatalf("paging returned %d rows, want %d: %v", len(got), len(want), got)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d is %q, want %q", i, got[i], want[i])
		}
	}
}

// The reason the cursor carries an id as well as a timestamp.
//
// Rows written in the same instant tie on created_at. With a
// timestamp-only cursor, a page boundary landing inside a tied group
// either re-reads the whole group forever or skips the rest of it.
// Bounces arrive exactly this way - one provider rejecting a batch
// writes them all at once.
func TestRowsSharingATimestampAreNotLostOrRepeated(t *testing.T) {
	s := testStore(t)
	proj := newProject(t, s)

	// Every row at the same instant, so created_at orders nothing.
	at := time.Now().UTC().Add(-time.Hour)

	for i := range 10 {
		add(t, s, proj, fmt.Sprintf("tied%02d@example.com", i), supmodel.KindBounce, at)
	}

	got := walk(t, s, proj, store.SuppressionFilter{}, 3)
	if len(got) != 10 {
		t.Fatalf("paging over tied timestamps returned %d rows, want 10: %v", len(got), got)
	}

	seen := map[string]bool{}
	for _, e := range got {
		if seen[e] {
			t.Errorf("%s was returned twice", e)
		}

		seen[e] = true
	}
}

// Search is what the list is actually for. Prefix-anchored, so the
// index on (project_id, email) can serve it.
func TestSearchFindsOneAddress(t *testing.T) {
	s := testStore(t)
	proj := newProject(t, s)
	now := time.Now().UTC()
	for i := range 30 {
		add(t, s, proj, fmt.Sprintf("noise%02d@example.com", i), supmodel.KindBounce, now.Add(-time.Duration(i)*time.Minute))
	}

	add(t, s, proj, "ada@lovelace.example", supmodel.KindBounce, now.Add(-time.Hour))

	rows, err := s.List(t.Context(), proj, store.SuppressionFilter{Search: "ada@", Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if len(rows) != 1 || rows[0].Email != "ada@lovelace.example" {
		t.Fatalf("search returned %+v", rows)
	}
}

// A search term is data, not a pattern. Without escaping, "%" matches
// every address - which on this list would look like the whole
// project is suppressed.
func TestSearchTreatsWildcardsAsLiteralText(t *testing.T) {
	s := testStore(t)
	proj := newProject(t, s)
	now := time.Now().UTC()
	add(t, s, proj, "someone@example.com", supmodel.KindBounce, now)
	add(t, s, proj, "another@example.com", supmodel.KindBounce, now.Add(-time.Minute))

	rows, err := s.List(t.Context(), proj, store.SuppressionFilter{Search: "%", Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if len(rows) != 0 {
		t.Errorf("a bare %% matched %d addresses, so wildcards are reaching LIKE unescaped", len(rows))
	}
}

func TestKindFilterAndSearchCombine(t *testing.T) {
	s := testStore(t)
	proj := newProject(t, s)
	now := time.Now().UTC()
	add(t, s, proj, "ada@example.com", supmodel.KindBounce, now)
	add(t, s, proj, "adam@example.com", supmodel.KindManual, now.Add(-time.Minute))

	rows, err := s.List(t.Context(), proj, store.SuppressionFilter{
		Kind: supmodel.KindManual, Search: "ad", Limit: 10,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if len(rows) != 1 || rows[0].Email != "adam@example.com" {
		t.Fatalf("filtered search returned %+v", rows)
	}
}

// Every store query scopes on project first, and paging must not be
// a way around that.
func TestPagingStaysInsideTheProject(t *testing.T) {
	s := testStore(t)
	mine, theirs := newProject(t, s), newProject(t, s)
	now := time.Now().UTC()
	for i := range 5 {
		add(t, s, mine, fmt.Sprintf("mine%d@example.com", i), supmodel.KindBounce, now.Add(-time.Duration(i)*time.Minute))
		add(t, s, theirs, fmt.Sprintf("theirs%d@example.com", i), supmodel.KindBounce, now.Add(-time.Duration(i)*time.Minute))
	}

	for _, e := range walk(t, s, mine, store.SuppressionFilter{}, 2) {
		if e[:4] != "mine" {
			t.Errorf("paging returned %q from another project", e)
		}
	}
}
