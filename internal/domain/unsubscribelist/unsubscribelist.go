// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package unsubscribelist persists transactional opt-out scopes and
// serves their console surface. Membership is deliberately absent -
// an address is "opted out" when a suppression row points at the
// list, nothing more.
package unsubscribelist

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/domain"
	ulmodel "github.com/yousysadmin/mailyard/internal/models/unsubscribelist"
)

// Store persists transactional opt-out scopes. Project scoped: a
// method taking projID answers nothing for a row another project owns.
type Store struct {
	database.Base
}

// NewStore builds the store on db, wiring the shared query helpers
// through database.Base.
func NewStore(db *sql.DB) *Store {
	return &Store{Base: database.NewBase(db)}
}

const listSelect = `
SELECT id, project_id, name, public_name, description, active, created_at, updated_at
FROM unsubscribe_lists`

// Get returns one unsubscribe list within projID, or nil when there is
// no such row.
func (s *Store) Get(ctx context.Context, projID, id string) (*ulmodel.List, error) {
	row := s.QueryRow(ctx, listSelect+` WHERE project_id = ? AND id = ?`, projID, id)
	l, err := scanList(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return l, err
}

// GetAny resolves a list without a project, for the public
// unsubscribe page where the signed token is the authority.
func (s *Store) GetAny(ctx context.Context, id string) (*ulmodel.List, error) {
	row := s.QueryRow(ctx, listSelect+` WHERE id = ?`, id)
	l, err := scanList(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return l, err
}

// GetByName returns one unsubscribe list by name within projID, or nil
// when there is no such row.
func (s *Store) GetByName(ctx context.Context, projID, name string) (*ulmodel.List, error) {
	row := s.QueryRow(ctx, listSelect+` WHERE project_id = ? AND name = ?`, projID, name)
	l, err := scanList(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return l, err
}

// List returns every unsubscribe list in projID.
func (s *Store) List(ctx context.Context, projID string) ([]*ulmodel.List, error) {
	rows, err := s.Query(ctx, listSelect+` WHERE project_id = ? ORDER BY name ASC`, projID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*ulmodel.List
	for rows.Next() {
		l, err := scanList(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, l)
	}

	return out, rows.Err()
}

// Put inserts the unsubscribe list, or updates the row when its id
// already exists.
func (s *Store) Put(ctx context.Context, l *ulmodel.List) error {
	if l.CreatedAt.IsZero() {
		l.CreatedAt = time.Now().UTC()
	}

	_, err := s.Exec(ctx, `
        INSERT INTO unsubscribe_lists (
            id, project_id, name, public_name, description, active, created_at, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            name        = excluded.name,
            public_name = excluded.public_name,
            description = excluded.description,
            active      = excluded.active,
            updated_at  = excluded.updated_at
    `, l.ID, l.ProjectID, l.Name, l.PublicName, l.Description, l.Active,
		l.CreatedAt, database.NullTime(l.UpdatedAt))

	return err
}

// Delete removes one unsubscribe list from projID.
func (s *Store) Delete(ctx context.Context, projID, id string) error {
	_, err := s.Exec(ctx, `DELETE FROM unsubscribe_lists WHERE project_id = ? AND id = ?`, projID, id)

	return err
}

func scanList(r interface{ Scan(...any) error }) (*ulmodel.List, error) {
	var l ulmodel.List
	var updated sql.NullTime
	if err := r.Scan(&l.ID, &l.ProjectID, &l.Name, &l.PublicName,
		&l.Description, &l.Active, &l.CreatedAt, &updated); err != nil {
		return nil, err
	}

	if updated.Valid {
		l.UpdatedAt = new(updated.Time)
	}

	return &l, nil
}

// Handler owns /api/unsubscribe-lists.
type Handler struct {
	Runtime *env.Runtime
}

// List serves GET /api/v1/unsubscribe-lists.
func (h *Handler) List(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	lists, err := h.Runtime.Store.UnsubscribeList.List(c.Context(), rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if lists == nil {
		lists = []*ulmodel.List{}
	}

	// The opt-out tally is the number an operator actually wants to
	// see next to a list, and it lives in the suppressions table.
	for _, l := range lists {
		n, err := h.Runtime.Store.Suppression.CountForList(c.Context(), rc.Project.ID, l.ID)
		if err != nil {
			return response.Internal(c, err)
		}

		l.SuppressedCount = n
	}

	return response.Success(c, ListResponse{UnsubscribeLists: lists})
}

// Get serves GET /api/v1/unsubscribe-lists/:id.
func (h *Handler) Get(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	l, err := h.Runtime.Store.UnsubscribeList.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if l == nil {
		return response.NotFound(c, "unsubscribe list not found")
	}

	n, err := h.Runtime.Store.Suppression.CountForList(c.Context(), rc.Project.ID, l.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	l.SuppressedCount = n

	return response.Success(c, GetResponse{UnsubscribeList: l})
}

// Create serves POST /api/v1/unsubscribe-lists.
func (h *Handler) Create(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[createInput](c)
	if !ok {
		return resp
	}

	existing, err := h.Runtime.Store.UnsubscribeList.GetByName(c.Context(), rc.Project.ID, in.Name)
	if err != nil {
		return response.Internal(c, err)
	}

	if existing != nil {
		return response.Conflict(c, "an unsubscribe list with this name already exists")
	}

	l := &ulmodel.List{
		ID:          ids.New(),
		ProjectID:   rc.Project.ID,
		Name:        in.Name,
		PublicName:  in.PublicName,
		Description: in.Description,
		Active:      true,
	}
	if err := h.Runtime.Store.UnsubscribeList.Put(c.Context(), l); err != nil {
		return response.Internal(c, err)
	}

	return response.Created(c, GetResponse{UnsubscribeList: l})
}

// Update serves PATCH /api/v1/unsubscribe-lists/:id.
func (h *Handler) Update(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	l, err := h.Runtime.Store.UnsubscribeList.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if l == nil {
		return response.NotFound(c, "unsubscribe list not found")
	}

	in, resp, ok := validation.Bind[updateInput](c)
	if !ok {
		return resp
	}

	if in.Name != "" && in.Name != l.Name {
		clash, err := h.Runtime.Store.UnsubscribeList.GetByName(c.Context(), rc.Project.ID, in.Name)
		if err != nil {
			return response.Internal(c, err)
		}

		if clash != nil {
			return response.Conflict(c, "an unsubscribe list with this name already exists")
		}

		l.Name = in.Name
	}

	if in.PublicName != nil {
		l.PublicName = *in.PublicName
	}

	if in.Description != nil {
		l.Description = *in.Description
	}

	if in.Active != nil {
		l.Active = *in.Active
	}

	l.UpdatedAt = new(time.Now().UTC())
	if err := h.Runtime.Store.UnsubscribeList.Put(c.Context(), l); err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, GetResponse{UnsubscribeList: l})
}

// Delete removes the scope. The opt-out rows pointing at it are left
// alone deliberately: deleting a list must not silently resubscribe
// everyone who asked to be left out of it, and a recreated list with
// the same name gets a new id anyway.
func (h *Handler) Delete(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	l, err := h.Runtime.Store.UnsubscribeList.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if l == nil {
		return response.NotFound(c, "unsubscribe list not found")
	}

	if err := h.Runtime.Store.UnsubscribeList.Delete(c.Context(), rc.Project.ID, l.ID); err != nil {
		return response.Internal(c, err)
	}

	return response.NoContent(c)
}
