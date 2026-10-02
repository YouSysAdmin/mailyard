// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package sender is the persistence and handler surface for approved
// sender addresses. Creation is gated on the address domain being
// verified (see internal/domain/domains) by the same project.
package sender

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/domain"
	smodel "github.com/yousysadmin/mailyard/internal/models/sender"
)

// Store persists approved sender addresses and their signing keys.
// Project scoped: a method taking projID answers nothing for a row
// another project owns.
type Store struct {
	database.Base
	crypto *crypto.Service
}

// NewStore builds the store on db, wiring the shared query helpers
// through database.Base. cr seals the signing keys' private halves.
func NewStore(db *sql.DB, cr *crypto.Service) *Store {
	return &Store{Base: database.NewBase(db), crypto: cr}
}

// senderSelect reads a sender with the DESCRIPTION of its signing key,
// never the key material: the list is what every From picker loads,
// and the private half is read only by the delivery path through
// GetSigning.
const senderSelect = `
SELECT s.id, s.project_id, s.created_by, s.email, s.name, s.created_at,
       k.kind, k.fingerprint, k.algorithm, k.subject, k.issuer, k.not_after,
       k.sign, k.attach_key, k.created_at, k.updated_at
FROM senders s
LEFT JOIN sender_signing_keys k ON k.sender_id = s.id`

