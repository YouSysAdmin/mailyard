// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package subscriber

import (
	"encoding/csv"
	"errors"
	"io"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/paging"
	"github.com/yousysadmin/mailyard/internal/core/quota"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain"
	submodel "github.com/yousysadmin/mailyard/internal/models/subscriber"
)

// Handler owns the /api/subscribers surface.
type Handler struct {
	Runtime *env.Runtime
}

// List serves GET /api/v1/subscribers.
func (h *Handler) List(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	status := c.Query("status")
	if status != "" {
		if _, ok := submodel.ValidStatuses[status]; !ok {
			return response.BadRequest(c, "unknown status "+status)
		}
	}

	pg := paging.From(c)
	search := paging.Search(c, "q")
	subs, err := h.Runtime.Store.Subscriber.List(c.UserContext(), rc.Project.ID,
		status, search, pg.Limit, pg.Offset)
	if err != nil {
		return response.Internal(c, err)
	}

	if subs == nil {
		subs = []*submodel.Subscriber{}
	}

	// The total of what this page is a page OF. Plain Count ignores both
	// filters, so a search matching one row out of five thousand reported
	// total 5000 and the pager offered fifty pages, forty-nine empty.
	total, err := h.Runtime.Store.Subscriber.CountMatching(c.UserContext(), rc.Project.ID, status, search)
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, ListResponse{Subscribers: subs, Total: total})
}

