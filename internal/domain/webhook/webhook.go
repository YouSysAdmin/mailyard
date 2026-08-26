// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package webhook is the persistence and handler surface for outgoing
// event webhooks and their delivery log. The store doubles as the
// dispatcher's Sink (core/dispatch).
package webhook

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/url"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/keyset"
	"github.com/yousysadmin/mailyard/internal/core/paging"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/safedial"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/domain"
	whmodel "github.com/yousysadmin/mailyard/internal/models/webhook"
)

// Store persists outgoing webhooks and their delivery log. Project
// scoped: a method taking projID answers nothing for a row another
// project owns.
type Store struct {
	database.Base
	crypto *crypto.Service
}

// NewStore takes the at-rest crypto service and optional read replicas,
// the latter used by the delivery history
// (database.replica_reads.webhook_deliveries). The webhook definitions
// stay on the primary - the console edits them, so a read follows a
// save.
//
// Whether the replicas arrive at all is the operator's call - see
// env.ReplicaReadsConfig.
//
// The crypto service seals the signing secret, as it does the SMTP
// password beside it. The secret cannot be hashed - the dispatcher signs
// each payload with the same value the receiver verifies with - so
// without sealing, a database dump carries every project's HMAC key and
// the ability to forge X-Mailyard-Signature against their endpoint.
//
// No tolerant read: a "decrypt, else assume plaintext" fallback is the
// ambiguity crypto's own doc comment rules out. A column holds
// ciphertext or holds something wrong.
func NewStore(db *sql.DB, cr *crypto.Service, replicas ...*sql.DB) *Store {
	return &Store{Base: database.NewBase(db, replicas...), crypto: cr}
}

const hookSelect = `
SELECT id, project_id, created_by, url, events, filters, secret, created_at,
       disabled_at, disabled_reason
FROM webhooks`

