// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package language is the persistence and handler surface for the
// per-project language registry. Routes live behind requireAuth +
// requireProject in server/routes.go.
package language

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/domain"
	lmodel "github.com/yousysadmin/mailyard/internal/models/language"
)

// Store persists the per-project language registry. Project scoped: a
// method taking projID answers nothing for a row another project owns.
type Store struct {
	database.Base
}

// NewStore builds the store on db, wiring the shared query helpers
// through database.Base.
func NewStore(db *sql.DB) *Store {
	return &Store{Base: database.NewBase(db)}
}

const langSelect = `
SELECT id, project_id, code, name, is_default, created_at
FROM languages`

// Get returns one language within projID, or nil when there is no such
// row.
func (s *Store) Get(ctx context.Context, projID, id string) (*lmodel.Language, error) {
	row := s.QueryRow(ctx, langSelect+` WHERE project_id = ? AND id = ?`, projID, id)
	l, err := scanLanguage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return l, err
}

// GetByCode returns one language by code within projID, or nil when
// there is no such row.
func (s *Store) GetByCode(ctx context.Context, projID, code string) (*lmodel.Language, error) {
	row := s.QueryRow(ctx, langSelect+` WHERE project_id = ? AND code = ?`, projID, code)
	l, err := scanLanguage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return l, err
}

// List returns every language in projID.
func (s *Store) List(ctx context.Context, projID string) ([]*lmodel.Language, error) {
	rows, err := s.Query(ctx, langSelect+` WHERE project_id = ? ORDER BY code ASC`, projID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*lmodel.Language
	for rows.Next() {
		l, err := scanLanguage(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, l)
	}

	return out, rows.Err()
}

// Put upserts by id. Marking a language default clears the flag on
// the project's other languages first so exactly one holds it.
func (s *Store) Put(ctx context.Context, l *lmodel.Language) error {
	if l.CreatedAt.IsZero() {
		l.CreatedAt = time.Now().UTC()
	}

	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	defer func() {
		// ErrTxDone is the normal path: Commit already ran. Anything
		// else means the rollback itself failed, which can leave locks
		// held and is worth saying out loud.
		if rerr := tx.Rollback(); rerr != nil && !errors.Is(rerr, sql.ErrTxDone) {
			slog.Warn("store: rollback failed", "err", rerr)
		}
	}()
	if l.IsDefault {
		if _, err := tx.ExecContext(ctx,
			s.Q(`UPDATE languages SET is_default = FALSE WHERE project_id = ?`),
			l.ProjectID); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, s.Q(`
        INSERT INTO languages (id, project_id, code, name, is_default, created_at)
        VALUES (?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            code       = excluded.code,
            name       = excluded.name,
            is_default = excluded.is_default
    `), l.ID, l.ProjectID, l.Code, l.Name, l.IsDefault, l.CreatedAt); err != nil {
		return err
	}

	return tx.Commit()
}

// Delete removes one language from projID.
func (s *Store) Delete(ctx context.Context, projID, id string) error {
	_, err := s.Exec(ctx, `DELETE FROM languages WHERE project_id = ? AND id = ?`, projID, id)

	return err
}

func scanLanguage(r interface{ Scan(...any) error }) (*lmodel.Language, error) {
	var l lmodel.Language
	if err := r.Scan(&l.ID, &l.ProjectID, &l.Code, &l.Name, &l.IsDefault, &l.CreatedAt); err != nil {
		return nil, err
	}

	return &l, nil
}

// Handler owns the /api/languages surface.
type Handler struct {
	Runtime *env.Runtime
}

// List serves GET /api/v1/languages.
func (h *Handler) List(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	langs, err := h.Runtime.Store.Language.List(c.Context(), rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if langs == nil {
		langs = []*lmodel.Language{}
	}

	return response.Success(c, ListResponse{Languages: langs})
}

// Create serves POST /api/v1/languages.
func (h *Handler) Create(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[upsertInput](c)
	if !ok {
		return resp
	}

	existing, err := h.Runtime.Store.Language.GetByCode(c.Context(), rc.Project.ID, in.Code)
	if err != nil {
		return response.Internal(c, err)
	}

	if existing != nil {
		return response.Conflict(c, "a language with this code already exists")
	}

	l := &lmodel.Language{
		ID:        ids.New(),
		ProjectID: rc.Project.ID,
		Code:      in.Code,
		Name:      in.Name,
		IsDefault: in.IsDefault,
	}
	if err := h.Runtime.Store.Language.Put(c.Context(), l); err != nil {
		return response.Internal(c, err)
	}

	return response.Created(c, LanguageResponse{Language: l})
}

// Update serves PUT /api/v1/languages/:id.
func (h *Handler) Update(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	l, err := h.Runtime.Store.Language.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if l == nil {
		return response.NotFound(c, "language not found")
	}

	in, resp, ok := validation.Bind[upsertInput](c)
	if !ok {
		return resp
	}

	if in.Code != l.Code {
		other, err := h.Runtime.Store.Language.GetByCode(c.Context(), rc.Project.ID, in.Code)
		if err != nil {
			return response.Internal(c, err)
		}

		if other != nil {
			return response.Conflict(c, "a language with this code already exists")
		}
	}

	l.Code = in.Code
	l.Name = in.Name
	l.IsDefault = in.IsDefault
	if err := h.Runtime.Store.Language.Put(c.Context(), l); err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, LanguageResponse{Language: l})
}

// Delete serves DELETE /api/v1/languages/:id.
func (h *Handler) Delete(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	l, err := h.Runtime.Store.Language.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if l == nil {
		return response.NotFound(c, "language not found")
	}

	if err := h.Runtime.Store.Language.Delete(c.Context(), rc.Project.ID, l.ID); err != nil {
		return response.Internal(c, err)
	}

	return response.NoContent(c)
}
