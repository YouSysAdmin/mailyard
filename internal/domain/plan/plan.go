// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package plan is the persistence and handler surface for usage
// plans. Plans are platform-wide (not tenant scoped) and managed by
// platform admins - projects reference one by plan_id. Enforcement
// lives in core/quota.
package plan

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/quota"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/domain"
	pmodel "github.com/yousysadmin/mailyard/internal/models/plan"
)

// Store persists usage plans. Not project scoped - these belong to the
// installation, not to a tenant.
type Store struct {
	database.Base
}

// NewStore builds the store on db, wiring the shared query helpers
// through database.Base.
func NewStore(db *sql.DB) *Store {
	return &Store{Base: database.NewBase(db)}
}

const planSelect = `
SELECT id, name, description, is_default, hourly_email_limit, daily_email_limit,
       max_api_keys, max_smtp_servers, max_domains, max_subscribers,
       max_sandbox_messages, max_sandbox_retention_days,
       created_at, updated_at
FROM plans`

// Get returns one plan by id, or nil when there is no such row.
func (s *Store) Get(ctx context.Context, id string) (*pmodel.Plan, error) {
	row := s.QueryRow(ctx, planSelect+` WHERE id = ?`, id)
	p, err := scanPlan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return p, err
}

// GetDefault returns the plan applied to projects with none assigned,
// or nil when no plan holds the flag.
func (s *Store) GetDefault(ctx context.Context) (*pmodel.Plan, error) {
	row := s.QueryRow(ctx, planSelect+` WHERE is_default = TRUE LIMIT 1`)
	p, err := scanPlan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return p, err
}

