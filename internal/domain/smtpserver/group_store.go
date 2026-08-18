// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpserver

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/database"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

// GroupStore persists SMTP server groups. Project-scoped like every
// other tenant store: a foreign project's group is (nil, nil).
type GroupStore struct {
	database.Base
}

// NewGroupStore builds a GroupStore on db.
func NewGroupStore(db *sql.DB) *GroupStore {
	return &GroupStore{Base: database.NewBase(db)}
}

const groupSelect = `
SELECT id, project_id, name, slug, description, is_default, created_at
FROM smtp_server_groups`

// Get returns one SMTP server within projID, or nil when there is no
// such row.
func (s *GroupStore) Get(ctx context.Context, projID, id string) (*ssmodel.Group, error) {
	row := s.QueryRow(ctx, groupSelect+` WHERE project_id = ? AND id = ?`, projID, id)
	g, err := scanGroup(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return g, err
}

// GetBySlug is the delivery-side lookup: a send names a group by slug
// so an integration is not pinned to a uuid.
func (s *GroupStore) GetBySlug(ctx context.Context, projID, slug string) (*ssmodel.Group, error) {
	row := s.QueryRow(ctx, groupSelect+` WHERE project_id = ? AND slug = ?`, projID, slug)
	g, err := scanGroup(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return g, err
}

// GetDefault returns the project's default group, or (nil, nil) for a
// project created before migration 00003 backfilled them. Callers on
// the write path use EnsureDefault instead.
func (s *GroupStore) GetDefault(ctx context.Context, projID string) (*ssmodel.Group, error) {
	row := s.QueryRow(ctx, groupSelect+` WHERE project_id = ? AND is_default`, projID)
	g, err := scanGroup(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return g, err
}

// List returns the project's groups, the default one first.
func (s *GroupStore) List(ctx context.Context, projID string) ([]*ssmodel.Group, error) {
	rows, err := s.Query(ctx, groupSelect+` WHERE project_id = ? ORDER BY is_default DESC, name ASC`, projID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*ssmodel.Group
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, g)
	}

	return out, rows.Err()
}

// EnsureDefault returns the project's default group, creating it if
// this project has none.
//
// Migration 00003 backfilled every project that existed then, so this
// only fires for projects created since. Lazy rather than a call in
// the project create path: there are three places a project row comes
// into existence, and a missed one would leave a project whose sends
// resolve to no group at all.
func (s *GroupStore) EnsureDefault(ctx context.Context, projID string) (*ssmodel.Group, error) {
	g, err := s.GetDefault(ctx, projID)
	if err != nil || g != nil {
		return g, err
	}

	g = &ssmodel.Group{
		ID:          ids.New(),
		ProjectID:   projID,
		Name:        "Default",
		Slug:        "default",
		Description: "Used when a send names no group.",
		Default:     true,
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.Put(ctx, g); err != nil {
		// Lost a race, or the slug "default" is already taken by a
		// group the operator made by hand. Either way the answer is
		// whatever is there now, not an error.
		if again, err2 := s.GetDefault(ctx, projID); err2 == nil && again != nil {
			return again, nil
		}

		return nil, err
	}

	return g, nil
}

// Put inserts the SMTP server, or updates the row when its id already
// exists.
func (s *GroupStore) Put(ctx context.Context, g *ssmodel.Group) error {
	if g.CreatedAt.IsZero() {
		g.CreatedAt = time.Now().UTC()
	}

	// is_default is set on INSERT and never on the update path.
	// SetDefault is the only thing that moves it, which is what makes
	// "exactly one default per project" enforceable: an upsert carrying
	// the flag from a read taken moments earlier demotes a group somebody
	// else has just promoted, and the project is left with none - the
	// state that wedges every send AND the page that would repair it.
	// Renaming a group is not a statement about which one is the default.
	_, err := s.Exec(ctx, `
        INSERT INTO smtp_server_groups (id, project_id, name, slug, description, is_default, created_at)
        VALUES (?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            name        = excluded.name,
            slug        = excluded.slug,
            description = excluded.description
    `, g.ID, g.ProjectID, g.Name, g.Slug, g.Description, g.Default, g.CreatedAt)

	return err
}

// SetDefault makes one group the project's default, in one transaction,
// and reports whether that group was there to promote.
//
// One transaction, because doing it as two writes leaves a window where
// the project has no default at all. A crash or a cancelled request in
// that window makes the gap permanent, and then nothing works:
// ResolveCandidates finds no group, every send that names none is
// refused, and the console cannot repair it either - EnsureDefault tries
// to create a group called Default, the demoted row already holds the
// `default` slug, the unique index refuses the insert, and the fallback
// read finds no default. The repair fails for the same reason the state
// exists.
//
// So we demote and then promote inside the transaction, and we demote
// all the project's defaults rather than "all but this one". That
// makes promoting the group that is already default idempotent, and no
// statement has to compare an id against anything. If to promote
// matches no row we abandon the transaction, since the alternative is
// committing the demotion on its own.
func (s *GroupStore) SetDefault(ctx context.Context, projID, id string) (bool, error) {
	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}

	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, s.Q(`
        UPDATE smtp_server_groups SET is_default = FALSE
        WHERE project_id = ? AND is_default
    `), projID); err != nil {
		return false, err
	}

	res, err := tx.ExecContext(ctx, s.Q(`
        UPDATE smtp_server_groups SET is_default = TRUE
        WHERE project_id = ? AND id = ?
    `), projID, id)
	if err != nil {
		return false, err
	}

	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}

	if n == 0 {
		// Nothing promoted, so nothing is demoted either. A group that
		// is not there, or belongs to another project.
		return false, nil
	}

	return true, tx.Commit()
}

