// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package subscriberlist is the persistence and handler surface for
// campaign audiences: static member lists, dynamic filter-rule
// segments, and per-list unsubscribes. Routes live behind requireAuth
// + requireProject, plus subscribe endpoints on the machine API.
package subscriberlist

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	submodel "github.com/yousysadmin/mailyard/internal/models/subscriber"
	slmodel "github.com/yousysadmin/mailyard/internal/models/subscriberlist"
)

// Store persists campaign audiences. Project scoped: a method taking
// projID answers nothing for a row another project owns.
type Store struct {
	database.Base
}

// NewStore builds the store on db, wiring the shared query helpers
// through database.Base.
func NewStore(db *sql.DB) *Store {
	return &Store{Base: database.NewBase(db)}
}

const listSelect = `
SELECT id, project_id, name, description, type, filter_rules, created_at, updated_at
FROM subscriber_lists`

// Get returns one subscriber list within projID, or nil when there is
// no such row.
func (s *Store) Get(ctx context.Context, projID, id string) (*slmodel.List, error) {
	row := s.QueryRow(ctx, listSelect+` WHERE project_id = ? AND id = ?`, projID, id)
	l, err := scanList(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return l, err
}

// List returns every subscriber list in projID.
func (s *Store) List(ctx context.Context, projID string) ([]*slmodel.List, error) {
	rows, err := s.Query(ctx, listSelect+` WHERE project_id = ? ORDER BY name ASC`, projID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*slmodel.List
	for rows.Next() {
		l, err := scanList(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, l)
	}

	return out, rows.Err()
}

// Put inserts the subscriber list, or updates the row when its id
// already exists.
func (s *Store) Put(ctx context.Context, l *slmodel.List) error {
	if l.CreatedAt.IsZero() {
		l.CreatedAt = time.Now().UTC()
	}

	_, err := s.Exec(ctx, `
        INSERT INTO subscriber_lists (id, project_id, name, description, type, filter_rules, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            name         = excluded.name,
            description  = excluded.description,
            type         = excluded.type,
            filter_rules = excluded.filter_rules,
            updated_at   = excluded.updated_at
    `, l.ID, l.ProjectID, l.Name, l.Description, l.Type,
		database.MustJSON(l.FilterRules), l.CreatedAt, database.NullTime(l.UpdatedAt))

	return err
}

// Delete removes one subscriber list from projID.
func (s *Store) Delete(ctx context.Context, projID, id string) error {
	_, err := s.Exec(ctx, `DELETE FROM subscriber_lists WHERE project_id = ? AND id = ?`, projID, id)

	return err
}

// AddMember attaches a subscriber to a static list. The list join
// keeps the write project-scoped.
func (s *Store) AddMember(ctx context.Context, projID, listID, subscriberID string) error {
	res, err := s.Exec(ctx, `
        INSERT INTO subscriber_list_members (id, list_id, subscriber_id, created_at)
        SELECT ?, l.id, sub.id, ?
        FROM subscriber_lists l
        JOIN subscribers sub ON sub.project_id = l.project_id
        WHERE l.project_id = ? AND l.id = ? AND sub.id = ?
        ON CONFLICT(list_id, subscriber_id) DO NOTHING
    `, ids.New(), time.Now().UTC(), projID, listID, subscriberID)
	if err != nil {
		return err
	}

	if n, _ := res.RowsAffected(); n == 0 {
		// Either already a member (fine) or the pair failed the
		// project check. Distinguish so cross-tenant writes 404.
		var count int
		err := s.QueryRow(ctx, `
            SELECT COUNT(*) FROM subscriber_list_members m
            JOIN subscriber_lists l ON l.id = m.list_id
            WHERE l.project_id = ? AND m.list_id = ? AND m.subscriber_id = ?
        `, projID, listID, subscriberID).Scan(&count)
		if err != nil {
			return err
		}

		if count == 0 {
			return sql.ErrNoRows
		}
	}

	return nil
}

// RemoveMember drops a subscriber from a static list. Not an opt-out -
// see Unsubscribe, which records that the person asked.
func (s *Store) RemoveMember(ctx context.Context, projID, listID, subscriberID string) error {
	_, err := s.Exec(ctx, `
        DELETE FROM subscriber_list_members
        WHERE list_id = ? AND subscriber_id = ?
          AND EXISTS (SELECT 1 FROM subscriber_lists l WHERE l.id = ? AND l.project_id = ?)
    `, listID, subscriberID, listID, projID)

	return err
}

// ListMembers pages the static membership with subscriber rows.
func (s *Store) ListMembers(ctx context.Context, projID, listID string, limit, offset int) ([]*submodel.Subscriber, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	rows, err := s.Query(ctx, `
        SELECT sub.id, sub.project_id, sub.email, sub.name, sub.status, sub.custom_fields,
               sub.timezone, sub.language, sub.subscribed_at, sub.unsubscribed_at,
               sub.created_at, sub.updated_at
        FROM subscriber_list_members m
        JOIN subscribers sub ON sub.id = m.subscriber_id
        JOIN subscriber_lists l ON l.id = m.list_id
        WHERE l.project_id = ? AND m.list_id = ?
        ORDER BY m.created_at ASC
        LIMIT ? OFFSET ?
    `, projID, listID, limit, max(offset, 0))
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*submodel.Subscriber
	for rows.Next() {
		sub, err := scanMemberSubscriber(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, sub)
	}

	return out, rows.Err()
}

// CountMembers returns how many members there are.
func (s *Store) CountMembers(ctx context.Context, projID, listID string) (int, error) {
	var n int
	err := s.QueryRow(ctx, `
        SELECT COUNT(*) FROM subscriber_list_members m
        JOIN subscriber_lists l ON l.id = m.list_id
        WHERE l.project_id = ? AND m.list_id = ?
    `, projID, listID).Scan(&n)

	return n, err
}

// Unsubscribe records a per-list opt-out (the subscriber's global
// status is untouched).
func (s *Store) Unsubscribe(ctx context.Context, projID, listID, subscriberID, reason string) error {
	res, err := s.Exec(ctx, `
        INSERT INTO subscriber_list_unsubscribes (id, list_id, subscriber_id, reason, unsubscribed_at)
        SELECT ?, l.id, ?, ?, ?
        FROM subscriber_lists l
        WHERE l.project_id = ? AND l.id = ?
        ON CONFLICT(list_id, subscriber_id) DO UPDATE SET reason = excluded.reason
    `, ids.New(), subscriberID, reason, time.Now().UTC(), projID, listID)
	if err != nil {
		return err
	}

	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// Resubscribe lifts a per-list opt-out.
func (s *Store) Resubscribe(ctx context.Context, projID, listID, subscriberID string) error {
	_, err := s.Exec(ctx, `
        DELETE FROM subscriber_list_unsubscribes
        WHERE list_id = ? AND subscriber_id = ?
          AND EXISTS (SELECT 1 FROM subscriber_lists l WHERE l.id = ? AND l.project_id = ?)
    `, listID, subscriberID, listID, projID)

	return err
}

// UnsubscribedIDs returns the set of subscriber ids opted out of this
// list - consulted by the campaign fan-out.
func (s *Store) UnsubscribedIDs(ctx context.Context, projID, listID string) (map[string]struct{}, error) {
	rows, err := s.Query(ctx, `
        SELECT u.subscriber_id FROM subscriber_list_unsubscribes u
        JOIN subscriber_lists l ON l.id = u.list_id
        WHERE l.project_id = ? AND u.list_id = ?
    `, projID, listID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	out := map[string]struct{}{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}

		out[id] = struct{}{}
	}

	return out, rows.Err()
}

// resolvePageSize bounds how many subscribers the dynamic evaluator
// loads per page.
const resolvePageSize = 500

// ResolveRecipients returns the list's current audience: static
// members, or every project subscriber matching the dynamic rules.
// Only subscribed members are returned, and per-list unsubscribes are
// already removed.
func (s *Store) ResolveRecipients(ctx context.Context, subs store.SubscriberStore, projID string, l *slmodel.List) ([]*submodel.Subscriber, error) {
	optedOut, err := s.UnsubscribedIDs(ctx, projID, l.ID)
	if err != nil {
		return nil, err
	}

	var out []*submodel.Subscriber
	keep := func(sub *submodel.Subscriber) {
		if sub.Status != submodel.StatusSubscribed {
			return
		}

		if _, opted := optedOut[sub.ID]; opted {
			return
		}

		out = append(out, sub)
	}

	if l.Type == slmodel.TypeDynamic {
		for offset := 0; ; offset += resolvePageSize {
			page, err := subs.ListPage(ctx, projID, resolvePageSize, offset)
			if err != nil {
				return nil, err
			}

			for _, sub := range page {
				if slmodel.MatchRules(sub, l.FilterRules) {
					keep(sub)
				}
			}

			if len(page) < resolvePageSize {
				break
			}
		}

		return out, nil
	}

	for offset := 0; ; offset += resolvePageSize {
		page, err := s.ListMembers(ctx, projID, l.ID, resolvePageSize, offset)
		if err != nil {
			return nil, err
		}

		for _, sub := range page {
			keep(sub)
		}

		if len(page) < resolvePageSize {
			break
		}
	}

	return out, nil
}

func scanList(r interface{ Scan(...any) error }) (*slmodel.List, error) {
	var l slmodel.List
	var rules string
	var updated sql.NullTime
	if err := r.Scan(&l.ID, &l.ProjectID, &l.Name, &l.Description, &l.Type,
		&rules, &l.CreatedAt, &updated); err != nil {
		return nil, err
	}

	database.MustUnmarshalJSON(rules, &l.FilterRules)
	if l.FilterRules == nil {
		l.FilterRules = []slmodel.FilterRule{}
	}

	if updated.Valid {
		l.UpdatedAt = new(updated.Time)
	}

	return &l, nil
}

func scanMemberSubscriber(r interface{ Scan(...any) error }) (*submodel.Subscriber, error) {
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
