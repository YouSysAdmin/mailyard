// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package sender is the persistence and handler surface for approved
// sender addresses. Creation is gated on the address domain being
// verified (see internal/domain/domains) by the same project.
package sender

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/domain"
	smodel "github.com/yousysadmin/mailyard/internal/models/sender"
)

// Store persists approved sender addresses. Project scoped: a method
// taking projID answers nothing for a row another project owns.
type Store struct {
	database.Base
}

// NewStore builds the store on db, wiring the shared query helpers
// through database.Base.
func NewStore(db *sql.DB) *Store {
	return &Store{Base: database.NewBase(db)}
}

const senderSelect = `
SELECT id, project_id, created_by, email, name, created_at
FROM senders`

// Get returns one sender address within projID, or nil when there is
// no such row.
func (s *Store) Get(ctx context.Context, projID, id string) (*smodel.Sender, error) {
	row := s.QueryRow(ctx, senderSelect+` WHERE project_id = ? AND id = ?`, projID, id)
	m, err := scanSender(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return m, err
}

// GetByEmail returns one sender address by email within projID, or nil
// when there is no such row.
func (s *Store) GetByEmail(ctx context.Context, projID, email string) (*smodel.Sender, error) {
	row := s.QueryRow(ctx, senderSelect+` WHERE project_id = ? AND email = ?`, projID, strings.ToLower(email))
	m, err := scanSender(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return m, err
}

// List returns every sender address in projID.
func (s *Store) List(ctx context.Context, projID string) ([]*smodel.Sender, error) {
	rows, err := s.Query(ctx, senderSelect+` WHERE project_id = ? ORDER BY email ASC`, projID)
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
	if err := r.Scan(&m.ID, &m.ProjectID, &m.CreatedBy, &m.Email, &m.Name, &m.CreatedAt); err != nil {
		return nil, err
	}

	return &m, nil
}

// Handler owns the /api/senders surface.
type Handler struct {
	Runtime *env.Runtime
}

// List serves GET /api/v1/senders.
func (h *Handler) List(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	out, err := h.Runtime.Store.Sender.List(c.UserContext(), rc.Project.ID)
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
func (h *Handler) Create(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[createInput](c)
	if !ok {
		return resp
	}

	at := strings.LastIndex(in.Email, "@")
	domainName := in.Email[at+1:]
	d, err := h.Runtime.Store.Domain.GetVerifiedCovering(c.UserContext(), domainName)
	if err != nil {
		return response.Internal(c, err)
	}

	if d == nil || d.ProjectID != rc.Project.ID {
		return response.BadRequest(c,
			"domain "+domainName+" is not verified by this project, verify it under Domains first")
	}

	existing, err := h.Runtime.Store.Sender.GetByEmail(c.UserContext(), rc.Project.ID, in.Email)
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
	if err := h.Runtime.Store.Sender.Put(c.UserContext(), m); err != nil {
		return response.Internal(c, err)
	}

	return response.Created(c, SenderResponse{Sender: m})
}

// Delete serves DELETE /api/v1/senders/:id.
func (h *Handler) Delete(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	m, err := h.Runtime.Store.Sender.Get(c.UserContext(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if m == nil {
		return response.NotFound(c, "sender not found")
	}

	if err := h.Runtime.Store.Sender.Delete(c.UserContext(), rc.Project.ID, m.ID); err != nil {
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
