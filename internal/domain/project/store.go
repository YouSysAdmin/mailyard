// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package project

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/database"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
)

// Store persists projects, memberships, roles and invitations. Project
// scoped: a method taking projID answers nothing for a row another
// project owns.
type Store struct {
	database.Base
}

// NewStore binds the project store to db. engine is the dialect
// identifier the queries are rebound for - pass the owning backend's
// Engine() value.
func NewStore(db *sql.DB) *Store {
	return &Store{Base: database.NewBase(db)}
}

// ----------------------------------------------------------------------------
// Projects
// ----------------------------------------------------------------------------
const wsSelect = `
SELECT id, name, slug, description, owner_id, default_language,
       plan_id, default_role_id, strict_senders,
       track_opens, track_clicks, bounce_address, alert_email, sandbox_retention_days,
       created_at, updated_at
FROM projects`

// Get returns one project by id, or nil when there is no such row.
func (s *Store) Get(ctx context.Context, id string) (*projmodel.Project, error) {
	row := s.QueryRow(ctx, wsSelect+` WHERE id = ?`, id)
	w, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return w, err
}

// GetBySlug returns one project by slug, or nil when there is no such
// row.
func (s *Store) GetBySlug(ctx context.Context, slug string) (*projmodel.Project, error) {
	row := s.QueryRow(ctx, wsSelect+` WHERE slug = ?`, slug)
	w, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return w, err
}

// Put upserts the project keyed by id.
func (s *Store) Put(ctx context.Context, w *projmodel.Project) error {
	if w.CreatedAt.IsZero() {
		w.CreatedAt = time.Now().UTC()
	}

	_, err := s.Exec(ctx, `
        INSERT INTO projects (
            id, name, slug, description, owner_id, default_language,
            plan_id, default_role_id, strict_senders,
            track_opens, track_clicks, bounce_address, alert_email, sandbox_retention_days,
       created_at, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            name             = excluded.name,
            slug             = excluded.slug,
            description      = excluded.description,
            owner_id         = excluded.owner_id,
            default_language = excluded.default_language,
            plan_id          = excluded.plan_id,
            default_role_id  = excluded.default_role_id,
            strict_senders   = excluded.strict_senders,
            track_opens      = excluded.track_opens,
            track_clicks     = excluded.track_clicks,
            bounce_address   = excluded.bounce_address,
            alert_email      = excluded.alert_email,
            sandbox_retention_days = excluded.sandbox_retention_days,
            updated_at       = excluded.updated_at
    `,
		w.ID, w.Name, w.Slug, w.Description, database.NullStr(w.OwnerID), w.DefaultLanguage,
		database.NullStr(w.PlanID), database.NullStr(w.DefaultRoleID),
		w.StrictSenders, w.TrackOpens,
		w.TrackClicks, w.BounceAddress, w.AlertEmail, w.SandboxRetentionDays,
		w.CreatedAt, database.NullTime(w.UpdatedAt),
	)

	return err
}

// Delete removes the project. Every tenant table goes with it via ON
// DELETE CASCADE, the email log included since 00070.
//
// Offloaded attachment BLOBS are not reachable from SQL and are dropped
// by the handler before this runs - see Handler.Delete.
func (s *Store) Delete(ctx context.Context, id string) error {
	_, err := s.Exec(ctx, `DELETE FROM projects WHERE id = ?`, id)

	return err
}