// List returns every plan.
func (s *Store) List(ctx context.Context) ([]*pmodel.Plan, error) {
	rows, err := s.Query(ctx, planSelect+` ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*pmodel.Plan
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, p)
	}

	return out, rows.Err()
}

// Put upserts by id. Marking a plan default clears the flag on every
// other plan first so exactly one holds it.
func (s *Store) Put(ctx context.Context, p *pmodel.Plan) error {
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now().UTC()
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
	if p.IsDefault {
		if _, err := tx.ExecContext(ctx, s.Q(`UPDATE plans SET is_default = FALSE`)); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, s.Q(`
        INSERT INTO plans (id, name, description, is_default, hourly_email_limit,
            daily_email_limit, max_api_keys, max_smtp_servers, max_domains,
            max_subscribers, max_sandbox_messages, max_sandbox_retention_days,
            created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            name               = excluded.name,
            description        = excluded.description,
            is_default         = excluded.is_default,
            hourly_email_limit = excluded.hourly_email_limit,
            daily_email_limit  = excluded.daily_email_limit,
            max_api_keys       = excluded.max_api_keys,
            max_smtp_servers   = excluded.max_smtp_servers,
            max_domains        = excluded.max_domains,
            max_subscribers    = excluded.max_subscribers,
            max_sandbox_messages = excluded.max_sandbox_messages,
            max_sandbox_retention_days = excluded.max_sandbox_retention_days,
            updated_at         = excluded.updated_at
    `), p.ID, p.Name, p.Description, p.IsDefault, p.HourlyEmailLimit,
		p.DailyEmailLimit, p.MaxAPIKeys, p.MaxSMTPServers, p.MaxDomains,
		p.MaxSubscribers, p.MaxSandboxMessages, p.MaxSandboxRetentionDays,
		p.CreatedAt, database.NullTime(p.UpdatedAt)); err != nil {
		return err
	}

	return tx.Commit()
}

// Delete removes one plan by id.
func (s *Store) Delete(ctx context.Context, id string) error {
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

	// Unassign the plan from projects so they fall back to the
	// default rather than pointing at a ghost.
	if _, err := tx.ExecContext(ctx, s.Q(`UPDATE projects SET plan_id = NULL WHERE plan_id = ?`), id); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, s.Q(`DELETE FROM plans WHERE id = ?`), id); err != nil {
		return err
	}

	return tx.Commit()
}

func scanPlan(r interface{ Scan(...any) error }) (*pmodel.Plan, error) {
	var p pmodel.Plan
	var updated sql.NullTime
	if err := r.Scan(&p.ID, &p.Name, &p.Description, &p.IsDefault,
		&p.HourlyEmailLimit, &p.DailyEmailLimit, &p.MaxAPIKeys, &p.MaxSMTPServers,
		&p.MaxDomains, &p.MaxSubscribers, &p.MaxSandboxMessages,
		&p.MaxSandboxRetentionDays, &p.CreatedAt, &updated); err != nil {
		return nil, err
	}

	if updated.Valid {
		p.UpdatedAt = new(updated.Time)
	}

	return &p, nil
}

// Handler owns the /api/plans surface (platform admin) plus the
// per-project usage endpoint.
type Handler struct {
	Runtime *env.Runtime
}

// List serves GET /api/v1/admin/plans.
func (h *Handler) List(c *fiber.Ctx) error {
	plans, err := h.Runtime.Store.Plan.List(c.UserContext())
	if err != nil {
		return response.Internal(c, err)
	}

	if plans == nil {
		plans = []*pmodel.Plan{}
	}

	return response.Success(c, ListResponse{Plans: plans})
}

// Create serves POST /api/v1/admin/plans.
func (h *Handler) Create(c *fiber.Ctx) error {
	in, resp, ok := validation.Bind[upsertInput](c)
	if !ok {
		return resp
	}

	p := &pmodel.Plan{ID: ids.New()}
	apply(p, in)
	if err := h.Runtime.Store.Plan.Put(c.UserContext(), p); err != nil {
		return response.Internal(c, err)
	}

	return response.Created(c, PlanResponse{Plan: p})
}

// Update serves PATCH /api/v1/admin/plans/:id.
func (h *Handler) Update(c *fiber.Ctx) error {
	p, err := h.Runtime.Store.Plan.Get(c.UserContext(), c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if p == nil {
		return response.NotFound(c, "plan not found")
	}

	in, resp, ok := validation.Bind[upsertInput](c)
	if !ok {
		return resp
	}

	apply(p, in)
	now := time.Now().UTC()
	p.UpdatedAt = &now
	if err := h.Runtime.Store.Plan.Put(c.UserContext(), p); err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, PlanResponse{Plan: p})
}

// Delete serves DELETE /api/v1/admin/plans/:id.
func (h *Handler) Delete(c *fiber.Ctx) error {
	p, err := h.Runtime.Store.Plan.Get(c.UserContext(), c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if p == nil {
		return response.NotFound(c, "plan not found")
	}

	if err := h.Runtime.Store.Plan.Delete(c.UserContext(), p.ID); err != nil {
		return response.Internal(c, err)
	}

	return response.NoContent(c)
}

// Assign sets or clears a project's plan. Platform admin only.
func (h *Handler) Assign(c *fiber.Ctx) error {
	w, err := h.Runtime.Store.Project.Get(c.UserContext(), c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if w == nil {
		return response.NotFound(c, "project not found")
	}

	in, resp, ok := validation.Bind[assignInput](c)
	if !ok {
		return resp
	}

	if in.PlanID != "" {
		p, err := h.Runtime.Store.Plan.Get(c.UserContext(), in.PlanID)
		if err != nil {
			return response.Internal(c, err)
		}

		if p == nil {
			return response.NotFound(c, "plan not found")
		}
	}

	w.PlanID = in.PlanID
	now := time.Now().UTC()
	w.UpdatedAt = &now
	if err := h.Runtime.Store.Project.Put(c.UserContext(), w); err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, AssignResponse{Project: w})
}

// Usage reports the active project's effective plan and current
// consumption so the UI can render limits. Any member may read it.
func (h *Handler) Usage(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	counts, p, err := quota.Usage(c.UserContext(), h.Runtime.Store, rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, UsageResponse{Usage: counts, Plan: p})
}

// apply folds the body onto the record, leaving absent fields alone.
//
// An absent limit keeps what the plan already sells. See upsertInput for
// why that cannot be expressed with a plain int: 0 is a MEANING here
// (unlimited), not a blank.
func apply(p *pmodel.Plan, in upsertInput) {
	p.Name = in.Name
	p.Description = in.Description

	if in.IsDefault != nil {
		p.IsDefault = *in.IsDefault
	}

	if in.HourlyEmailLimit != nil {
		p.HourlyEmailLimit = *in.HourlyEmailLimit
	}

	if in.DailyEmailLimit != nil {
		p.DailyEmailLimit = *in.DailyEmailLimit
	}

	if in.MaxAPIKeys != nil {
		p.MaxAPIKeys = *in.MaxAPIKeys
	}

	if in.MaxSMTPServers != nil {
		p.MaxSMTPServers = *in.MaxSMTPServers
	}

	if in.MaxDomains != nil {
		p.MaxDomains = *in.MaxDomains
	}

	if in.MaxSubscribers != nil {
		p.MaxSubscribers = *in.MaxSubscribers
	}

	if in.MaxSandboxMessages != nil {
		p.MaxSandboxMessages = *in.MaxSandboxMessages
	}

	if in.MaxSandboxRetentionDays != nil {
		p.MaxSandboxRetentionDays = *in.MaxSandboxRetentionDays
	}
}
