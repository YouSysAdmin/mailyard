// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpserver

import (
	"context"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

func groupStore(t *testing.T) (*GroupStore, string, context.Context) {
	t.Helper()

	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &GroupStore{Base: database.NewBase(db)}
	ctx := context.Background()

	proj := ids.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, owner_id, created_at)
		VALUES ($1, 'acme', $2, NULL, now())`, proj, proj); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	return s, proj, ctx
}

func defaultCount(t *testing.T, s *GroupStore, ctx context.Context, proj string) int {
	t.Helper()

	var n int
	if err := s.DB().QueryRowContext(ctx,
		`SELECT count(*) FROM smtp_server_groups WHERE project_id = $1 AND is_default`,
		proj).Scan(&n); err != nil {
		t.Fatalf("count defaults: %v", err)
	}

	return n
}

// A project has exactly one default group, and promotion must not be
// able to leave it with none.
//
// It was two writes from the handler - demote the old, promote the new -
// so the project spent an instant with no default and stayed that way
// for good if anything cut in between. That state is not cosmetic: every
// send that names no group is refused, and the console cannot repair it,
// because EnsureDefault creates a group called Default whose slug
// `default` the demoted row still holds, the unique index refuses the
// insert, and the fallback read finds no default either.
func TestPromotingAGroupNeverLeavesTheProjectWithoutADefault(t *testing.T) {
	s, proj, ctx := groupStore(t)

	first, err := s.EnsureDefault(ctx, proj)
	if err != nil {
		t.Fatalf("ensure default: %v", err)
	}

	second := &ssmodel.Group{
		ID: ids.New(), ProjectID: proj, Name: "Bulk", Slug: "bulk",
	}
	if err := s.Put(ctx, second); err != nil {
		t.Fatalf("put second: %v", err)
	}

	promoted, err := s.SetDefault(ctx, proj, second.ID)
	if err != nil || !promoted {
		t.Fatalf("promote: promoted=%v err=%v", promoted, err)
	}

	if n := defaultCount(t, s, ctx, proj); n != 1 {
		t.Fatalf("%d defaults, want exactly 1", n)
	}

	got, err := s.GetDefault(ctx, proj)
	if err != nil {
		t.Fatalf("get default: %v", err)
	}

	if got.ID != second.ID {
		t.Errorf("default is %s, want the promoted group %s", got.ID, second.ID)
	}

	// Promoting the one that already holds it is a no-op, not a way to
	// end up with two or none.
	if promoted, err := s.SetDefault(ctx, proj, second.ID); err != nil || !promoted {
		t.Errorf("re-promoting the default: promoted=%v err=%v", promoted, err)
	}

	if n := defaultCount(t, s, ctx, proj); n != 1 {
		t.Errorf("%d defaults after re-promoting, want 1", n)
	}

	// And the group it took the flag from is still there, demoted rather
	// than removed.
	old, err := s.Get(ctx, proj, first.ID)
	if err != nil {
		t.Fatalf("get the demoted group: %v", err)
	}

	if old == nil || old.Default {
		t.Error("the previous default is gone or still marked default")
	}
}

// A group that is not there promotes nothing AND demotes nothing. The
// demote runs first, so without the transaction abandoning itself on a
// zero-row promote, a bad id is exactly how a project loses its default.
func TestPromotingSomethingThatIsNotThereChangesNothing(t *testing.T) {
	s, proj, ctx := groupStore(t)

	def, err := s.EnsureDefault(ctx, proj)
	if err != nil {
		t.Fatalf("ensure default: %v", err)
	}

	// Another project's group, which is the reachable form of this: the
	// id is a real uuid and the row exists.
	other := ids.New()
	if _, err := s.DB().ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, owner_id, created_at)
		VALUES ($1, 'other', $2, NULL, now())`, other, other); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	theirs := &ssmodel.Group{ID: ids.New(), ProjectID: other, Name: "Theirs", Slug: "theirs"}
	if err := s.Put(ctx, theirs); err != nil {
		t.Fatalf("put theirs: %v", err)
	}

	promoted, err := s.SetDefault(ctx, proj, theirs.ID)
	if err != nil {
		t.Fatalf("promote: %v", err)
	}

	if promoted {
		t.Error("promoted another project's group")
	}

	if n := defaultCount(t, s, ctx, proj); n != 1 {
		t.Fatalf("%d defaults, want the original one intact", n)
	}

	got, err := s.GetDefault(ctx, proj)
	if err != nil {
		t.Fatalf("get default: %v", err)
	}

	if got.ID != def.ID {
		t.Errorf("default is %s, want %s - the demote committed without a promote", got.ID, def.ID)
	}
}

// Renaming a group says nothing about which group is the default.
//
// Put used to write is_default from the model, so an update built on a
// read taken moments earlier demoted whatever had been promoted in
// between - and left the project with no default, from a request that
// only changed a name.
func TestRenamingAGroupDoesNotMoveTheDefault(t *testing.T) {
	s, proj, ctx := groupStore(t)

	def, err := s.EnsureDefault(ctx, proj)
	if err != nil {
		t.Fatalf("ensure default: %v", err)
	}

	// A read taken before the flag moved, which is the whole shape of it.
	stale := &ssmodel.Group{
		ID: def.ID, ProjectID: proj, Name: "Renamed", Slug: def.Slug,
		Description: def.Description, Default: false, CreatedAt: def.CreatedAt,
	}
	if err := s.Put(ctx, stale); err != nil {
		t.Fatalf("put stale: %v", err)
	}

	if n := defaultCount(t, s, ctx, proj); n != 1 {
		t.Errorf("%d defaults after a rename, want 1", n)
	}

	got, err := s.Get(ctx, proj, def.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if got.Name != "Renamed" {
		t.Errorf("name = %q, want the rename applied", got.Name)
	}

	if !got.Default {
		t.Error("a rename demoted the default group")
	}
}
