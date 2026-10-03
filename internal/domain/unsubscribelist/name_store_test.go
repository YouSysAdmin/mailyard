// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package unsubscribelist

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	ulmodel "github.com/yousysadmin/mailyard/internal/models/unsubscribelist"
)

const testProject = "e66e7a4d-9e6c-4884-869a-cf9ffcf22181"

func newListStore(t *testing.T) *Store {
	t.Helper()
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	dbtest.Schema(t, db, `
        INSERT INTO projects (id, name, slug, default_language, created_at)
        VALUES ('`+testProject+`', 'Test', 'test', 'en', now())`)

	return NewStore(db)
}

// A list name is one name whatever its case: found by any spelling,
// and a second spelling is refused by the schema.
func TestAListNameIgnoresCase(t *testing.T) {
	s := newListStore(t)
	ctx := t.Context()

	l := &ulmodel.List{ID: ids.New(), ProjectID: testProject, Name: "News", Active: true}
	if err := s.Put(ctx, l); err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := s.GetByName(ctx, testProject, "NEWS")
	if err != nil || got == nil || got.ID != l.ID {
		t.Fatalf("GetByName(NEWS) = %+v, %v, want the list", got, err)
	}

	dup := &ulmodel.List{ID: ids.New(), ProjectID: testProject, Name: "news", Active: true}
	if err := s.Put(ctx, dup); err == nil {
		t.Error("a second list differing only in case was stored")
	}
}

// The migration renames lists that already differ only in case, keeping
// the oldest, so the unique index can be built.
func TestTheMigrationRenamesCaseDuplicates(t *testing.T) {
	s := newListStore(t)
	ctx := t.Context()

	if _, err := s.DB().ExecContext(ctx, `DROP INDEX idx_unsubscribe_lists_project_lower_name`); err != nil {
		t.Fatalf("drop index: %v", err)
	}

	first, second := ids.New(), ids.New()
	if _, err := s.DB().ExecContext(ctx, `
        INSERT INTO unsubscribe_lists (id, project_id, name, created_at)
        VALUES ($1, $3, 'News', now() - interval '1 day'), ($2, $3, 'NEWS', now())`,
		first, second, testProject); err != nil {
		t.Fatalf("seed: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dbtest.MigrationsDir(t), "00111_unsubscribe_lists_name_lower_unique.sql"))
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}

	up, _, _ := strings.Cut(strings.SplitN(string(raw), "-- +goose Up", 2)[1], "-- +goose Down")
	//sqlconst:allow the statement is the migration file as committed
	if _, err := s.DB().ExecContext(ctx, up); err != nil {
		t.Fatalf("apply: %v", err)
	}

	a, _ := s.Get(ctx, testProject, first)
	b, _ := s.Get(ctx, testProject, second)
	if a == nil || b == nil {
		t.Fatal("a list was lost")
	}

	if a.Name != "News" || b.Name != "NEWS-"+second[:8] {
		t.Errorf("names = %q, %q, want the oldest kept and the other suffixed", a.Name, b.Name)
	}
}