// Delete removes a group and moves its servers to the default one, in
// one transaction.
//
// Moving rather than deleting: a group is a routing label, and losing
// it must not lose the credentials behind it. Doing both in one
// transaction is what stops a crash in between from leaving servers
// pointing at a group that no longer exists - they would belong to no
// group, which no query looks for, so they would simply stop being
// picked with nothing to show why.
func (s *GroupStore) Delete(ctx context.Context, projID, id, defaultGroupID string) error {
	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, s.Q(`
        UPDATE smtp_servers SET group_id = ?
        WHERE project_id = ? AND group_id = ?
    `), defaultGroupID, projID, id); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, s.Q(`
        DELETE FROM smtp_server_groups WHERE project_id = ? AND id = ? AND NOT is_default
    `), projID, id); err != nil {
		return err
	}

	return tx.Commit()
}

// SlugTaken reports whether slug is already used in the project,
// ignoring exceptID so an update can keep its own slug.
//
// IS DISTINCT FROM over a NULL, not a comparison against an empty
// string. id is a uuid column, so an
// empty exceptID was compared as a uuid and answered 22P02 - whose
// pgconn Routine is string_to_uuid, which is exactly what
// database.MalformedID matches, so response.Internal softened it into
// 404 "not found". CREATE passes an empty exceptID by definition, so
// every attempt to create a group answered 404 before inserting
// anything, on both surfaces, and the log recorded a warn rather than an
// error. A NULL is distinct from every id, so the clause is simply true
// for a create.
func (s *GroupStore) SlugTaken(ctx context.Context, projID, slug, exceptID string) (bool, error) {
	var n int
	err := s.QueryRow(ctx, `
        SELECT COUNT(*) FROM smtp_server_groups
        WHERE project_id = ? AND slug = ? AND id IS DISTINCT FROM ?::uuid
    `, projID, slug, database.NullStr(exceptID)).Scan(&n)

	return n > 0, err
}

func scanGroup(r interface{ Scan(...any) error }) (*ssmodel.Group, error) {
	var g ssmodel.Group
	if err := r.Scan(&g.ID, &g.ProjectID, &g.Name, &g.Slug, &g.Description,
		&g.Default, &g.CreatedAt); err != nil {
		return nil, err
	}

	return &g, nil
}
