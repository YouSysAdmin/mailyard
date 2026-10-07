// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package suppression

import (
	"testing"

	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	supmodel "github.com/yousysadmin/mailyard/internal/models/suppression"
)

// A manual create of a block that exists writes nothing and says so,
// and a list-scoped block sits beside the global one.
func TestInsertRefusesTheSameBlockTwice(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := t.Context()

	const email = "bob@x.test"
	proj, listA, _ := seedScopes(t, s, ctx, email)

	again := &supmodel.Suppression{ProjectID: proj, Email: "BOB@x.test", Kind: supmodel.KindManual, Reason: "again"}
	created, err := s.Insert(ctx, again)
	if err != nil || created {
		t.Fatalf("second global block: created=%v err=%v, want false", created, err)
	}

	rows, err := s.List(ctx, proj, store.SuppressionFilter{Email: email})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	for _, r := range rows {
		if r.UnsubscribeListID == "" && (r.Kind != supmodel.KindBounce || r.Reason != "mailbox full") {
			t.Errorf("the refused insert rewrote the global block: kind=%q reason=%q", r.Kind, r.Reason)
		}
	}

	scoped := &supmodel.Suppression{ProjectID: proj, Email: "fresh@x.test", Kind: supmodel.KindManual, UnsubscribeListID: listA}
	created, err = s.Insert(ctx, scoped)
	if err != nil || !created {
		t.Fatalf("scoped block: created=%v err=%v", created, err)
	}

	if hit, _ := s.IsSuppressed(ctx, proj, "fresh@x.test"); hit {
		t.Error("a list-scoped block answered as a global one")
	}

	if hit, _ := s.IsSuppressedForList(ctx, proj, "fresh@x.test", listA); !hit {
		t.Error("a list-scoped block does not block its list")
	}
}

// Upsert over an existing block hands back the stored id, not the one
// it minted and never wrote.
func TestUpsertReturnsTheStoredRow(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := t.Context()

	const email = "bob@x.test"
	proj, _, _ := seedScopes(t, s, ctx, email)

	rows, err := s.List(ctx, proj, store.SuppressionFilter{Email: email})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	var stored string
	for _, r := range rows {
		if r.UnsubscribeListID == "" {
			stored = r.ID
		}
	}

	sup := &supmodel.Suppression{ProjectID: proj, Email: email, Kind: supmodel.KindManual}
	if err := s.Upsert(ctx, sup); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	if sup.ID != stored {
		t.Errorf("upsert reported id %s, the stored row is %s", sup.ID, stored)
	}
}

// An import is one transaction: a row the database refuses takes the
// rows before it back with it, so nothing is half applied.
func TestUpsertAllLeavesNothingWhenOneRowIsRefused(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := t.Context()

	proj, _, _ := seedScopes(t, s, ctx, "seed@x.test")
	block := func(email, id string) *supmodel.Suppression {
		return &supmodel.Suppression{ID: id, ProjectID: proj, Email: email, Kind: supmodel.KindManual}
	}

	// The third row carries an id the uuid column will not take.
	err := s.UpsertAll(ctx, []*supmodel.Suppression{
		block("one@x.test", ""), block("two@x.test", ""), block("three@x.test", "banana"),
	})
	if err == nil {
		t.Fatal("a malformed id was accepted")
	}

	for _, email := range []string{"one@x.test", "two@x.test"} {
		rows, err := s.List(ctx, proj, store.SuppressionFilter{Email: email})
		if err != nil {
			t.Fatal(err)
		}

		if len(rows) != 0 {
			t.Errorf("%s was written although the import failed", email)
		}
	}

	if err := s.UpsertAll(ctx, []*supmodel.Suppression{block("one@x.test", ""), block("two@x.test", "")}); err != nil {
		t.Fatal(err)
	}

	rows, err := s.List(ctx, proj, store.SuppressionFilter{})
	if err != nil {
		t.Fatal(err)
	}

	if len(rows) < 2 {
		t.Errorf("a clean import wrote %d rows", len(rows))
	}
}
