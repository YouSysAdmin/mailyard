// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package subscriber is the persistence and handler surface for the
// marketing audience: subscriber CRUD plus bulk import. Routes live
// behind requireAuth + requireProject in server/routes.go.
package subscriber

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/database"
	submodel "github.com/yousysadmin/mailyard/internal/models/subscriber"
)

// Store persists the marketing audience. Project scoped: a method
// taking projID answers nothing for a row another project owns.
type Store struct {
	database.Base
}

// NewStore builds the store on db, wiring the shared query helpers
// through database.Base.
func NewStore(db *sql.DB) *Store {
	return &Store{Base: database.NewBase(db)}
}

const subSelect = `
SELECT id, project_id, email, name, status, custom_fields, timezone, language,
       subscribed_at, unsubscribed_at, created_at, updated_at
FROM subscribers`

// Get returns one subscriber within projID, or nil when there is no
// such row.
func (s *Store) Get(ctx context.Context, projID, id string) (*submodel.Subscriber, error) {
	row := s.QueryRow(ctx, subSelect+` WHERE project_id = ? AND id = ?`, projID, id)
	sub, err := scanSubscriber(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return sub, err
}

// GetByEmail returns one subscriber by email within projID, or nil
// when there is no such row.
func (s *Store) GetByEmail(ctx context.Context, projID, email string) (*submodel.Subscriber, error) {
	row := s.QueryRow(ctx, subSelect+` WHERE project_id = ? AND email = ?`, projID, normalizeEmail(email))
	sub, err := scanSubscriber(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return sub, err
}

// List returns subscribers newest first, optionally narrowed by
// status and an email substring.
func (s *Store) List(ctx context.Context, projID, status, query string, limit, offset int) ([]*submodel.Subscriber, error) {
	sqlQuery := subSelect + subscriberScope
	if status != "" {
		sqlQuery += subscriberByStatus
	}

	if query != "" {
		sqlQuery += subscriberByEmail
	}

	args := subscriberFilterArgs(projID, status, query)

	// id in the ORDER BY, because this is OFFSET paging and created_at is
	// not unique - two subscribers imported in the same batch can share
	// it, and with an unstable order a row is free to move across a page
	// boundary between two requests, so it shows twice or not at all.
	sqlQuery += ` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	args = append(args, limit, max(offset, 0))

	rows, err := s.Query(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	return collectSubscribers(rows)
}

// CountMatching counts what List would return over every page.
//
// The list route reported plain Count beside a FILTERED list, so a
// project with five thousand subscribers answered `total: 5000` for a
// search matching one - and the pager built from that total offered
// fifty pages, forty-nine of them empty. Two queries answering two
// different questions under one name.
//
// The predicate is built by the same two helpers List uses, which is the
// point: a filter added to one and not the other is how they drifted.
func (s *Store) CountMatching(ctx context.Context, projID, status, query string) (int, error) {
	sqlQuery := `SELECT COUNT(*) FROM subscribers` + subscriberScope
	if status != "" {
		sqlQuery += subscriberByStatus
	}

	if query != "" {
		sqlQuery += subscriberByEmail
	}

	var n int
	err := s.QueryRow(ctx, sqlQuery, subscriberFilterArgs(projID, status, query)...).Scan(&n)

	return n, err
}

// The filter clauses List and CountMatching share, one constant each.
//
// Constants assembled by the callers rather than a helper returning the
// whole clause: TestNoDynamicSQL cannot follow a string through a
// function value or a call, so a helper made both statements look like
// SQL built from runtime data - and, worse, took them out of the reach of
// the schema and tenancy guards, which evaluate the same way. Same reason
// project.lockOwners is a method and email.addressMatchClause is a const.
const (
	subscriberScope    = ` WHERE project_id = ?`
	subscriberByStatus = ` AND status = ?`

	// Same rule as every other search here - see contact.List. LOWER
	// on the column, since the term is lowercased: without it a
	// subscriber stored as Ann@Example.com never matches "ann".
	subscriberByEmail = ` AND LOWER(email) LIKE ? ESCAPE '\'`
)

func subscriberFilterArgs(projID, status, query string) []any {
	args := []any{projID}
	if status != "" {
		args = append(args, status)
	}

	if query != "" {
		args = append(args, "%"+database.EscapeLike(strings.ToLower(query))+"%")
	}

	return args
}

// ListPage pages through every subscriber of the project in stable
// order - the dynamic-segment evaluator's iterator.
func (s *Store) ListPage(ctx context.Context, projID string, limit, offset int) ([]*submodel.Subscriber, error) {
	rows, err := s.Query(ctx, subSelect+` WHERE project_id = ? ORDER BY created_at ASC, id ASC LIMIT ? OFFSET ?`,
		projID, limit, offset)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	return collectSubscribers(rows)
}

// Count returns how many subscribers projID holds.
func (s *Store) Count(ctx context.Context, projID string) (int, error) {
	var n int
	err := s.QueryRow(ctx, `SELECT COUNT(*) FROM subscribers WHERE project_id = ?`, projID).Scan(&n)

	return n, err
}

// Put upserts by id (callers resolve existing rows by email first, so
// imports stay idempotent and list memberships stable). The
// (project, email) unique index still rejects duplicate addresses.
func (s *Store) Put(ctx context.Context, sub *submodel.Subscriber) error {
	if sub.ID == "" {
		sub.ID = ids.New()
	}

	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = time.Now().UTC()
	}

	if sub.Status == "" {
		sub.Status = submodel.StatusSubscribed
	}

	if sub.Status == submodel.StatusSubscribed && sub.SubscribedAt == nil {
		sub.SubscribedAt = new(sub.CreatedAt)
	}

	sub.Email = normalizeEmail(sub.Email)
	_, err := s.Exec(ctx, `
        INSERT INTO subscribers (
            id, project_id, email, name, status, custom_fields, timezone, language,
            subscribed_at, unsubscribed_at, created_at, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            email           = excluded.email,
            name            = excluded.name,
            status          = excluded.status,
            custom_fields   = excluded.custom_fields,
            timezone        = excluded.timezone,
            language        = excluded.language,
            subscribed_at   = excluded.subscribed_at,
            unsubscribed_at = excluded.unsubscribed_at,
            updated_at      = excluded.updated_at
    `,
		sub.ID, sub.ProjectID, sub.Email, sub.Name, sub.Status,
		database.MustJSON(sub.CustomFields), sub.Timezone, sub.Language,
		database.NullTime(sub.SubscribedAt), database.NullTime(sub.UnsubscribedAt),
		sub.CreatedAt, database.NullTime(sub.UpdatedAt),
	)

	return err
}

// Delete removes one subscriber from projID.
func (s *Store) Delete(ctx context.Context, projID, id string) error {
	_, err := s.Exec(ctx, `DELETE FROM subscribers WHERE project_id = ? AND id = ?`, projID, id)

	return err
}

// SetStatusByEmail flips the lifecycle status (unsubscribe, bounce
// feedback). Returns false when the subscriber does not exist.
func (s *Store) SetStatusByEmail(ctx context.Context, projID, email, status string) (bool, error) {
	now := time.Now().UTC()
	var unsubAt any
	if status == submodel.StatusUnsubscribed {
		unsubAt = now
	}

	res, err := s.Exec(ctx, `
        UPDATE subscribers SET status = ?, unsubscribed_at = ?, updated_at = ?
        WHERE project_id = ? AND email = ?
    `, status, unsubAt, now, projID, normalizeEmail(email))
	if err != nil {
		return false, err
	}

	n, err := res.RowsAffected()

	return n > 0, err
}

func scanSubscriber(r interface{ Scan(...any) error }) (*submodel.Subscriber, error) {
	var sub submodel.Subscriber
	var fields string
	var subscribedAt, unsubscribedAt, updatedAt sql.NullTime
	if err := r.Scan(&sub.ID, &sub.ProjectID, &sub.Email, &sub.Name, &sub.Status,
		&fields, &sub.Timezone, &sub.Language,
		&subscribedAt, &unsubscribedAt, &sub.CreatedAt, &updatedAt); err != nil {
		return nil, err
	}

	database.MustUnmarshalJSON(fields, &sub.CustomFields)
	if subscribedAt.Valid {
		sub.SubscribedAt = new(subscribedAt.Time)
	}

	if unsubscribedAt.Valid {
		sub.UnsubscribedAt = new(unsubscribedAt.Time)
	}

	if updatedAt.Valid {
		sub.UpdatedAt = new(updatedAt.Time)
	}

	return &sub, nil
}

func collectSubscribers(rows *sql.Rows) ([]*submodel.Subscriber, error) {
	var out []*submodel.Subscriber
	for rows.Next() {
		sub, err := scanSubscriber(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, sub)
	}

	return out, rows.Err()
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