// Get serves GET /api/v1/subscribers/:id.
func (h *Handler) Get(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	sub, err := h.Runtime.Store.Subscriber.Get(c.UserContext(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if sub == nil {
		return response.NotFound(c, "subscriber not found")
	}

	return response.Success(c, SubscriberResponse{Subscriber: sub})
}

// Create serves POST /api/v1/subscribers.
func (h *Handler) Create(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[upsertInput](c)
	if !ok {
		return resp
	}

	if err := quota.CheckResource(c.UserContext(), h.Runtime.Store, rc.Project.ID, quota.ResSubscribers, 1); err != nil {
		if qe, ok := errors.AsType[*quota.Error](err); ok {
			return response.TooManyRequests(c, qe.Error())
		}

		return response.Internal(c, err)
	}

	existing, err := h.Runtime.Store.Subscriber.GetByEmail(c.UserContext(), rc.Project.ID, in.Email)
	if err != nil {
		return response.Internal(c, err)
	}

	if existing != nil {
		return response.Conflict(c, "a subscriber with this email already exists")
	}

	sub := in.toModel(rc.Project.ID)
	if err := h.Runtime.Store.Subscriber.Put(c.UserContext(), sub); err != nil {
		return response.Internal(c, err)
	}

	return response.Created(c, SubscriberResponse{Subscriber: sub})
}

// Update serves PATCH /api/v1/subscribers/:id.
func (h *Handler) Update(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	sub, err := h.Runtime.Store.Subscriber.Get(c.UserContext(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if sub == nil {
		return response.NotFound(c, "subscriber not found")
	}

	in, resp, ok := validation.Bind[upsertInput](c)
	if !ok {
		return resp
	}

	if in.Email != sub.Email {
		other, err := h.Runtime.Store.Subscriber.GetByEmail(c.UserContext(), rc.Project.ID, in.Email)
		if err != nil {
			return response.Internal(c, err)
		}

		if other != nil {
			return response.Conflict(c, "a subscriber with this email already exists")
		}

		sub.Email = in.Email
	}

	sub.Name = in.Name
	if in.Status != "" && in.Status != sub.Status {
		sub.Status = in.Status
		if in.Status == submodel.StatusUnsubscribed {
			sub.UnsubscribedAt = new(time.Now().UTC())
		}
	}

	if in.CustomFields != nil {
		sub.CustomFields = in.CustomFields
	}

	sub.Timezone = in.Timezone
	sub.Language = in.Language
	sub.UpdatedAt = new(time.Now().UTC())
	if err := h.Runtime.Store.Subscriber.Put(c.UserContext(), sub); err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, SubscriberResponse{Subscriber: sub})
}

// Delete serves DELETE /api/v1/subscribers/:id.
func (h *Handler) Delete(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	sub, err := h.Runtime.Store.Subscriber.Get(c.UserContext(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if sub == nil {
		return response.NotFound(c, "subscriber not found")
	}

	if err := h.Runtime.Store.Subscriber.Delete(c.UserContext(), rc.Project.ID, sub.ID); err != nil {
		return response.Internal(c, err)
	}

	return response.NoContent(c)
}

// Import bulk-upserts subscribers from JSON. Existing emails are
// updated, invalid rows are reported and skipped.
func (h *Handler) Import(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[importInput](c)
	if !ok {
		return resp
	}

	return h.runImport(c, rc.Project.ID, in.Subscribers)
}

// ImportCSV bulk-upserts from a CSV body. The header row names the
// columns: email (required), name, status, timezone, language - any
// other column lands in custom_fields.
func (h *Handler) ImportCSV(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	reader := csv.NewReader(strings.NewReader(string(c.Body())))
	reader.TrimLeadingSpace = true
	header, err := reader.Read()
	if err != nil {
		return response.BadRequest(c, "csv is empty or unreadable")
	}

	for i := range header {
		header[i] = strings.ToLower(strings.TrimSpace(header[i]))
	}

	emailCol := -1
	for i, name := range header {
		if name == "email" {
			emailCol = i
		}
	}

	if emailCol < 0 {
		return response.BadRequest(c, "csv header must include an email column")
	}

	var items []upsertInput
	for line := 2; ; line++ {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return response.BadRequest(c, "csv parse error at line "+strconv.Itoa(line))
		}

		if len(items) >= 10000 {
			return response.BadRequest(c, "csv exceeds the 10000 row import limit")
		}

		item := upsertInput{CustomFields: map[string]any{}}
		for i, val := range record {
			if i >= len(header) {
				break
			}

			val = strings.TrimSpace(val)
			switch header[i] {
			case "email":
				item.Email = strings.ToLower(val)
			case "name":
				item.Name = val
			case "status":
				item.Status = strings.ToLower(val)
			case "timezone":
				item.Timezone = val
			case "language":
				item.Language = strings.ToLower(val)
			default:
				if val != "" {
					item.CustomFields[header[i]] = val
				}
			}
		}

		items = append(items, item)
	}

	if len(items) == 0 {
		return response.BadRequest(c, "csv has no data rows")
	}

	return h.runImport(c, rc.Project.ID, items)
}

// runImport applies the upserts, tolerating per-row failures.
func (h *Handler) runImport(c *fiber.Ctx, projID string, items []upsertInput) error {
	if err := quota.CheckResource(c.UserContext(), h.Runtime.Store, projID, quota.ResSubscribers, len(items)); err != nil {
		if qe, ok := errors.AsType[*quota.Error](err); ok {
			return response.TooManyRequests(c, qe.Error())
		}

		return response.Internal(c, err)
	}

	created, updated := 0, 0
	var rowErrors []fiber.Map
	for i, item := range items {
		if _, err := mail.ParseAddress(item.Email); err != nil {
			rowErrors = append(rowErrors, fiber.Map{"index": i, "email": item.Email, "error": "invalid email"})
			continue
		}

		if item.Status != "" {
			if _, ok := submodel.ValidStatuses[item.Status]; !ok {
				rowErrors = append(rowErrors, fiber.Map{"index": i, "email": item.Email, "error": "unknown status " + item.Status})
				continue
			}
		}

		existing, err := h.Runtime.Store.Subscriber.GetByEmail(c.UserContext(), projID, item.Email)
		if err != nil {
			return response.Internal(c, err)
		}

		sub := item.toModel(projID)
		if existing != nil {
			sub.ID = existing.ID
			sub.CreatedAt = existing.CreatedAt
			sub.SubscribedAt = existing.SubscribedAt
			sub.UpdatedAt = new(time.Now().UTC())
			if sub.Status == "" {
				sub.Status = existing.Status
			}

			updated++
		} else {
			created++
		}

		if err := h.Runtime.Store.Subscriber.Put(c.UserContext(), sub); err != nil {
			return response.Internal(c, err)
		}
	}

	if rowErrors == nil {
		rowErrors = []fiber.Map{}
	}

	return response.Success(c, ImportResponse{
		Created: created, Updated: updated,
		Skipped: len(rowErrors), Errors: rowErrors,
	})
}

func (in *upsertInput) toModel(projID string) *submodel.Subscriber {
	return &submodel.Subscriber{
		ProjectID:    projID,
		Email:        in.Email,
		Name:         in.Name,
		Status:       in.Status,
		CustomFields: in.CustomFields,
		Timezone:     in.Timezone,
		Language:     in.Language,
	}
}
