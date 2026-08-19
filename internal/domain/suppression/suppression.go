// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package suppression is the persistence and handler surface for the
// do-not-send list. The send pipeline consults FilterSuppressed
// before queueing, the worker adds entries on permanent rejections.
package suppression

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/keyset"
	"github.com/yousysadmin/mailyard/internal/core/paging"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	supmodel "github.com/yousysadmin/mailyard/internal/models/suppression"
)

// Store persists the do-not-send list. Project scoped: a method taking
// projID answers nothing for a row another project owns.
type Store struct {
	database.Base
}

// NewStore takes optional read replicas, used by the console list and search
// (database.replica_reads.suppressions). The send-time filters in this
// same file stay on the primary: a stale answer there delivers to an
// address that was just blocked.
//
// Whether they arrive at all is the operator's call - see env.ReplicaReadsConfig.
func NewStore(db *sql.DB, replicas ...*sql.DB) *Store {
	return &Store{Base: database.NewBase(db, replicas...)}
}

const supSelect = `
SELECT id, project_id, email, kind, reason, unsubscribe_list_id, created_at
FROM suppressions`

// List returns one keyset page, newest first.
//
// A keyset page rather than a fixed limit, because this table is
// permanent by design and grows per bounce. On an installation where a few
// percent of a million daily sends bounce, a `LIMIT 500` makes
// everything older than about a day unreachable through any surface,
// and sorting the project's whole suppression set on every page load
// needs the created_at index from migration 00015.
//
// Search matters more than paging here. The question this list exists to
// answer is "why is this one customer not getting our mail", and no
// amount of paging answers that on a table with millions of rows.
func (s *Store) List(ctx context.Context, projID string, f store.SuppressionFilter) ([]*supmodel.Suppression, error) {
	query := supSelect + ` WHERE project_id = ?`
	args := []any{projID}
	if f.Kind != "" {
		query += ` AND kind = ?`
		args = append(args, f.Kind)
	}

	if f.Search != "" {
		// Prefix-anchored, not %term%. The index on
		// (project_id, email) serves a prefix and cannot serve a
		// leading wildcard, and an operator looking somebody up types
		// the start of their address, not the middle of it.
		query += ` AND email LIKE ? ESCAPE '\'`
		args = append(args, database.EscapeLike(strings.ToLower(strings.TrimSpace(f.Search)))+"%")
	}

	// The keyset predicate. Row-value comparison rather than
	// `created_at < ? OR (created_at = ? AND id < ?)`: it is the same
	// condition, it matches the index order directly, and it cannot be
	// got subtly wrong.
	if !f.Cursor.IsZero() {
		query += ` AND (created_at, id) < (?, ?)`
		args = append(args, f.Cursor.CreatedAt.UTC(), f.Cursor.ID)
	}

	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, listLimit(f.Limit))

	rows, err := s.ReadQuery(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	out := []*supmodel.Suppression{} // we need empty slice here
	for rows.Next() {
		sup, err := scanSuppression(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, sup)
	}

	return out, rows.Err()
}

// listLimit is the store's own backstop, for callers that are not
// HTTP handlers. Handlers clamp through paging.From before they get
// here - see the comment on that package about two layers each
// half-responsible for one bound.
func listLimit(n int) int {
	if n < 1 || n > 201 {
		return 51
	}

	return n
}

// Upsert adds or refreshes the block for (project, email).
func (s *Store) Upsert(ctx context.Context, sup *supmodel.Suppression) error {
	if sup.ID == "" {
		sup.ID = ids.New()
	}

	if sup.CreatedAt.IsZero() {
		sup.CreatedAt = time.Now().UTC()
	}

	sup.Email = strings.ToLower(strings.TrimSpace(sup.Email))
	_, err := s.Exec(ctx, `
        INSERT INTO suppressions (id, project_id, email, kind, reason, unsubscribe_list_id, created_at)
        VALUES (?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(project_id, email, unsubscribe_list_id) DO UPDATE SET
            kind   = excluded.kind,
            reason = excluded.reason
    `, sup.ID, sup.ProjectID, sup.Email, sup.Kind, sup.Reason,
		database.NullStr(sup.UnsubscribeListID), sup.CreatedAt)

	return err
}