// Get returns one sender address within projID, or nil when there is
// no such row.
func (s *Store) Get(ctx context.Context, projID, id string) (*smodel.Sender, error) {
	row := s.QueryRow(ctx, senderSelect+` WHERE s.project_id = ? AND s.id = ?`, projID, id)
	m, err := scanSender(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return m, err
}

// GetByEmail returns one sender address by email within projID, or nil
// when there is no such row.
func (s *Store) GetByEmail(ctx context.Context, projID, email string) (*smodel.Sender, error) {
	row := s.QueryRow(ctx, senderSelect+` WHERE s.project_id = ? AND s.email = ?`, projID, strings.ToLower(email))
	m, err := scanSender(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return m, err
}

// List returns every sender address in projID.
func (s *Store) List(ctx context.Context, projID string) ([]*smodel.Sender, error) {
	rows, err := s.Query(ctx, senderSelect+` WHERE s.project_id = ? ORDER BY s.email ASC`, projID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*smodel.Sender
	for rows.Next() {
		m, err := scanSender(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, m)
	}

	return out, rows.Err()
}

// Put inserts the sender address, or updates the row when its id
// already exists.
func (s *Store) Put(ctx context.Context, m *smodel.Sender) error {
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}

	_, err := s.Exec(ctx, `
        INSERT INTO senders (id, project_id, created_by, email, name, created_at)
        VALUES (?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET name = excluded.name
    `, m.ID, m.ProjectID, m.CreatedBy, strings.ToLower(m.Email), m.Name, m.CreatedAt)

	return err
}

// Delete removes one sender address from projID.
func (s *Store) Delete(ctx context.Context, projID, id string) error {
	_, err := s.Exec(ctx, `DELETE FROM senders WHERE project_id = ? AND id = ?`, projID, id)

	return err
}

func scanSender(r interface{ Scan(...any) error }) (*smodel.Sender, error) {
	var m smodel.Sender
	var k smodel.SigningKey
	var kind, fingerprint, algorithm, subject, issuer sql.NullString
	var notAfter, kCreated, kUpdated sql.NullTime
	var sign, attach sql.NullBool
	if err := r.Scan(&m.ID, &m.ProjectID, &m.CreatedBy, &m.Email, &m.Name, &m.CreatedAt,
		&kind, &fingerprint, &algorithm, &subject, &issuer, &notAfter,
		&sign, &attach, &kCreated, &kUpdated); err != nil {
		return nil, err
	}

	if kind.Valid {
		k.SenderID, k.ProjectID = m.ID, m.ProjectID
		k.Kind, k.Fingerprint, k.Algorithm = kind.String, fingerprint.String, algorithm.String
		k.Subject, k.Issuer = subject.String, issuer.String
		k.Sign, k.AttachKey = sign.Bool, attach.Bool
		k.CreatedAt, k.UpdatedAt = kCreated.Time, kUpdated.Time
		if notAfter.Valid {
			k.NotAfter = new(notAfter.Time)
		}

		m.Signing = &k
	}

	return &m, nil
}

// signingSelect reads the whole key, private half included.
const signingSelect = `
SELECT sender_id, project_id, kind, private_key, public_key, fingerprint, algorithm, subject, issuer,
       not_after, sign, attach_key, created_at, updated_at
FROM sender_signing_keys`

// GetSigning returns the sender's key with its material, or nil when
// the sender has none. The one reader of the private half.
func (s *Store) GetSigning(ctx context.Context, projID, senderID string) (*smodel.SigningKey, error) {
	row := s.QueryRow(ctx, signingSelect+` WHERE project_id = ? AND sender_id = ?`, projID, senderID)
	k, err := s.scanSigning(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return k, err
}

// PutSigning stores the key, replacing whatever the sender had. The
// handler decides whether replacing is allowed, this just writes.
func (s *Store) PutSigning(ctx context.Context, k *smodel.SigningKey) error {
	now := time.Now().UTC()
	if k.CreatedAt.IsZero() {
		k.CreatedAt = now
	}

	k.UpdatedAt = now
	sealed, err := s.seal(k.PrivateKey)
	if err != nil {
		return err
	}

	_, err = s.Exec(ctx, `
        INSERT INTO sender_signing_keys (sender_id, project_id, kind, private_key, public_key, fingerprint,
                                         algorithm, subject, issuer, not_after, sign, attach_key, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT (sender_id) DO UPDATE SET
            kind = excluded.kind, private_key = excluded.private_key, public_key = excluded.public_key,
            fingerprint = excluded.fingerprint, algorithm = excluded.algorithm, subject = excluded.subject,
            issuer = excluded.issuer, not_after = excluded.not_after, sign = excluded.sign,
            attach_key = excluded.attach_key, created_at = excluded.created_at, updated_at = excluded.updated_at
    `, k.SenderID, k.ProjectID, k.Kind, sealed, k.PublicKey, k.Fingerprint,
		k.Algorithm, k.Subject, k.Issuer, database.NullTime(k.NotAfter), k.Sign, k.AttachKey, k.CreatedAt, k.UpdatedAt)

	return err
}

// SetSigningFlags updates the two switches and nothing else, so a
// toggle never rewrites key material.
func (s *Store) SetSigningFlags(ctx context.Context, projID, senderID string, sign, attachKey bool) error {
	_, err := s.Exec(ctx, `
        UPDATE sender_signing_keys SET sign = ?, attach_key = ?, updated_at = ?
        WHERE project_id = ? AND sender_id = ?`, sign, attachKey, time.Now().UTC(), projID, senderID)

	return err
}

// DeleteSigning removes the sender's key.
func (s *Store) DeleteSigning(ctx context.Context, projID, senderID string) error {
	_, err := s.Exec(ctx, `DELETE FROM sender_signing_keys WHERE project_id = ? AND sender_id = ?`, projID, senderID)

	return err
}

// SigningExpiringBefore lists every key, across projects, that runs
// out before t - the expiry sweep's question, which is why it is not
// project scoped. Without the material.
func (s *Store) SigningExpiringBefore(ctx context.Context, t time.Time) ([]*smodel.SigningKey, error) {
	rows, err := s.Query(ctx, `
        SELECT k.sender_id, k.project_id, s.email, k.kind, k.fingerprint, k.not_after, k.sign
        FROM sender_signing_keys k
        JOIN senders s ON s.id = k.sender_id
        WHERE k.not_after IS NOT NULL AND k.not_after < ?
        ORDER BY k.not_after ASC`, t)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*smodel.SigningKey
	for rows.Next() {
		var k smodel.SigningKey
		var notAfter sql.NullTime
		if err := rows.Scan(&k.SenderID, &k.ProjectID, &k.SenderEmail, &k.Kind, &k.Fingerprint, &notAfter, &k.Sign); err != nil {
			return nil, err
		}

		if notAfter.Valid {
			k.NotAfter = new(notAfter.Time)
		}

		out = append(out, &k)
	}

	return out, rows.Err()
}

func (s *Store) scanSigning(r interface{ Scan(...any) error }) (*smodel.SigningKey, error) {
	var k smodel.SigningKey
	var sealed string
	var notAfter sql.NullTime
	if err := r.Scan(&k.SenderID, &k.ProjectID, &k.Kind, &sealed, &k.PublicKey, &k.Fingerprint,
		&k.Algorithm, &k.Subject, &k.Issuer, &notAfter, &k.Sign, &k.AttachKey, &k.CreatedAt, &k.UpdatedAt); err != nil {
		return nil, err
	}

	if notAfter.Valid {
		k.NotAfter = new(notAfter.Time)
	}

	plain, err := s.unseal(k.SenderID, sealed)
	if err != nil {
		return nil, err
	}

	k.PrivateKey = plain

	return &k, nil
}

// seal encrypts a private key for the row. Empty stays empty, so a
// row with no material is told apart from the encryption of "".
func (s *Store) seal(key string) (string, error) {
	if key == "" {
		return "", nil
	}

	sealed, err := s.crypto.Encrypt(key)
	if err != nil {
		return "", fmt.Errorf("senders: seal signing key: %w", err)
	}

	return sealed, nil
}

// unseal is the reverse, loud on failure: a key read back empty would
// send mail unsigned, which is the failure hardest to notice.
func (s *Store) unseal(senderID, sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}

	plain, err := s.crypto.Decrypt(sealed)
	if err != nil {
		return "", fmt.Errorf("senders: unseal signing key for %s: %w", senderID, err)
	}

	return plain, nil
}

// Handler owns the /api/senders surface.
type Handler struct {
	Runtime *env.Runtime
}

// List serves GET /api/v1/senders.
func (h *Handler) List(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	out, err := h.Runtime.Store.Sender.List(c.Context(), rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if out == nil {
		out = []*smodel.Sender{}
	}

	return response.Success(c, ListResponse{Senders: out})
}

// Create registers an address after checking the domain is verified
// by this project - the point of the entity is that From selectors
// only ever offer addresses the operator can legitimately use.
func (h *Handler) Create(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[createInput](c)
	if !ok {
		return resp
	}

	_, domainName, _ := strings.CutLast(in.Email, "@")
	d, err := h.Runtime.Store.Domain.GetVerifiedCoveringFor(c.Context(), domainName, rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if d == nil {
		return response.BadRequest(c,
			"domain "+domainName+" is not verified by this project, verify it under Domains first")
	}

	existing, err := h.Runtime.Store.Sender.GetByEmail(c.Context(), rc.Project.ID, in.Email)
	if err != nil {
		return response.Internal(c, err)
	}

	if existing != nil {
		return response.Conflict(c, "this sender address is already registered")
	}

	m := &smodel.Sender{
		ID:        ids.New(),
		ProjectID: rc.Project.ID,
		CreatedBy: userID(rc),
		Email:     in.Email,
		Name:      in.Name,
	}
	if err := h.Runtime.Store.Sender.Put(c.Context(), m); err != nil {
		return response.Internal(c, err)
	}

	return response.Created(c, SenderResponse{Sender: m})
}

// Get serves GET /api/v1/senders/:id.
func (h *Handler) Get(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	m, err := h.Runtime.Store.Sender.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if m == nil {
		return response.NotFound(c, "sender not found")
	}

	return response.Success(c, SenderResponse{Sender: m})
}

// Update serves PATCH /api/v1/senders/:id: the display name, which is
// what a bare From gets on the server. An empty name clears it.
func (h *Handler) Update(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[updateInput](c)
	if !ok {
		return resp
	}

	m, err := h.Runtime.Store.Sender.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if m == nil {
		return response.NotFound(c, "sender not found")
	}

	if in.Name != nil {
		m.Name = strings.TrimSpace(*in.Name)
	}

	if err := h.Runtime.Store.Sender.Put(c.Context(), m); err != nil {
		return response.Internal(c, err)
	}

	return h.answerSender(c, rc.Project.ID, m.ID, response.Success)
}

// Delete serves DELETE /api/v1/senders/:id.
func (h *Handler) Delete(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	m, err := h.Runtime.Store.Sender.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if m == nil {
		return response.NotFound(c, "sender not found")
	}

	if err := h.Runtime.Store.Sender.Delete(c.Context(), rc.Project.ID, m.ID); err != nil {
		return response.Internal(c, err)
	}

	return response.NoContent(c)
}

func userID(rc *domain.RequestContext) string {
	if rc.User != nil {
		return rc.User.ID
	}

	return ""
}