// List returns every project, oldest first. Platform-admin surface.
func (s *Store) List(ctx context.Context) ([]*projmodel.Project, error) {
	rows, err := s.Query(ctx, wsSelect+` ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	return collectProjects(rows)
}

// ListForUser returns the projects the user is a member of.
//
// A subquery rather than a JOIN so it can reuse wsSelect. It carried
// its own copy of the column list, aliased, and that copy went stale
// the moment projects gained a column - fourteen destinations for a
// fifteen-column scan, which no test caught because nothing listed a
// project by user. Two hand-kept column lists feeding one scanner is
// the emails-table trap, and the fix is to have one list rather than
// a guard over two.
func (s *Store) ListForUser(ctx context.Context, userID string) ([]*projmodel.Project, error) {
	rows, err := s.Query(ctx, wsSelect+`
WHERE id IN (SELECT project_id FROM project_members WHERE user_id = ?)
ORDER BY created_at ASC`, userID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	return collectProjects(rows)
}

// MembershipsForUser is every membership one account holds, with the
// effective role resolved, in one query.
//
// It exists so the project LIST can ship each row's access the way
// GET /projects/:id ships one project's. The console was gating the
// buttons on a row by the ACTIVE project's permissions, which is a
// different project whenever more than one is listed - so a member with
// no members:read was offered a Members button that answered 403, and a
// member who did hold it elsewhere was refused a page they could read.
//
// One query, not one per project: this is the same join GetMember does,
// with the user rather than the pair as the predicate.
func (s *Store) MembershipsForUser(ctx context.Context, userID string) ([]*projmodel.Member, error) {
	rows, err := s.Query(ctx, memberSelect+` WHERE m.user_id = ?`, userID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*projmodel.Member
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, m)
	}

	return out, rows.Err()
}

// ----------------------------------------------------------------------------
// Members
// ----------------------------------------------------------------------------
// memberSelect resolves the membership AND the role it effectively
// carries in one round trip - this feeds the authorization check on
// every project-scoped request, so a second query here would be a
// second query everywhere.
//
// The effective role is the member's own, falling back to the
// project's default. Doing it in SQL rather than in Go is what keeps
// that one round trip: the caller and the caller's permissions arrive
// together, and no code path can forget to apply the default.
//
// Both joins are tenancy-scoped (r.project_id = m.project_id), so a
// role id from another project resolves to NULL even if one ever lands
// in either column. COALESCE over r.permissions falls back to an empty
// string meaning "no role resolved": a real role can never yield it,
// because the column is NOT NULL DEFAULT '[]' - a role granting nothing
// is '[]', which is a deliberate lockdown and must stay distinguishable
// from absence.
const memberSelect = `
SELECT m.id, m.project_id, m.user_id, u.email, m.owner, m.created_at,
       COALESCE(m.role_id, p.default_role_id),
       m.role_id IS NULL AND p.default_role_id IS NOT NULL,
       COALESCE(r.name, ''), COALESCE(r.permissions, '')
FROM project_members m
JOIN users u ON u.id = m.user_id
JOIN projects p ON p.id = m.project_id
LEFT JOIN project_roles r
       ON r.id = COALESCE(m.role_id, p.default_role_id)
      AND r.project_id = m.project_id`

// GetMember returns the membership row linking user to project, or
// (nil, nil) when the user is not a member.
func (s *Store) GetMember(ctx context.Context, projID, userID string) (*projmodel.Member, error) {
	row := s.QueryRow(ctx, memberSelect+` WHERE m.project_id = ? AND m.user_id = ?`, projID, userID)
	m, err := scanMember(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return m, err
}

// PutMember ensures a membership row exists and does nothing else.
//
// It writes neither role_id nor owner on conflict, and that is a
// contract rather than an omission. Three callers re-put existing
// members - AddMember, AcceptInvitation and the OIDC auto-provision -
// and none of them is making a statement about either column. An
// upsert that wrote excluded.role_id would have every one of them
// silently clear a role an owner assigned - one that wrote
// excluded.owner would have an OIDC sign-in DEMOTE the person who
// created the project. Each column has exactly one writer -
// SetMemberRole and SetMemberOwner - and a caller that means to change
// one says so.
func (s *Store) PutMember(ctx context.Context, m *projmodel.Member) error {
	if m.ID == "" {
		m.ID = ids.New()
	}

	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}

	_, err := s.Exec(ctx, `
        INSERT INTO project_members (id, project_id, user_id, owner, role_id, created_at)
        VALUES (?, ?, ?, ?, ?, ?)
        ON CONFLICT(project_id, user_id) DO NOTHING
    `, m.ID, m.ProjectID, m.UserID, m.Owner, database.NullStr(m.RoleID), m.CreatedAt)

	return err
}

// SetMemberRole assigns or clears (roleID == "") a member's role. The
// only writer of the column.
//
// One conditional UPDATE, not read-then-write: the EXISTS clause
// requires the role to live in this project, so a cross-project or
// unknown role id affects zero rows and is indistinguishable from a
// missing member - the tenancy rule, with no race window in which a
// deleted role could still be assigned.
//
// Clearing does not strip a member of everything: they fall back to
// the project default, which is the difference between "no role of
// their own" and a role granting nothing.
func (s *Store) SetMemberRole(ctx context.Context, projID, userID, roleID string) (bool, error) {
	res, err := s.Exec(ctx, `
        UPDATE project_members m
        SET role_id = ?
        WHERE m.project_id = ? AND m.user_id = ?
          AND (?::uuid IS NULL OR EXISTS (
              SELECT 1 FROM project_roles r WHERE r.id = ? AND r.project_id = ?
          ))
    `, database.NullStr(roleID), projID, userID, database.NullStr(roleID),
		database.NullStr(roleID), projID)
	if err != nil {
		return false, err
	}

	n, err := res.RowsAffected()

	return n > 0, err
}

// lockOwners locks the project's owner rows and returns whose they are.
//
// This is what makes the last-owner rule actually hold. Carrying the
// check as an EXISTS inside the mutating statement only looks atomic:
// the row lock is taken on the row being written, while the EXISTS reads
// a different row through the transaction snapshot, and under READ
// COMMITTED that read cannot see a concurrent uncommitted delete. Two
// owners removed at the same moment both pass, and the project is left
// with no owner at all, which nothing in the product can undo.
//
// Locking exactly the set the rule counts fixes it. Two callers contend
// on the same rows, the second one blocks, and when it wakes READ
// COMMITTED re-evaluates against the reduced set and refuses.
//
// Worth contrasting with the guards nearby that are safe as they stand,
// MarkUsed and ClaimTOTPStep: those put the predicate on the row they
// update, so the lock and the check are the same thing.
func (s *Store) lockOwners(ctx context.Context, tx *sql.Tx, projID string) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, s.Q(`
        SELECT user_id FROM project_members
        WHERE project_id = ? AND owner
        FOR UPDATE
    `), projID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	owners := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}

		owners[id] = true
	}

	return owners, rows.Err()
}

// SetMemberOwner promotes or demotes a project owner, refusing to
// remove the last one.
//
// Returns changed=false both for a member who does not exist and for
// the last owner, which the caller disambiguates - it already holds the
// membership row it is acting on.
func (s *Store) SetMemberOwner(ctx context.Context, projID, userID string, owner bool) (bool, error) {
	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}

	defer func() { _ = tx.Rollback() }()

	// Promotion needs no guard - it can only add one.
	if !owner {
		owners, lerr := s.lockOwners(ctx, tx, projID)
		if lerr != nil {
			return false, lerr
		}

		if owners[userID] && len(owners) == 1 {
			return false, nil
		}
	}

	res, err := tx.ExecContext(ctx, s.Q(`
        UPDATE project_members SET owner = ?
        WHERE project_id = ? AND user_id = ?
    `), owner, projID, userID)
	if err != nil {
		return false, err
	}

	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}

	return n > 0, nil
}

// RemoveMember deletes a membership unless it is the project's last
// owner. Same lock as SetMemberOwner, and the same reason: a project with
// no owner cannot be deleted or handed on, and nothing else in the
// product can put an owner back.
//
// Returns removed=false for a member who was not there and for the
// last owner. The caller has the row and can tell them apart.
func (s *Store) RemoveMember(ctx context.Context, projID, userID string) (bool, error) {
	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}

	defer func() { _ = tx.Rollback() }()

	owners, err := s.lockOwners(ctx, tx, projID)
	if err != nil {
		return false, err
	}

	if owners[userID] && len(owners) == 1 {
		return false, nil
	}

	res, err := tx.ExecContext(ctx, s.Q(`
        DELETE FROM project_members WHERE project_id = ? AND user_id = ?
    `), projID, userID)
	if err != nil {
		return false, err
	}

	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}

	return n > 0, nil
}

// ----------------------------------------------------------------------------
// Project roles
// ----------------------------------------------------------------------------

// ----------------------------------------------------------------------------
// Roles
// ----------------------------------------------------------------------------
// roleSelect carries the live member count so the console list and the
// referenced-delete refusal read the same number.
//
// The count includes members who hold the role by INHERITANCE - their
// own role_id is empty and the project names this one as its default.
// Counting only the explicit holders would report zero for the role
// most of the project is actually using, and that number is what the
// delete refusal is built on.
const roleSelect = `
SELECT r.id, r.project_id, r.name, r.description, r.permissions,
       (SELECT COUNT(*) FROM project_members m
         WHERE m.project_id = r.project_id
           AND COALESCE(m.role_id, p.default_role_id) = r.id),
       p.default_role_id IS NOT DISTINCT FROM r.id,
       r.created_at, r.updated_at
FROM project_roles r
JOIN projects p ON p.id = r.project_id`

// GetRole returns one role within projID, or nil when there is no such
// row.
func (s *Store) GetRole(ctx context.Context, projID, id string) (*projmodel.Role, error) {
	row := s.QueryRow(ctx, roleSelect+` WHERE r.project_id = ? AND r.id = ?`, projID, id)
	role, err := scanRole(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return role, err
}

// ListRoles returns the roles in projID.
func (s *Store) ListRoles(ctx context.Context, projID string) ([]*projmodel.Role, error) {
	rows, err := s.Query(ctx, roleSelect+` WHERE r.project_id = ? ORDER BY r.name ASC`, projID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	out := []*projmodel.Role{} // we need empty slice here
	for rows.Next() {
		role, err := scanRole(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, role)
	}

	return out, rows.Err()
}

// PutRole inserts or updates by id, always scoped to the project.
func (s *Store) PutRole(ctx context.Context, role *projmodel.Role) error {
	if role.ID == "" {
		role.ID = ids.New()
	}

	if role.CreatedAt.IsZero() {
		role.CreatedAt = time.Now().UTC()
	}

	perms := database.MustJSON(orEmpty(role.Permissions))
	_, err := s.Exec(ctx, `
        INSERT INTO project_roles (id, project_id, name, description, permissions, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            name = excluded.name,
            description = excluded.description,
            permissions = excluded.permissions,
            updated_at = excluded.updated_at
        WHERE project_roles.project_id = excluded.project_id
    `, role.ID, role.ProjectID, role.Name, role.Description, string(perms),
		role.CreatedAt, time.Now().UTC())

	return err
}

// DeleteRole removes a role nobody carries and the project does not
// name as its default. Deleting one members carry would drop them all
// to the project default; deleting the default would leave the project
// naming a row that is gone, which the member join reads as "no role".
//
// Both guards are inside the one statement, and the projects row is
// locked FOR UPDATE. One statement alone is not enough: a predicate
// over a row the statement does not write is read from a snapshot, so
// SetDefaultRole could name role X while this DELETE still sees the old
// default and removes X. SetDefaultRole writes the projects row, so the
// lock serializes them.
//
// The member guard stays a snapshot read. A member assigned this role in
// the same instant carries an id that is gone, which reads as "no role"
// rather than locking anyone out - migration 00054 declined the foreign
// key for that reason. Locking every membership row would put a
// project-wide lock on the common path to prevent a benign outcome.
//
// Returns (deleted, membersHolding, isDefault): zero rows affected is
// disambiguated by the other two, so the caller answers 409 saying
// which, or 404.
func (s *Store) DeleteRole(ctx context.Context, projID, id string) (bool, int, bool, error) {
	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return false, 0, false, err
	}

	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, s.Q(`
        SELECT id FROM projects WHERE id = ? FOR UPDATE
    `), projID); err != nil {
		return false, 0, false, err
	}

	res, err := tx.ExecContext(ctx, s.Q(`
        DELETE FROM project_roles r
        WHERE r.project_id = ? AND r.id = ?
          AND NOT EXISTS (
              SELECT 1 FROM project_members m
              WHERE m.project_id = r.project_id AND m.role_id = r.id
          )
          AND NOT EXISTS (
              SELECT 1 FROM projects p
              WHERE p.id = r.project_id AND p.default_role_id = r.id
          )
    `), projID, id)
	if err != nil {
		return false, 0, false, err
	}

	if n, err := res.RowsAffected(); err != nil {
		return false, 0, false, err
	} else if n > 0 {
		return true, 0, false, tx.Commit()
	}

	// Read the reason back inside the same transaction, still holding the
	// project lock - so the 409 names the state the refusal was decided
	// on rather than one it may have moved to since.
	var holding int
	var isDefault bool
	if err := tx.QueryRowContext(ctx, s.Q(`
        SELECT (SELECT COUNT(*) FROM project_members
                 WHERE project_id = ? AND role_id = ?),
               EXISTS (SELECT 1 FROM projects
                        WHERE id = ? AND default_role_id = ?)`),
		projID, id, projID, id).Scan(&holding, &isDefault); err != nil {
		return false, 0, false, err
	}

	return false, holding, isDefault, tx.Commit()
}

// SetDefaultRole names the role members carry when they have none of
// their own, or clears it (roleID == "").
//
// The EXISTS clause is the same tenancy guard SetMemberRole uses: a
// role from another project, or one that has just been deleted, moves
// zero rows rather than leaving the project pointing at nothing.
func (s *Store) SetDefaultRole(ctx context.Context, projID, roleID string) (bool, error) {
	res, err := s.Exec(ctx, `
        UPDATE projects p
        SET default_role_id = ?
        WHERE p.id = ?
          AND (?::uuid IS NULL OR EXISTS (
              SELECT 1 FROM project_roles r WHERE r.id = ? AND r.project_id = p.id
          ))
    `, database.NullStr(roleID), projID, database.NullStr(roleID), database.NullStr(roleID))
	if err != nil {
		return false, err
	}

	n, err := res.RowsAffected()

	return n > 0, err
}

func scanRole(r interface{ Scan(...any) error }) (*projmodel.Role, error) {
	var role projmodel.Role
	var perms string
	var updated sql.NullTime
	if err := r.Scan(&role.ID, &role.ProjectID, &role.Name, &role.Description, &perms,
		&role.Members, &role.Default, &role.CreatedAt, &updated); err != nil {
		return nil, err
	}

	database.MustUnmarshalJSON(perms, &role.Permissions)
	if role.Permissions == nil {
		role.Permissions = []string{}
	}

	if updated.Valid {
		t := updated.Time.UTC()
		role.UpdatedAt = &t
	}

	return &role, nil
}

func orEmpty(v []string) []string {
	if v == nil {
		return []string{}
	}

	return v
}

// ListMembers returns the members in projID.
func (s *Store) ListMembers(ctx context.Context, projID string) ([]*projmodel.Member, error) {
	rows, err := s.Query(ctx, memberSelect+` WHERE m.project_id = ? ORDER BY m.created_at ASC`, projID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*projmodel.Member
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, m)
	}

	return out, rows.Err()
}

// ----------------------------------------------------------------------------
// Invitations
// ----------------------------------------------------------------------------
// invitationSelect resolves the offered role's name alongside its id,
// so the console list can say "Support" rather than a uuid and so an
// invitation naming a role that has since been deleted shows up as
// offering the project default, which is what redeeming it will
// actually do.
const invitationSelect = `
SELECT i.id, i.project_id, i.email, i.role_id, COALESCE(r.name, ''),
       i.token, i.status, i.invited_by, i.expires_at, i.created_at
FROM project_invitations i
LEFT JOIN project_roles r ON r.id = i.role_id AND r.project_id = i.project_id`

// PutInvitation upserts an invitation keyed by id (accepting flips
// the status through the same path).
func (s *Store) PutInvitation(ctx context.Context, inv *projmodel.Invitation) error {
	if inv.CreatedAt.IsZero() {
		inv.CreatedAt = time.Now().UTC()
	}

	// Hashed at rest. Only the insert path carries a plaintext (a
	// fresh mint), and the conflict update below never touches the
	// token column - so a fetched row re-put to flip its status
	// cannot double-hash.
	storedToken := projmodel.HashInvitationToken(inv.Token)

	_, err := s.Exec(ctx, `
        INSERT INTO project_invitations (
            id, project_id, email, role_id, token, status, invited_by, expires_at, created_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            email   = excluded.email,
            role_id = excluded.role_id,
            status  = excluded.status
    `,
		inv.ID, inv.ProjectID, inv.Email, database.NullStr(inv.RoleID), storedToken,
		inv.Status, inv.InvitedBy, inv.ExpiresAt, inv.CreatedAt,
	)

	return err
}

// GetInvitationByToken returns one invitation by the PLAINTEXT token a
// link presented, or nil when there is no such row. The stored column
// is the hash - see HashInvitationToken.
func (s *Store) GetInvitationByToken(ctx context.Context, token string) (*projmodel.Invitation, error) {
	row := s.QueryRow(ctx, invitationSelect+` WHERE i.token = ?`, projmodel.HashInvitationToken(token))
	inv, err := scanInvitation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return inv, err
}

// ListInvitations returns the invitations in projID.
func (s *Store) ListInvitations(ctx context.Context, projID string) ([]*projmodel.Invitation, error) {
	rows, err := s.Query(ctx, invitationSelect+` WHERE i.project_id = ? ORDER BY i.created_at ASC`, projID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*projmodel.Invitation
	for rows.Next() {
		inv, err := scanInvitation(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, inv)
	}

	return out, rows.Err()
}

// DeleteInvitation removes one invitation from projID.
func (s *Store) DeleteInvitation(ctx context.Context, projID, id string) error {
	_, err := s.Exec(ctx, `DELETE FROM project_invitations WHERE project_id = ? AND id = ?`, projID, id)

	return err
}

// EnsureDefault returns the shared "default" project used when auth
// is disabled (no user concept, so no personal projects). Creates
// it without any membership rows - the middleware grants owner role
// directly in that mode.
func (s *Store) EnsureDefault(ctx context.Context) (*projmodel.Project, error) {
	w, err := s.GetBySlug(ctx, "default")
	if err != nil || w != nil {
		return w, err
	}

	w = &projmodel.Project{
		ID:              ids.New(),
		Name:            "Default",
		Slug:            "default",
		DefaultLanguage: "en",
		CreatedAt:       time.Now().UTC(),
	}

	return w, s.Put(ctx, w)
}

// insertWithOwner creates the project and its owner membership in
// one transaction so a crash cannot leave an ownerless project.
func (s *Store) insertWithOwner(ctx context.Context, w *projmodel.Project, ownerID string) error {
	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	defer func() {
		// ErrTxDone is the normal path: Commit already ran. Anything
		// else means the rollback itself failed, which can leave locks
		// held and is worth saying out loud.
		if rerr := tx.Rollback(); rerr != nil && !errors.Is(rerr, sql.ErrTxDone) {
			slog.Warn("store: rollback failed", "err", rerr)
		}
	}()
	if _, err := tx.ExecContext(ctx, s.Q(`
        INSERT INTO projects (
            id, name, slug, description, owner_id, default_language,
            plan_id, default_role_id, strict_senders,
            track_opens, track_clicks, bounce_address, alert_email, sandbox_retention_days,
       created_at, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `),
		w.ID, w.Name, w.Slug, w.Description, database.NullStr(w.OwnerID), w.DefaultLanguage,
		database.NullStr(w.PlanID), database.NullStr(w.DefaultRoleID),
		w.StrictSenders, w.TrackOpens,
		w.TrackClicks, w.BounceAddress, w.AlertEmail, w.SandboxRetentionDays,
		w.CreatedAt, database.NullTime(w.UpdatedAt),
	); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, s.Q(`
        INSERT INTO project_members (id, project_id, user_id, owner, created_at)
        VALUES (?, ?, ?, TRUE, ?)
    `), ids.New(), w.ID, ownerID, time.Now().UTC()); err != nil {
		return err
	}

	return tx.Commit()
}

// CreateWithOwner persists a new project and makes ownerID its
// owner member atomically. Used by the create endpoint.
func (s *Store) CreateWithOwner(ctx context.Context, w *projmodel.Project, ownerID string) error {
	if w.CreatedAt.IsZero() {
		w.CreatedAt = time.Now().UTC()
	}

	return s.insertWithOwner(ctx, w, ownerID)
}

func scanProject(r interface{ Scan(...any) error }) (*projmodel.Project, error) {
	var w projmodel.Project
	var updated sql.NullTime
	if err := r.Scan(&w.ID, &w.Name, &w.Slug, &w.Description, database.Str(&w.OwnerID),
		&w.DefaultLanguage, database.Str(&w.PlanID), database.Str(&w.DefaultRoleID),
		&w.StrictSenders, &w.TrackOpens, &w.TrackClicks, &w.BounceAddress,
		&w.AlertEmail, &w.SandboxRetentionDays, &w.CreatedAt, &updated); err != nil {
		return nil, err
	}

	if updated.Valid {
		w.UpdatedAt = new(updated.Time)
	}

	return &w, nil
}

func collectProjects(rows *sql.Rows) ([]*projmodel.Project, error) {
	var out []*projmodel.Project
	for rows.Next() {
		w, err := scanProject(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, w)
	}

	return out, rows.Err()
}

func scanMember(r interface{ Scan(...any) error }) (*projmodel.Member, error) {
	var m projmodel.Member
	var rolePerms string
	if err := r.Scan(&m.ID, &m.ProjectID, &m.UserID, &m.Email, &m.Owner, &m.CreatedAt,
		database.Str(&m.RoleID), &m.InheritedRole, &m.RoleName, &rolePerms); err != nil {
		return nil, err
	}

	// Empty string is the join saying "no role resolved" - see
	// memberSelect. Anything else is the role's JSON, '[]' included.
	if rolePerms != "" {
		m.HasRole = true
		database.MustUnmarshalJSON(rolePerms, &m.RolePermissions)
	}

	return &m, nil
}

func scanInvitation(r interface{ Scan(...any) error }) (*projmodel.Invitation, error) {
	var inv projmodel.Invitation
	if err := r.Scan(&inv.ID, &inv.ProjectID, &inv.Email, database.Str(&inv.RoleID), &inv.RoleName,
		&inv.Token, &inv.Status, &inv.InvitedBy, &inv.ExpiresAt, &inv.CreatedAt); err != nil {
		return nil, err
	}

	return &inv, nil
}
