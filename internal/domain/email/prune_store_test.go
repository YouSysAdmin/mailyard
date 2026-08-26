// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"context"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
)

func pruneFixture(t *testing.T) (*Store, string, context.Context) {
	t.Helper()

	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := t.Context()

	projID := ids.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, owner_id, created_at)
		VALUES ($1, 'acme', $2, NULL, now())`, projID, projID); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	return s, projID, ctx
}

// seedAt plants a row with an id and a created_at chosen independently,
// which is what the fallback exists for.
func seedAt(t *testing.T, s *Store, ctx context.Context, projID, id string, at time.Time) {
	t.Helper()

	if _, err := s.DB().ExecContext(ctx, `
		INSERT INTO emails (id, project_id, sender, recipients, subject, status, created_at)
		VALUES ($1, $2, 'a@b.test', '["c@d.test"]', 's', 'sent', $3)`, id, projID, at); err != nil {
		t.Fatalf("seed email: %v", err)
	}
}

// A lookup by id must find the row however far created_at is from the
// id's own timestamp.
//
// `emails` is partitioned by created_at, so `WHERE id = ?` visits every
// live partition - 488us and 209 buffers at 104 weekly partitions, which
// is one default install with retention off. A v7 id carries the
// millisecond it was minted, so the window can be derived from the id.
//
// But the id is minted before the insert, and a caller that sets
// created_at itself puts them wherever it likes. That is what this
// asserts: the derived window is a HINT, and the answer never depends on
// it being right. Missing costs a second query, never a row.
func TestALookupByIDFindsTheRowWhereverItsWindowFalls(t *testing.T) {
	s, projID, ctx := pruneFixture(t)

	now := time.Now().UTC()
	for _, tc := range []struct {
		what string
		at   time.Time
	}{
		{"created the moment the id was minted", now},
		{"a minute later", now.Add(time.Minute)},
		// Outside the window in both directions, which is the fallback.
		{"a day later than its id", now.Add(24 * time.Hour)},
		{"three weeks earlier - a backfilled row", now.Add(-21 * 24 * time.Hour)},
	} {
		id := ids.New()
		seedAt(t, s, ctx, projID, id, tc.at)

		got, err := s.GetAny(ctx, id)
		if err != nil {
			t.Errorf("%s: GetAny: %v", tc.what, err)
			continue
		}

		if got == nil {
			t.Errorf("%s: GetAny found nothing - the derived window swallowed a real row", tc.what)
			continue
		}

		scoped, err := s.Get(ctx, projID, id)
		if err != nil || scoped == nil {
			t.Errorf("%s: Get found nothing (err=%v)", tc.what, err)
		}
	}
}

// An id that carries no timestamp at all still has to work. Nothing
// mints these today - TestNothingElseMintsAnID sees to that - but a row
// written by an older binary, or an id typed by hand, must not become a
// message that does not exist.
func TestALookupByANonV7IDStillWorks(t *testing.T) {
	s, projID, ctx := pruneFixture(t)

	// A LITERAL v4, not a minted one: TestNothingElseMintsAnID forbids a
	// second generator anywhere in the tree, including here, and it is
	// right to - the point of that rule is that one table never carries
	// two uuid versions. A constant plants the shape without minting it.
	const v4 = "3f2504e0-4f89-41d3-9a0c-0305e82c3301"
	seedAt(t, s, ctx, projID, v4, time.Now().UTC())

	if _, ok := ids.MintedAt(v4); ok {
		t.Fatal("the fixture is a v7, so it proves nothing")
	}

	got, err := s.GetAny(ctx, v4)
	if err != nil || got == nil {
		t.Errorf("GetAny on a v4 id: got=%v err=%v", got, err)
	}

	if scoped, err := s.Get(ctx, projID, v4); err != nil || scoped == nil {
		t.Errorf("Get on a v4 id: got=%v err=%v", scoped, err)
	}
}

// A missing message is still missing, and a malformed id is still not a
// message. The pruning must not turn either into something else.
func TestAMissingMessageIsStillMissing(t *testing.T) {
	s, projID, ctx := pruneFixture(t)

	if got, err := s.GetAny(ctx, ids.New()); err != nil || got != nil {
		t.Errorf("GetAny for an id nothing was written under: got=%v err=%v", got, err)
	}

	if got, err := s.Get(ctx, projID, ids.New()); err != nil || got != nil {
		t.Errorf("Get for an id nothing was written under: got=%v err=%v", got, err)
	}

	// Another project's message stays invisible - the pruning changed the
	// window, not the tenancy.
	other := ids.New()
	if _, err := s.DB().ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, owner_id, created_at)
		VALUES ($1, 'other', $2, NULL, now())`, other, other); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	theirs := ids.New()
	seedAt(t, s, ctx, other, theirs, time.Now().UTC())

	if got, err := s.Get(ctx, projID, theirs); err != nil || got != nil {
		t.Errorf("Get reached another project's message: got=%v err=%v", got, err)
	}
}

// The tracking marks name the partition instead of visiting all of them,
// and they take created_at from the row the handler already read. A
// wrong value must not silently count nothing.
func TestTheTrackingMarksNeedTheRowsOwnCreatedAt(t *testing.T) {
	s, projID, ctx := pruneFixture(t)

	id := ids.New()
	at := time.Now().UTC().Truncate(time.Microsecond)
	seedAt(t, s, ctx, projID, id, at)

	first, opens, err := s.MarkOpened(ctx, id, at, time.Now().UTC())
	if err != nil {
		t.Fatalf("mark opened: %v", err)
	}

	if !first || opens != 1 {
		t.Errorf("first=%v opens=%d, want the first open counted", first, opens)
	}

	clicks, err := s.MarkClicked(ctx, id, at, time.Now().UTC())
	if err != nil {
		t.Fatalf("mark clicked: %v", err)
	}

	if clicks != 1 {
		t.Errorf("clicks=%d, want 1", clicks)
	}

	// A created_at that is not the row's matches nothing, and says so by
	// counting nothing rather than by erroring. That is why the caller
	// passes the value it read rather than one it derived.
	if _, opens, err := s.MarkOpened(ctx, id, at.Add(time.Hour), time.Now().UTC()); err != nil || opens != 0 {
		t.Errorf("a wrong created_at counted %d opens (err=%v), want 0", opens, err)
	}
}