// Delete removes the global block on an address and nothing else.
//
// The rows are unique on (project, email, list) because those are
// different facts: one says the project may not mail this person at all,
// the others say the person asked to leave a named list. So this names
// the list column too. Leaving it out would make unblocking an address
// destroy every list opt-out it ever made - and pressing Remove on a
// hard bounce that turned out to be a full mailbox is exactly when that
// happens, quietly re-subscribing somebody to everything they left.
//
// A list opt-out is lifted through DeleteForList, which the endpoint
// reaches when the caller names one. Erasure wants every scope and says
// so through PurgeForAddress.
func (s *Store) Delete(ctx context.Context, projID, email string) (bool, error) {
	res, err := s.Exec(ctx, `
        DELETE FROM suppressions
        WHERE project_id = ? AND email = ? AND unsubscribe_list_id IS NULL`,
		projID, strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return false, err
	}

	n, err := res.RowsAffected()

	return n > 0, err
}

// PurgeForAddress removes every suppression row for an address, in
// every scope. This is the erasure path and the one caller that wants
// the old behaviour of Delete: erasing a person removes what we hold
// about them, and a list opt-out is a record about them like any
// other.
func (s *Store) PurgeForAddress(ctx context.Context, projID, email string) (int64, error) {
	res, err := s.Exec(ctx, `DELETE FROM suppressions WHERE project_id = ? AND email = ?`,
		projID, strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

// IsSuppressed reports a global block only. A list-scoped opt-out
// does not stop unrelated mail, so it must not answer true here -
// see IsSuppressedForList.
func (s *Store) IsSuppressed(ctx context.Context, projID, email string) (bool, error) {
	return s.IsSuppressedForList(ctx, projID, email, "")
}

// IsSuppressedForList reports whether the address is blocked for a
// send scoped to listID: either a global block, or an opt-out from
// that specific list. Pass an empty listID for an unscoped send,
// which then only consults global blocks.
func (s *Store) IsSuppressedForList(ctx context.Context, projID, email, listID string) (bool, error) {
	var n int
	// NullStr and a CAST, not the bare string. An unscoped send passes
	// "" here, and "" is not a uuid - Postgres refused the whole
	// statement, which meant every ordinary send answered 500 at the
	// suppression check. The cast is what gives a parameter used only
	// against NULL a type to be deduced from.
	//
	// x = NULL is NULL rather than false, so an empty listID leaves
	// the IS NULL branch as the only one that can match, which is
	// exactly "global blocks only".
	err := s.QueryRow(ctx, `
        SELECT COUNT(*) FROM suppressions
        WHERE project_id = ? AND email = ?
          AND (unsubscribe_list_id IS NULL OR unsubscribe_list_id = ?::uuid)`,
		projID, strings.ToLower(strings.TrimSpace(email)),
		database.NullStr(listID)).Scan(&n)

	return n > 0, err
}

// CountForList reports how many addresses have opted out of one list,
// for display alongside the list.
func (s *Store) CountForList(ctx context.Context, projID, listID string) (int, error) {
	var n int
	err := s.QueryRow(ctx, `
        SELECT COUNT(*) FROM suppressions
        WHERE project_id = ? AND unsubscribe_list_id = ?`, projID, listID).Scan(&n)

	return n, err
}

// DeleteForList removes one address's opt-out from a single list,
// leaving any global block in place.
//
// NullStr, so an empty listID means the global row rather than failing:
// the column is a uuid and `”::uuid` is 22P02, which MalformedID turns
// into a 404 - the caller would read "no such opt-out" for a request
// that never reached the table.
func (s *Store) DeleteForList(ctx context.Context, projID, email, listID string) (bool, error) {
	res, err := s.Exec(ctx, `
        DELETE FROM suppressions
        WHERE project_id = ? AND email = ?
          AND unsubscribe_list_id IS NOT DISTINCT FROM ?::uuid`,
		projID, strings.ToLower(strings.TrimSpace(email)), database.NullStr(listID))
	if err != nil {
		return false, err
	}

	n, err := res.RowsAffected()

	return n > 0, err
}

// FilterSuppressed splits recipients into deliverable and blocked.
// Comparison uses the bare lowercased address, so display-name forms
// still match their suppression rows.
func (s *Store) FilterSuppressed(ctx context.Context, projID string, emails []string) (allowed, blocked []string, err error) {
	return s.FilterSuppressedForList(ctx, projID, "", emails)
}

// FilterSuppressedForList is FilterSuppressed for a send scoped to an
// unsubscribe list: it also drops addresses that opted out of that
// list specifically.
func (s *Store) FilterSuppressedForList(ctx context.Context, projID, listID string, emails []string) (allowed, blocked []string, err error) {
	for _, raw := range emails {
		bare := strings.ToLower(smtpclient.EnvelopeAddress(raw))
		hit, err := s.IsSuppressedForList(ctx, projID, bare, listID)
		if err != nil {
			return nil, nil, err
		}

		if hit {
			blocked = append(blocked, raw)
		} else {
			allowed = append(allowed, raw)
		}
	}

	return allowed, blocked, nil
}

func scanSuppression(r interface{ Scan(...any) error }) (*supmodel.Suppression, error) {
	var sup supmodel.Suppression
	if err := r.Scan(&sup.ID, &sup.ProjectID, &sup.Email, &sup.Kind,
		&sup.Reason, database.Str(&sup.UnsubscribeListID), &sup.CreatedAt); err != nil {
		return nil, err
	}

	return &sup, nil
}

// Handler owns the suppression endpoints (console and machine).
type Handler struct {
	Runtime *env.Runtime
}

// List serves GET /api/v1/suppressions.
func (h *Handler) List(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	kind := c.Query("kind")
	if kind != "" {
		if _, ok := supmodel.ValidKinds[kind]; !ok {
			return response.BadRequest(c, "unknown suppression kind "+kind)
		}
	}

	w := paging.WindowFrom(c)
	rows, err := h.Runtime.Store.Suppression.List(c.Context(), rc.Project.ID, store.SuppressionFilter{
		Kind:   kind,
		Search: paging.Search(c, "search"),
		Limit:  w.Fetch(),
		Cursor: w.Cursor,
	})
	if err != nil {
		return response.Internal(c, err)
	}

	// No total. COUNT(*) on a table that is never pruned - a
	// suppression is permanent by design - is a full index scan per
	// page load, and the number it produces answers nothing an
	// operator does anything with.
	page, more := keyset.Cut(rows, w.Limit)
	next := ""
	if more && len(page) > 0 {
		last := page[len(page)-1]
		next = keyset.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}.Encode()
	}

	return response.Success(c, ListResponse{Suppressions: page, NextCursor: next})
}

// Create serves POST /api/v1/suppressions.
func (h *Handler) Create(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[createInput](c)
	if !ok {
		return resp
	}

	sup := &supmodel.Suppression{
		ProjectID: rc.Project.ID,
		Email:     in.Email,
		Kind:      in.Kind,
		Reason:    in.Reason,
	}
	if sup.Kind == "" {
		sup.Kind = supmodel.KindManual
	}

	if err := h.Runtime.Store.Suppression.Upsert(c.Context(), sup); err != nil {
		return response.Internal(c, err)
	}

	return response.Created(c, CreateResponse{Suppression: sup})
}

// Delete unblocks an address. The email comes as a query param since
// addresses do not belong in path segments.
//
// SCOPED. Without list_id this lifts the global block only, and with it
// exactly that list's opt-out. One call cannot do both, because they are
// not one fact: a project unblocking a mailbox that bounced is saying
// nothing about the lists that mailbox chose to leave.
func (h *Handler) Delete(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	email := c.Query("email")
	if email == "" {
		return response.BadRequest(c, "email query parameter is required")
	}

	del := h.Runtime.Store.Suppression.Delete
	if listID := c.Query("list_id"); listID != "" {
		del = func(ctx context.Context, projID, email string) (bool, error) {
			return h.Runtime.Store.Suppression.DeleteForList(ctx, projID, email, listID)
		}
	}

	ok, err := del(c.Context(), rc.Project.ID, email)
	if err != nil {
		return response.Internal(c, err)
	}

	if !ok {
		return response.NotFound(c, "no suppression for this email in that scope")
	}

	return response.NoContent(c)
}
