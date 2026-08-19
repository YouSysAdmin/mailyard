// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package stylesheet is the persistence and handler surface for
// reusable CSS blocks referenced by template versions. Routes live
// behind requireAuth + requireProject in server/routes.go.
package stylesheet

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
	ssmodel "github.com/yousysadmin/mailyard/internal/models/stylesheet"
)

// Store persists reusable CSS blocks. Project scoped: a method taking
// projID answers nothing for a row another project owns.
type Store struct {
	database.Base
}

// NewStore builds the store on db, wiring the shared query helpers
// through database.Base.
func NewStore(db *sql.DB) *Store {
	return &Store{Base: database.NewBase(db)}
}

const sheetSelect = `
SELECT id, project_id, name, css, created_at, updated_at
FROM stylesheets`

// Get returns one stylesheet within projID, or nil when there is no
// such row.
func (s *Store) Get(ctx context.Context, projID, id string) (*ssmodel.Stylesheet, error) {
	row := s.QueryRow(ctx, sheetSelect+` WHERE project_id = ? AND id = ?`, projID, id)
	sh, err := scanSheet(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return sh, err
}

// List returns every stylesheet in projID.
func (s *Store) List(ctx context.Context, projID string) ([]*ssmodel.Stylesheet, error) {
	rows, err := s.Query(ctx, sheetSelect+` WHERE project_id = ? ORDER BY name ASC`, projID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*ssmodel.Stylesheet
	for rows.Next() {
		sh, err := scanSheet(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, sh)
	}

	return out, rows.Err()
}

// Put inserts the stylesheet, or updates the row when its id already
// exists.
func (s *Store) Put(ctx context.Context, sh *ssmodel.Stylesheet) error {
	if sh.CreatedAt.IsZero() {
		sh.CreatedAt = time.Now().UTC()
	}

	_, err := s.Exec(ctx, `
        INSERT INTO stylesheets (id, project_id, name, css, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            name       = excluded.name,
            css        = excluded.css,
            updated_at = excluded.updated_at
    `, sh.ID, sh.ProjectID, sh.Name, sh.CSS, sh.CreatedAt, database.NullTime(sh.UpdatedAt))

	return err
}

// Delete removes one stylesheet from projID.
func (s *Store) Delete(ctx context.Context, projID, id string) error {
	_, err := s.Exec(ctx, `DELETE FROM stylesheets WHERE project_id = ? AND id = ?`, projID, id)

	return err
}

func scanSheet(r interface{ Scan(...any) error }) (*ssmodel.Stylesheet, error) {
	var sh ssmodel.Stylesheet
	var updated sql.NullTime
	if err := r.Scan(&sh.ID, &sh.ProjectID, &sh.Name, &sh.CSS, &sh.CreatedAt, &updated); err != nil {
		return nil, err
	}

	if updated.Valid {
		sh.UpdatedAt = new(updated.Time)
	}

	return &sh, nil
}

// Handler owns the /api/stylesheets surface.
type Handler struct {
	Runtime *env.Runtime
}

// List serves GET /api/v1/stylesheets.
func (h *Handler) List(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	sheets, err := h.Runtime.Store.Stylesheet.List(c.Context(), rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if sheets == nil {
		sheets = []*ssmodel.Stylesheet{}
	}

	return response.Success(c, ListResponse{Stylesheets: sheets})
}

// Get serves GET /api/v1/stylesheets/:id.
func (h *Handler) Get(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	sh, err := h.Runtime.Store.Stylesheet.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if sh == nil {
		return response.NotFound(c, "stylesheet not found")
	}

	return response.Success(c, StylesheetResponse{Stylesheet: sh})
}

// Create serves POST /api/v1/stylesheets.
func (h *Handler) Create(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[upsertInput](c)
	if !ok {
		return resp
	}

	sh := &ssmodel.Stylesheet{
		ID:        ids.New(),
		ProjectID: rc.Project.ID,
		Name:      in.Name,
		CSS:       in.CSS,
	}
	if err := h.Runtime.Store.Stylesheet.Put(c.Context(), sh); err != nil {
		return response.Internal(c, err)
	}

	return response.Created(c, StylesheetResponse{Stylesheet: sh})
}

// Update serves PUT /api/v1/stylesheets/:id.
func (h *Handler) Update(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	sh, err := h.Runtime.Store.Stylesheet.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if sh == nil {
		return response.NotFound(c, "stylesheet not found")
	}

	in, resp, ok := validation.Bind[upsertInput](c)
	if !ok {
		return resp
	}

	sh.Name = in.Name
	sh.CSS = in.CSS
	sh.UpdatedAt = new(time.Now().UTC())
	if err := h.Runtime.Store.Stylesheet.Put(c.Context(), sh); err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, StylesheetResponse{Stylesheet: sh})
}

// Delete serves DELETE /api/v1/stylesheets/:id.
func (h *Handler) Delete(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	sh, err := h.Runtime.Store.Stylesheet.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if sh == nil {
		return response.NotFound(c, "stylesheet not found")
	}

	if err := h.Runtime.Store.Stylesheet.Delete(c.Context(), rc.Project.ID, sh.ID); err != nil {
		return response.Internal(c, err)
	}

	return response.NoContent(c)
}
