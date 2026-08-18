// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"context"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain/store"
)

// Paging the log must not lose a message.
//
// The cursor was created_at ALONE, and two messages can share one: the
// column is microsecond precision and a batch mints its rows in a tight
// loop. When the tie lands across a page boundary, the next page asks for
// `created_at < that instant` and every row holding it is skipped - not
// duplicated, skipped. They appear on neither page, so the log does not
// contain a message that was sent, and nothing about the result says so.
//
// Six rows sharing one timestamp, paged three at a time. The house rule
// this asserts is written down: the cursor is `(created_at, id)`, never
// created_at alone.
func TestPagingTheLogDoesNotSkipMessagesThatShareATimestamp(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := context.Background()

	projID := ids.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, owner_id, created_at)
		VALUES ($1, 'acme', $2, NULL, now())`, projID, projID); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	// One instant, six messages - a batch send.
	at := time.Now().UTC().Truncate(time.Microsecond)
	want := map[string]bool{}
	for range 6 {
		id := ids.New()
		want[id] = true
		if _, err := db.ExecContext(ctx, `
			INSERT INTO emails (id, project_id, sender, recipients, subject, status, created_at)
			VALUES ($1, $2, 'from@x.test', '["to@y.test"]', 's', 'sent', $3)`,
			id, projID, at); err != nil {
			t.Fatalf("seed email: %v", err)
		}
	}

	seen := map[string]bool{}
	f := store.EmailFilter{Limit: 3}
	for page := range 5 {
		rows, err := s.List(ctx, projID, f)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}

		if len(rows) == 0 {
			break
		}

		for _, e := range rows {
			if seen[e.ID] {
				t.Errorf("page %d returned %s again - the cursor is not advancing", page, e.ID)
			}

			seen[e.ID] = true
		}

		last := rows[len(rows)-1]
		created := last.CreatedAt
		f.Before = &created
		f.BeforeID = last.ID
	}

	if len(seen) != len(want) {
		t.Fatalf("paged over %d of %d messages - the rest share a created_at with a page "+
			"boundary and were skipped by both pages", len(seen), len(want))
	}

	for id := range want {
		if !seen[id] {
			t.Errorf("message %s appeared on no page", id)
		}
	}
}

// The timestamp on its own still works, because the older `?before=`
// contract has to keep answering something sensible - it just cannot
// promise completeness across a tie, which is why the id half exists.
func TestATimestampOnlyCursorStillPages(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := context.Background()

	projID := ids.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, owner_id, created_at)
		VALUES ($1, 'acme', $2, NULL, now())`, projID, projID); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	base := time.Now().UTC().Truncate(time.Microsecond)
	for i := range 4 {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO emails (id, project_id, sender, recipients, subject, status, created_at)
			VALUES ($1, $2, 'from@x.test', '["to@y.test"]', 's', 'sent', $3)`,
			ids.New(), projID, base.Add(-time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("seed email: %v", err)
		}
	}

	first, err := s.List(ctx, projID, store.EmailFilter{Limit: 2})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}

	if len(first) != 2 {
		t.Fatalf("first page has %d rows, want 2", len(first))
	}

	cursor := first[1].CreatedAt
	second, err := s.List(ctx, projID, store.EmailFilter{Limit: 2, Before: &cursor})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}

	if len(second) != 2 {
		t.Errorf("second page has %d rows, want 2", len(second))
	}

	for _, e := range second {
		if !e.CreatedAt.Before(cursor) {
			t.Errorf("row %s is not older than the cursor", e.ID)
		}
	}
}
