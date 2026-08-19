// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package bounce is the persistence and handler surface for delivery
// failure records, including the provider ingest webhook on the
// machine surface (POST /api/v1/webhooks/bounce).
package bounce

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
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	bmodel "github.com/yousysadmin/mailyard/internal/models/bounce"
	supmodel "github.com/yousysadmin/mailyard/internal/models/suppression"
)

// Store persists delivery failure records. Project scoped: a method
// taking projID answers nothing for a row another project owns.
type Store struct {
	database.Base
}

// NewStore takes optional read replicas, used by the bounce list (database.replica_reads.bounces).
// The has-this-address-bounced lookup stays on the primary.
//
// Whether they arrive at all is the operator's call - see env.ReplicaReadsConfig.
func NewStore(db *sql.DB, replicas ...*sql.DB) *Store {
	return &Store{Base: database.NewBase(db, replicas...)}
}

// Put inserts the bounce record, or updates the row when its id
// already exists.
func (s *Store) Put(ctx context.Context, b *bmodel.Bounce) error {
	if b.ID == "" {
		b.ID = ids.New()
	}

	if b.CreatedAt.IsZero() {
		b.CreatedAt = time.Now().UTC()
	}

	_, err := s.Exec(ctx, `
        INSERT INTO bounces (id, project_id, email_id, recipient, type, reason, created_at)
        VALUES (?, ?, ?, ?, ?, ?, ?)
    `, b.ID, b.ProjectID, database.NullStr(b.EmailID), b.Recipient, b.Type,
		b.Reason, b.CreatedAt)

	return err
}

const bounceSelect = `
SELECT id, project_id, email_id, recipient, type, reason, created_at
FROM bounces`

// List returns one keyset page, newest first.
//
// Same change and the same reason as the suppression list: it was a
// bare LIMIT 500 with no paging and no search, and this table grows
// per message. A few percent of a large send is still tens of
// thousands of rows a day, so the five hundredth row was under an
// hour old.
func (s *Store) List(ctx context.Context, projID string, f store.BounceFilter) ([]*bmodel.Bounce, error) {
	query := bounceSelect + ` WHERE project_id = ?`
	args := []any{projID}
	if f.Type != "" {
		query += ` AND type = ?`
		args = append(args, f.Type)
	}

	if f.Search != "" {
		// LOWER(recipient), matching HasHardBounce and DeleteByEmail.
		// recipient is stored exactly as the report named it, and the
		// term is lowercased here - so `recipient LIKE 'bob@%'` missed
		// every report about `Bob@x.test`, and the operator searching for
		// the address they were shown got an empty page. Which is the one
		// answer this list must never give wrongly: it reads as "that
		// address has never bounced".
		query += ` AND LOWER(recipient) LIKE ? ESCAPE '\'`
		args = append(args, database.EscapeLike(strings.ToLower(strings.TrimSpace(f.Search)))+"%")
	}

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
	out := []*bmodel.Bounce{} // we need empty slice here
	for rows.Next() {
		var b bmodel.Bounce
		if err := rows.Scan(&b.ID, &b.ProjectID, database.Str(&b.EmailID), &b.Recipient,
			&b.Type, &b.Reason, &b.CreatedAt); err != nil {
			return nil, err
		}

		out = append(out, &b)
	}

	return out, rows.Err()
}

// listLimit is the store's own backstop for non-HTTP callers.
// Handlers clamp through paging.From before they reach here.
func listLimit(n int) int {
	if n < 1 || n > 201 {
		return 51
	}

	return n
}

// Handler owns the bounce endpoints.
type Handler struct {
	Runtime *env.Runtime
}