// Get returns one webhook within projID, or nil when there is no such
// row.
func (s *Store) Get(ctx context.Context, projID, id string) (*whmodel.Webhook, error) {
	row := s.QueryRow(ctx, hookSelect+` WHERE project_id = ? AND id = ?`, projID, id)
	h, err := s.scanWebhook(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return h, err
}

// List returns every webhook in projID.
func (s *Store) List(ctx context.Context, projID string) ([]*whmodel.Webhook, error) {
	rows, err := s.Query(ctx, hookSelect+` WHERE project_id = ? ORDER BY created_at ASC`, projID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*whmodel.Webhook
	for rows.Next() {
		h, err := s.scanWebhook(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, h)
	}

	return out, rows.Err()
}

// Put inserts the webhook, or updates the row when its id already
// exists.
func (s *Store) Put(ctx context.Context, h *whmodel.Webhook) error {
	if h.CreatedAt.IsZero() {
		h.CreatedAt = time.Now().UTC()
	}

	// Sealed on the way in. The object keeps its plaintext secret, so a
	// create still returns the value once - the column is what changes.
	stored, err := s.crypto.Encrypt(h.Secret)
	if err != nil {
		return err
	}

	// secret is NOT in the DO UPDATE list, and that is deliberate rather
	// than an omission: an edit of a url or an event list must not
	// rotate the key a receiver is already verifying with.
	_, err = s.Exec(ctx, `
        INSERT INTO webhooks (id, project_id, created_by, url, events, filters, secret, created_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            url     = excluded.url,
            events  = excluded.events,
            filters = excluded.filters
    `, h.ID, h.ProjectID, h.CreatedBy, h.URL,
		database.MustJSON(h.Events), database.MustJSON(h.Filters), stored, h.CreatedAt)

	return err
}

// Disable takes a hook out of rotation with the reason the dispatcher
// gave up. Scoped by project like every other write, though the
// dispatcher already holds the row - a caller handing it a foreign hook
// must still change nothing.
func (s *Store) Disable(ctx context.Context, projID, id, reason string) error {
	_, err := s.Exec(ctx, `
        UPDATE webhooks SET disabled_at = now(), disabled_reason = ?
        WHERE project_id = ? AND id = ? AND disabled_at IS NULL
    `, reason, projID, id)

	return err
}

// Enable puts a disabled hook back. Reports whether a row changed, so
// the handler can tell an unknown id from one already enabled.
func (s *Store) Enable(ctx context.Context, projID, id string) (bool, error) {
	res, err := s.Exec(ctx, `
        UPDATE webhooks SET disabled_at = NULL, disabled_reason = ''
        WHERE project_id = ? AND id = ?
    `, projID, id)
	if err != nil {
		return false, err
	}

	n, err := res.RowsAffected()

	return n > 0, err
}

// RotateSecret replaces the signing secret of one webhook within
// projID, reporting whether the id existed. The plaintext is the
// caller's to return once - the column holds it sealed.
func (s *Store) RotateSecret(ctx context.Context, projID, id, secret string) (bool, error) {
	stored, err := s.crypto.Encrypt(secret)
	if err != nil {
		return false, err
	}

	res, err := s.Exec(ctx, `UPDATE webhooks SET secret = ? WHERE project_id = ? AND id = ?`, stored, projID, id)
	if err != nil {
		return false, err
	}

	n, err := res.RowsAffected()

	return n > 0, err
}

// Delete removes one webhook from projID.
func (s *Store) Delete(ctx context.Context, projID, id string) error {
	_, err := s.Exec(ctx, `DELETE FROM webhooks WHERE project_id = ? AND id = ?`, projID, id)

	return err
}

// RecordDelivery appends one attempt to the log (dispatcher side).
func (s *Store) RecordDelivery(ctx context.Context, d *whmodel.Delivery) error {
	if d.ID == "" {
		d.ID = ids.New()
	}

	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now().UTC()
	}

	_, err := s.Exec(ctx, `
        INSERT INTO webhook_deliveries (
            id, webhook_id, project_id, event, status, http_status,
            error_message, attempt, created_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, d.ID, d.WebhookID, d.ProjectID, d.Event, d.Status, d.HTTPStatus,
		d.ErrorMessage, d.Attempt, d.CreatedAt)

	return err
}

const deliverySelect = `
SELECT id, webhook_id, project_id, event, status, http_status,
       error_message, attempt, created_at
FROM webhook_deliveries`

// ListDeliveries returns one keyset page, newest first.
//
// This is a per-message table too: a project with email.sent
// subscribed writes a delivery row for every message it sends, plus
// one per retry. A bare LIMIT 200 covers a couple of minutes on a busy
// install, which makes the page useless for the one thing it is for:
// finding the delivery that failed.
//
// It does have a retention window, unlike suppressions, so it is
// bounded in time. Bounded in time is not the same as small.
func (s *Store) ListDeliveries(ctx context.Context, projID, webhookID string, limit int, cur keyset.Cursor) ([]*whmodel.Delivery, error) {
	query := deliverySelect + ` WHERE project_id = ? AND webhook_id = ?`
	args := []any{projID, webhookID}
	if !cur.IsZero() {
		query += ` AND (created_at, id) < (?, ?)`
		args = append(args, cur.CreatedAt.UTC(), cur.ID)
	}

	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	if limit < 1 || limit > 201 {
		limit = 51
	}

	args = append(args, limit)

	rows, err := s.ReadQuery(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*whmodel.Delivery
	for rows.Next() {
		var d whmodel.Delivery
		if err := rows.Scan(&d.ID, &d.WebhookID, &d.ProjectID, &d.Event, &d.Status,
			&d.HTTPStatus, &d.ErrorMessage, &d.Attempt, &d.CreatedAt); err != nil {
			return nil, err
		}

		out = append(out, &d)
	}

	return out, rows.Err()
}

// scanWebhook is a method because it unseals the signing secret, which
// needs the store's crypto service. Same shape as the smtp server scan
// for the same reason.
func (s *Store) scanWebhook(r interface{ Scan(...any) error }) (*whmodel.Webhook, error) {
	var h whmodel.Webhook
	var events, filters string
	var disabledAt sql.NullTime
	if err := r.Scan(&h.ID, &h.ProjectID, &h.CreatedBy, &h.URL,
		&events, &filters, &h.Secret, &h.CreatedAt, &disabledAt, &h.DisabledReason); err != nil {
		return nil, err
	}

	if disabledAt.Valid {
		h.DisabledAt = &disabledAt.Time
	}

	database.MustUnmarshalJSON(events, &h.Events)
	database.MustUnmarshalJSON(filters, &h.Filters)

	// Unsealed here rather than at each caller, so the dispatcher signs
	// with the same bytes the receiver was given and no read path can
	// forget to unwrap.
	plain, err := s.crypto.Decrypt(h.Secret)
	if err != nil {
		return nil, err
	}

	h.Secret = plain

	return &h, nil
}

// Handler owns the webhook endpoints (console and machine).
type Handler struct {
	Runtime *env.Runtime
}

// List serves GET /api/v1/webhooks.
func (h *Handler) List(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	hooks, err := h.Runtime.Store.Webhook.List(c.Context(), rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if hooks == nil {
		hooks = []*whmodel.Webhook{}
	}

	return response.Success(c, ListResponse{Webhooks: hooks})
}

// Create registers a webhook. The signing secret is generated
// server-side and returned EXACTLY ONCE.
func (h *Handler) Create(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[createInput](c)
	if !ok {
		return resp
	}

	for _, e := range in.Events {
		if _, ok := whmodel.ValidEvents[e]; !ok && e != "*" {
			return response.BadRequest(c, "unknown event "+e)
		}
	}

	// Reject a private destination here so the operator finds out now
	// instead of watching every delivery fail. This is a courtesy
	// check, not the control: the dialer in internal/core/safedial is
	// what actually enforces it, because a name can resolve
	// differently between this moment and the first delivery.
	if !h.Runtime.Config.Webhook.AllowPrivateTargets {
		u, perr := url.Parse(in.URL)
		if perr != nil || u.Hostname() == "" {
			return response.BadRequest(c, "url is not a valid absolute http(s) url")
		}

		if !safedial.HostAllowed(c.Context(), u.Hostname()) {
			return response.BadRequest(c,
				"webhook targets a private or reserved address, which is refused (set webhook.allow_private_targets to permit it)")
		}
	}

	secret, err := newSecret()
	if err != nil {
		return response.Internal(c, err)
	}

	createdBy := ""
	if rc.User != nil {
		createdBy = rc.User.ID
	}

	hook := &whmodel.Webhook{
		ID:        ids.New(),
		ProjectID: rc.Project.ID,
		CreatedBy: createdBy,
		URL:       in.URL,
		Events:    in.Events,
		Filters:   in.Filters,
		Secret:    secret,
	}
	if hook.Filters == nil {
		hook.Filters = []string{}
	}

	if err := h.Runtime.Store.Webhook.Put(c.Context(), hook); err != nil {
		return response.Internal(c, err)
	}

	return response.Created(c, CreateResponse{Webhook: hook, Secret: secret})
}

// newSecret mints a signing secret: 256 bits, hex.
func newSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}

	return hex.EncodeToString(raw), nil
}

// RotateSecret serves POST /api/v1/webhooks/:id/rotate-secret: a fresh
// signing secret, returned once like the one Create returned. The old
// secret stops verifying at once - a receiver switches by verifying
// against both for the length of the changeover.
func (h *Handler) RotateSecret(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	hook, err := h.Runtime.Store.Webhook.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if hook == nil {
		return response.NotFound(c, "webhook not found")
	}

	secret, err := newSecret()
	if err != nil {
		return response.Internal(c, err)
	}

	ok, err := h.Runtime.Store.Webhook.RotateSecret(c.Context(), rc.Project.ID, hook.ID, secret)
	if err != nil {
		return response.Internal(c, err)
	}

	if !ok {
		return response.NotFound(c, "webhook not found")
	}

	hook.Secret = secret

	return response.Success(c, CreateResponse{Webhook: hook, Secret: secret})
}

// Delete serves DELETE /api/v1/webhooks/:id.
func (h *Handler) Delete(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	hook, err := h.Runtime.Store.Webhook.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if hook == nil {
		return response.NotFound(c, "webhook not found")
	}

	if err := h.Runtime.Store.Webhook.Delete(c.Context(), rc.Project.ID, hook.ID); err != nil {
		return response.Internal(c, err)
	}

	return response.NoContent(c)
}

// Enable serves POST /api/v1/webhooks/:id/enable: puts a hook the
// dispatcher disabled back into rotation. Idempotent on an enabled one.
func (h *Handler) Enable(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	ok, err := h.Runtime.Store.Webhook.Enable(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if !ok {
		return response.NotFound(c, "webhook not found")
	}

	hook, err := h.Runtime.Store.Webhook.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, EnableResponse{Webhook: hook})
}

// Deliveries serves GET /api/v1/webhooks/:id/deliveries.
func (h *Handler) Deliveries(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	hook, err := h.Runtime.Store.Webhook.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if hook == nil {
		return response.NotFound(c, "webhook not found")
	}

	w := paging.WindowFrom(c)
	dels, err := h.Runtime.Store.Webhook.ListDeliveries(c.Context(), rc.Project.ID, hook.ID, w.Fetch(), w.Cursor)
	if err != nil {
		return response.Internal(c, err)
	}

	page, more := keyset.Cut(dels, w.Limit)
	next := ""
	if more && len(page) > 0 {
		last := page[len(page)-1]
		next = keyset.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}.Encode()
	}

	if page == nil {
		page = []*whmodel.Delivery{}
	}

	return response.Success(c, DeliveriesResponse{Deliveries: page, NextCursor: next})
}
