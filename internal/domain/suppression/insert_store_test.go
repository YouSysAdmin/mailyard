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