// List returns one keyset page of bounces.
//
// No total. COUNT(*) on a table that gains a row per failed message
// is a full index scan per page load, to render a number nobody acts
// on. next_cursor being present is the honest answer to "is there
// more", and it costs one extra row.
func (h *Handler) List(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	w := paging.WindowFrom(c)
	rows, err := h.Runtime.Store.Bounce.List(c.Context(), rc.Project.ID, store.BounceFilter{
		Type:   c.Query("type"),
		Search: paging.Search(c, "search"),
		Limit:  w.Fetch(),
		Cursor: w.Cursor,
	})
	if err != nil {
		return response.Internal(c, err)
	}

	page, more := keyset.Cut(rows, w.Limit)
	next := ""
	if more && len(page) > 0 {
		last := page[len(page)-1]
		next = keyset.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}.Encode()
	}

	return response.Success(c, ListResponse{Bounces: page, NextCursor: next})
}

// Delete removes every bounce report for one address.
//
// The case: a customer list where some mailboxes do not exist yet.
// They bounce, they are created a week later, and HasHardBounce keeps
// reporting the address as previously bounced.
//
// It does not unblock the address. Delivery is stopped by the
// SUPPRESSION the intake wrote, removed through DELETE /suppressions -
// doing both here would let a caller holding only bounces:delete put
// an address back into circulation. The console makes both calls and
// says so in the confirmation.
//
// By address and not row id, like DELETE /suppressions: the question
// is about a person, not about one report.
func (h *Handler) Delete(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	email := c.Query("email")
	if email == "" {
		return response.BadRequest(c, "email query parameter is required")
	}

	n, err := h.Runtime.Store.Bounce.DeleteByEmail(c.Context(), rc.Project.ID, email)
	if err != nil {
		return response.Internal(c, err)
	}

	if n == 0 {
		return response.NotFound(c, "no bounce reports for this email")
	}

	return response.Success(c, DeleteResponse{Deleted: n})
}

// Ingest records a reported bounce. Hard bounces and complaints
// auto-suppress the recipient (soft ones do not).
func (h *Handler) Ingest(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[ingestInput](c)
	if !ok {
		return resp
	}

	b := &bmodel.Bounce{
		ProjectID: rc.Project.ID,
		EmailID:   in.EmailID,
		Recipient: in.Recipient,
		Type:      in.Type,
		Reason:    in.Reason,
	}
	if b.Type == "" {
		b.Type = bmodel.TypeHard
	}

	if err := h.Runtime.Store.Bounce.Put(c.Context(), b); err != nil {
		return response.Internal(c, err)
	}

	suppressed := false
	if b.Type == bmodel.TypeHard || b.Type == bmodel.TypeComplaint {
		kind := supmodel.KindBounce
		if b.Type == bmodel.TypeComplaint {
			kind = supmodel.KindComplaint
		}

		if err := h.Runtime.Store.Suppression.Upsert(c.Context(), &supmodel.Suppression{
			ProjectID: rc.Project.ID,
			Email:     in.Recipient,
			Kind:      kind,
			Reason:    in.Reason,
		}); err != nil {
			return response.Internal(c, err)
		}

		suppressed = true
	}

	return response.Created(c, IngestResponse{Bounce: b, Suppressed: suppressed})
}

// DeleteByEmail removes every report for one recipient in this project.
//
// LOWER(recipient) matches HasHardBounce, which is the read this delete
// exists to change: an address matched in one case and not the other
// would leave the verifier calling a mailbox bounced with no row on the
// page to explain why.
//
// The primary, not a replica - it is a write, and the console reads the
// list back off a follower.
func (s *Store) DeleteByEmail(ctx context.Context, projID, email string) (int, error) {
	res, err := s.Exec(ctx, `
        DELETE FROM bounces
        WHERE project_id = ? AND LOWER(recipient) = ?`,
		projID, strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return 0, err
	}

	n, err := res.RowsAffected()

	return int(n), err
}

// HasHardBounce reports whether the address has ever permanently
// bounced for this project. Used by the verifier: your own
// delivery history is stronger evidence than any DNS lookup.
func (s *Store) HasHardBounce(ctx context.Context, projID, email string) (bool, error) {
	var n int
	err := s.QueryRow(ctx, `
        SELECT COUNT(*) FROM bounces
        WHERE project_id = ? AND LOWER(recipient) = ? AND type = ?`,
		projID, strings.ToLower(strings.TrimSpace(email)), bmodel.TypeHard).Scan(&n)

	return n > 0, err
}
