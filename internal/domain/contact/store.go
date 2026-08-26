// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package contact persists auto-tracked recipient addresses and
// serves the read-only console surface.
package contact

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/database"
	cmodel "github.com/yousysadmin/mailyard/internal/models/contact"
)

// Store persists auto-tracked recipient addresses. Project scoped: a
// method taking projID answers nothing for a row another project owns.
type Store struct {
	database.Base
}

// NewStore takes optional read replicas, used by the contact list, its search and its
// count (database.replica_reads.contacts). Written only by the worker,
// so nobody reads back what they just wrote.
//
// Whether they arrive at all is the operator's call - see
// env.ReplicaReadsConfig.
func NewStore(db *sql.DB, replicas ...*sql.DB) *Store {
	return &Store{Base: database.NewBase(db, replicas...)}
}

const contactSelect = `
SELECT id, project_id, email, name, sent_count, fail_count,
       last_sent_at, last_failed_at, created_at, updated_at
FROM contacts`

// Get returns one contact within projID, or nil when there is no such
// row.
func (s *Store) Get(ctx context.Context, projID, id string) (*cmodel.Contact, error) {
	row := s.QueryRow(ctx, contactSelect+` WHERE project_id = ? AND id = ?`, projID, id)
	c, err := scanContact(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return c, err
}

// GetByEmail resolves one contact by address. Used by the DSN
// pipeline to check a reported recipient is somebody this project
// actually mailed before acting on the report.
func (s *Store) GetByEmail(ctx context.Context, projID, email string) (*cmodel.Contact, error) {
	row := s.QueryRow(ctx, contactSelect+` WHERE project_id = ? AND email = ?`, projID, strings.ToLower(email))
	c, err := scanContact(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return c, err
}

// List returns a page of contacts, most recently active first.
// search matches the address or the display name, case-insensitively.
func (s *Store) List(ctx context.Context, projID, search string, limit, offset int) ([]*cmodel.Contact, error) {
	query := contactSelect + ` WHERE project_id = ?`
	args := []any{projID}
	if search != "" {
		// LOWER on both operands rather than ILIKE: same result, and
		// it keeps the expression usable by a plain lower(email) index.
		// EscapeLike, or the % and _ a caller types are WILDCARDS: a
		// search for "%" returned the whole project. Not a leak - the
		// query is scoped to project_id first - but a scan of a table
		// nobody meant to scan, and a filter that silently ignores
		// what was typed.
		query += ` AND (LOWER(email) LIKE ? ESCAPE '\' OR LOWER(name) LIKE ? ESCAPE '\')`
		pattern := "%" + database.EscapeLike(strings.ToLower(search)) + "%"
		args = append(args, pattern, pattern)
	}

	// COALESCE so contacts that have only ever failed still sort
	// sensibly against ones that have sent.
	query += ` ORDER BY COALESCE(last_sent_at, last_failed_at, created_at) DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.ReadQuery(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*cmodel.Contact
	for rows.Next() {
		c, err := scanContact(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, c)
	}

	return out, rows.Err()
}

// Count reports how many contacts match, for the pager.
func (s *Store) Count(ctx context.Context, projID, search string) (int, error) {
	query := `SELECT COUNT(*) FROM contacts WHERE project_id = ?`
	args := []any{projID}
	if search != "" {
		// The same escaped pattern List builds, or the pager disagrees
		// with the rows: a search for "100%_off" counted every contact
		// in the project while the list showed the one that matches
		// literally.
		query += ` AND (LOWER(email) LIKE ? ESCAPE '\' OR LOWER(name) LIKE ? ESCAPE '\')`
		pattern := "%" + database.EscapeLike(strings.ToLower(search)) + "%"
		args = append(args, pattern, pattern)
	}

	var n int
	err := s.ReadQueryRow(ctx, query, args...).Scan(&n)

	return n, err
}

// RecordOutcome upserts a contact and folds in one terminal delivery
// result. Called by the worker as each message finishes, so it must
// be a single statement: a read-then-write would race two workers
// finishing messages to the same address.
//
// Name is only written when non-empty, so a later send with a bare
// address does not erase a display name learned earlier.
func (s *Store) RecordOutcome(ctx context.Context, projID, email, name string, sent bool, at time.Time) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil
	}

	sentInc, failInc := 0, 1
	var sentAt, failedAt any = nil, at
	if sent {
		sentInc, failInc = 1, 0
		sentAt, failedAt = at, nil
	}

	_, err := s.Exec(ctx, `
        INSERT INTO contacts (
            id, project_id, email, name, sent_count, fail_count,
            last_sent_at, last_failed_at, created_at, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(project_id, email) DO UPDATE SET
            name           = CASE WHEN excluded.name <> '' THEN excluded.name ELSE contacts.name END,
            sent_count     = contacts.sent_count + excluded.sent_count,
            fail_count     = contacts.fail_count + excluded.fail_count,
            last_sent_at   = COALESCE(excluded.last_sent_at, contacts.last_sent_at),
            last_failed_at = COALESCE(excluded.last_failed_at, contacts.last_failed_at),
            updated_at     = excluded.updated_at
    `, ids.New(), projID, email, name, sentInc, failInc, sentAt, failedAt, at, at)

	return err
}

// PurgeForEmail removes one address from the project, for the data
// deletion surface.
func (s *Store) PurgeForEmail(ctx context.Context, projID, email string) (int64, error) {
	res, err := s.Exec(ctx, `DELETE FROM contacts WHERE project_id = ? AND email = ?`,
		projID, strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

func scanContact(r interface{ Scan(...any) error }) (*cmodel.Contact, error) {
	var c cmodel.Contact
	var lastSent, lastFailed sql.NullTime
	if err := r.Scan(&c.ID, &c.ProjectID, &c.Email, &c.Name,
		&c.SentCount, &c.FailCount, &lastSent, &lastFailed,
		&c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}

	if lastSent.Valid {
		c.LastSentAt = new(lastSent.Time)
	}

	if lastFailed.Valid {
		c.LastFailedAt = new(lastFailed.Time)
	}

	return &c, nil
}

// PurgeAll removes every contact in the project, for the
// erase-everything path.
func (s *Store) PurgeAll(ctx context.Context, projID string) (int64, error) {
	res, err := s.Exec(ctx, `DELETE FROM contacts WHERE project_id = ?`, projID)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

// Delete removes one contact. The next delivery to the address
// recreates it with a fresh tally, which is the whole cost.
func (s *Store) Delete(ctx context.Context, projID, id string) (bool, error) {
	res, err := s.Exec(ctx, `DELETE FROM contacts WHERE project_id = ? AND id = ?`, projID, id)
	if err != nil {
		return false, err
	}

	n, err := res.RowsAffected()

	return n > 0, err
}

// DeleteInactiveBefore removes the contacts nothing has happened to
// since before. GREATEST skips NULLs in Postgres, so a contact with
// only failures is judged by its last failure, and one with neither
// - which cannot exist after the first outcome, but the column allows
// it - by when it was created.
func (s *Store) DeleteInactiveBefore(ctx context.Context, projID string, before time.Time) (int64, error) {
	res, err := s.Exec(ctx, `
        DELETE FROM contacts
        WHERE project_id = ?
          AND COALESCE(GREATEST(last_sent_at, last_failed_at), created_at) < ?
    `, projID, before)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}
