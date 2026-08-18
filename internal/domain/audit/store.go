// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package audit persists and serves the audit trails.
package audit

import (
	"context"
	"database/sql"
	"time"

	"github.com/yousysadmin/mailyard/internal/database"
	amodel "github.com/yousysadmin/mailyard/internal/models/audit"
)

// Store persists the operational and security trails. Project scoped:
// a method taking projID answers nothing for a row another project
// owns.
type Store struct {
	database.Base
}

// NewStore takes optional read replicas, used by the project and security trails
// (database.replica_reads.audit_log). Already eventually consistent -
// audit writes go through an async queue.
//
// Whether they arrive at all is the operator's call - see env.ReplicaReadsConfig.
func NewStore(db *sql.DB, replicas ...*sql.DB) *Store {
	return &Store{Base: database.NewBase(db, replicas...)}
}

const eventSelect = `
SELECT id, category, type, project_id, actor_id, actor_email,
       client_ip, user_agent, method, path, status, detail, created_at
FROM audit_events`

// Put inserts the audit event, or updates the row when its id already
// exists.
func (s *Store) Put(ctx context.Context, e *amodel.Event) error {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}

	_, err := s.Exec(ctx, `
        INSERT INTO audit_events (
            id, category, type, project_id, actor_id, actor_email,
            client_ip, user_agent, method, path, status, detail, created_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, e.ID, e.Category, e.Type, database.NullStr(e.ProjectID), database.NullStr(e.ActorID), e.ActorEmail,
		e.ClientIP, e.UserAgent, e.Method, e.Path, e.Status, e.Detail, e.CreatedAt)

	return err
}

// ListProject returns operational events for one project, newest
// first. Scoped by projID like every other tenant read.
func (s *Store) ListProject(ctx context.Context, projID string, limit, offset int) ([]*amodel.Event, error) {
	rows, err := s.ReadQuery(ctx, eventSelect+`
        WHERE category = ? AND project_id = ?
        ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		amodel.CategoryProject, projID, limit, offset)
	if err != nil {
		return nil, err
	}

	return scanEvents(rows)
}

// GetProject returns one project event by id. Scoped on the
// project AND the category, so a security event's id resolves to
// nothing here even for an admin of some project.
func (s *Store) GetProject(ctx context.Context, projID, id string) (*amodel.Event, error) {
	rows, err := s.Query(ctx, eventSelect+`
        WHERE category = ? AND project_id = ? AND id = ?`,
		amodel.CategoryProject, projID, id)
	if err != nil {
		return nil, err
	}

	events, err := scanEvents(rows)
	if err != nil || len(events) == 0 {
		return nil, err
	}

	return events[0], nil
}

// ExportProject is ListProject over a time window and without paging,
// for an operator taking the trail out of here.
//
// Newest first, like the list and for the same reason - if the cap in
// the handler cuts the result, the half kept is the half somebody is
// looking at. The upper bound is EXCLUSIVE so a caller can ask for one
// day as [day, next day) without expressing the last microsecond of it.
func (s *Store) ExportProject(ctx context.Context, projID string, from, to time.Time, limit int) ([]*amodel.Event, error) {
	rows, err := s.ReadQuery(ctx, eventSelect+`
        WHERE category = ? AND project_id = ?
          AND created_at >= ? AND created_at < ?
        ORDER BY created_at DESC LIMIT ?`,
		amodel.CategoryProject, projID, from, to, limit)
	if err != nil {
		return nil, err
	}

	return scanEvents(rows)
}

// ExportForActor is the same window over one account's security events.
func (s *Store) ExportForActor(ctx context.Context, actorID string, from, to time.Time, limit int) ([]*amodel.Event, error) {
	rows, err := s.ReadQuery(ctx, eventSelect+`
        WHERE category = ? AND actor_id = ?
          AND created_at >= ? AND created_at < ?
        ORDER BY created_at DESC LIMIT ?`,
		amodel.CategorySecurity, actorID, from, to, limit)
	if err != nil {
		return nil, err
	}

	return scanEvents(rows)
}

// ExportSecurity is the same window over every account, for a platform
// admin. Unscoped by design, like ListSecurity beside it.
func (s *Store) ExportSecurity(ctx context.Context, from, to time.Time, limit int) ([]*amodel.Event, error) {
	rows, err := s.ReadQuery(ctx, eventSelect+`
        WHERE category = ?
          AND created_at >= ? AND created_at < ?
        ORDER BY created_at DESC LIMIT ?`,
		amodel.CategorySecurity, from, to, limit)
	if err != nil {
		return nil, err
	}

	return scanEvents(rows)
}

// ListForActor returns security events for one user, newest first.
func (s *Store) ListForActor(ctx context.Context, actorID string, limit, offset int) ([]*amodel.Event, error) {
	rows, err := s.ReadQuery(ctx, eventSelect+`
        WHERE category = ? AND actor_id = ?
        ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		amodel.CategorySecurity, actorID, limit, offset)
	if err != nil {
		return nil, err
	}

	return scanEvents(rows)
}

// ListSecurity returns every security event, for platform admins
// investigating sign-in activity across accounts.
func (s *Store) ListSecurity(ctx context.Context, limit, offset int) ([]*amodel.Event, error) {
	rows, err := s.ReadQuery(ctx, eventSelect+`
        WHERE category = ?
        ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		amodel.CategorySecurity, limit, offset)
	if err != nil {
		return nil, err
	}

	return scanEvents(rows)
}

// PurgeOlderThan trims the trail. Platform maintenance, unscoped.
func (s *Store) PurgeOlderThan(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.Exec(ctx, `DELETE FROM audit_events WHERE created_at < ?`, before)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

func scanEvents(rows *sql.Rows) ([]*amodel.Event, error) {
	defer func() { _ = rows.Close() }()
	var out []*amodel.Event
	for rows.Next() {
		var e amodel.Event
		if err := rows.Scan(&e.ID, &e.Category, &e.Type, database.Str(&e.ProjectID),
			database.Str(&e.ActorID), &e.ActorEmail, &e.ClientIP, &e.UserAgent, &e.Method, &e.Path,
			&e.Status, &e.Detail, &e.CreatedAt); err != nil {
			return nil, err
		}

		out = append(out, &e)
	}

	return out, rows.Err()
}
