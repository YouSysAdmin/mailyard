// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package subscriber

import (
	"encoding/csv"
	"errors"
	"io"
	"maps"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/paging"
	"github.com/yousysadmin/mailyard/internal/core/quota"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain"
	submodel "github.com/yousysadmin/mailyard/internal/models/subscriber"
	slmodel "github.com/yousysadmin/mailyard/internal/models/subscriberlist"
)

// Handler owns the /api/subscribers surface.
type Handler struct {
	Runtime *env.Runtime
}

// List serves GET /api/v1/subscribers.
func (h *Handler) List(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	status := c.Query("status")
	if status != "" {
		if _, ok := submodel.ValidStatuses[status]; !ok {
			return response.BadRequest(c, "unknown status "+status)
		}
	}

	// One whole address is a lookup, not a search: it goes through the
	// unique index and answers at most one row, in the list's shape so
	// a client keeps one code path.
	if email := paging.Search(c, "email"); email != "" {
		sub, err := h.Runtime.Store.Subscriber.GetByEmail(c.Context(), rc.Project.ID, email)
		if err != nil {
			return response.Internal(c, err)
		}

		out := ListResponse{Subscribers: []*submodel.Subscriber{}}
		if sub != nil && (status == "" || sub.Status == status) {
			out.Subscribers = append(out.Subscribers, sub)
			out.Total = 1
		}

		return response.Success(c, out)
	}

	pg := paging.From(c)
	search := paging.Search(c, "q")
	subs, err := h.Runtime.Store.Subscriber.List(c.Context(), rc.Project.ID,
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
	total, err := h.Runtime.Store.Subscriber.CountMatching(c.Context(), rc.Project.ID, status, search)
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, ListResponse{Subscribers: subs, Total: total})
}

// Get serves GET /api/v1/subscribers/:id.
func (h *Handler) Get(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	sub, err := h.Runtime.Store.Subscriber.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if sub == nil {
		return response.NotFound(c, "subscriber not found")
	}

	return response.Success(c, SubscriberResponse{Subscriber: sub})
}

// Lists serves GET /api/v1/subscribers/:id/lists: the static lists the
// subscriber is on.
func (h *Handler) Lists(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	sub, err := h.Runtime.Store.Subscriber.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if sub == nil {
		return response.NotFound(c, "subscriber not found")
	}

	lists, err := h.Runtime.Store.SubscriberList.ListsOf(c.Context(), rc.Project.ID, sub.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if lists == nil {
		lists = []*slmodel.Membership{}
	}

	return response.Success(c, MembershipResponse{SubscriberLists: lists})
}

// Create serves POST /api/v1/subscribers.
func (h *Handler) Create(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[upsertInput](c)
	if !ok {
		return resp
	}

	release, err := quota.HoldResource(c.Context(), h.Runtime.Store, rc.Project.ID, quota.ResSubscribers, 1)
	if err != nil {
		if qe, ok := errors.AsType[*quota.Error](err); ok {
			return response.TooManyRequests(c, qe.Error())
		}

		return response.Internal(c, err)
	}

	defer release()

	existing, err := h.Runtime.Store.Subscriber.GetByEmail(c.Context(), rc.Project.ID, in.Email)
	if err != nil {
		return response.Internal(c, err)
	}

	if existing != nil {
		return response.Conflict(c, "a subscriber with this email already exists")
	}

	sub := in.toModel(rc.Project.ID)
	if err := h.Runtime.Store.Subscriber.Put(c.Context(), sub); err != nil {
		return response.Internal(c, err)
	}

	return response.Created(c, SubscriberResponse{Subscriber: sub})
}

// Update serves PATCH /api/v1/subscribers/:id.
func (h *Handler) Update(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	sub, err := h.Runtime.Store.Subscriber.Get(c.Context(), rc.Project.ID, c.Params("id"))
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
		other, err := h.Runtime.Store.Subscriber.GetByEmail(c.Context(), rc.Project.ID, in.Email)
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
	if err := h.Runtime.Store.Subscriber.Put(c.Context(), sub); err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, SubscriberResponse{Subscriber: sub})
}

// Delete serves DELETE /api/v1/subscribers/:id.
func (h *Handler) Delete(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	sub, err := h.Runtime.Store.Subscriber.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if sub == nil {
		return response.NotFound(c, "subscriber not found")
	}

	if err := h.Runtime.Store.Subscriber.Delete(c.Context(), rc.Project.ID, sub.ID); err != nil {
		return response.Internal(c, err)
	}

	return response.NoContent(c)
}

// Import bulk-upserts subscribers from JSON. Existing emails are
// updated with what the row carries, invalid rows are reported and
// skipped.
func (h *Handler) Import(c fiber.Ctx) error {
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
func (h *Handler) ImportCSV(c fiber.Ctx) error {
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
func (h *Handler) runImport(c fiber.Ctx, projID string, items []upsertInput) error {
	release, err := quota.HoldResource(c.Context(), h.Runtime.Store, projID, quota.ResSubscribers, int64(len(items)))
	if err != nil {
		if qe, ok := errors.AsType[*quota.Error](err); ok {
			return response.TooManyRequests(c, qe.Error())
		}

		return response.Internal(c, err)
	}

	defer release()

	created, updated := 0, 0
	rowErrors := []ImportError{}
	for i, item := range items {
		if err := validation.NormalizeAndValidate(&item); err != nil {
			rowErrors = append(rowErrors, ImportError{Index: i, Email: item.Email, Error: validation.Summary(validation.Humanize(err))})

			continue
		}

		if _, err := mail.ParseAddress(item.Email); err != nil {
			rowErrors = append(rowErrors, ImportError{Index: i, Email: item.Email, Error: "invalid email"})

			continue
		}

		existing, err := h.Runtime.Store.Subscriber.GetByEmail(c.Context(), projID, item.Email)
		if err != nil {
			return response.Internal(c, err)
		}

		sub := item.toModel(projID)
		if existing != nil {
			sub = mergeImported(existing, &item)
			updated++
		} else {
			created++
		}

		if err := h.Runtime.Store.Subscriber.Put(c.Context(), sub); err != nil {
			return response.Internal(c, err)
		}
	}

	return response.Success(c, ImportResponse{
		Created: created, Updated: updated,
		Skipped: len(rowErrors), Errors: rowErrors,
	})
}

// mergeImported folds an import row onto the subscriber it names. What
// the row leaves empty keeps what the subscriber has, and its custom
// fields are laid over the existing ones key by key, so a file carrying
// one column does not erase the others.
func mergeImported(existing *submodel.Subscriber, in *upsertInput) *submodel.Subscriber {
	sub := *existing
	sub.UpdatedAt = new(time.Now().UTC())
	if in.Name != "" {
		sub.Name = in.Name
	}

	if in.Status != "" && in.Status != sub.Status {
		sub.Status = in.Status
		if in.Status == submodel.StatusUnsubscribed {
			sub.UnsubscribedAt = new(time.Now().UTC())
		}
	}

	if in.Timezone != "" {
		sub.Timezone = in.Timezone
	}

	if in.Language != "" {
		sub.Language = in.Language
	}

	if len(in.CustomFields) > 0 {
		fields := make(map[string]any, len(existing.CustomFields)+len(in.CustomFields))
		maps.Copy(fields, existing.CustomFields)
		maps.Copy(fields, in.CustomFields)
		sub.CustomFields = fields
	}

	return &sub
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
