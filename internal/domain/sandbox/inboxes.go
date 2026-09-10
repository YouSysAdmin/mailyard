// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package sandbox

import (
	"slices"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain"
	sbmodel "github.com/yousysadmin/mailyard/internal/models/sandbox"
)

// Inboxes are saved sender filters over the capture list, and this is
// their CRUD. They sit under /sandbox because they are sandbox
// configuration and nothing else: sandbox:read lists them, sandbox:write
// shapes them, sandbox:delete removes them - and removing one removes no
// mail, since no capture names an inbox.

// ListInboxes serves GET /api/v1/sandbox/inboxes.
func (h *Handler) ListInboxes(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	inboxes, err := h.Runtime.Store.SandboxInbox.List(c.Context(), rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, InboxListResponse{SandboxInboxes: inboxes})
}

// GetInbox serves GET /api/v1/sandbox/inboxes/:id.
func (h *Handler) GetInbox(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, err := h.Runtime.Store.SandboxInbox.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if in == nil {
		return response.NotFound(c, "inbox not found")
	}

	return response.Success(c, InboxResponse{SandboxInbox: in})
}

// CreateInbox serves POST /api/v1/sandbox/inboxes.
func (h *Handler) CreateInbox(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[inboxCreateInput](c)
	if !ok {
		return resp
	}

	existing, err := h.Runtime.Store.SandboxInbox.GetByName(c.Context(), rc.Project.ID, in.Name)
	if err != nil {
		return response.Internal(c, err)
	}

	if existing != nil {
		return response.Conflict(c, "an inbox with this name already exists")
	}

	box := &sbmodel.Inbox{
		ID:          ids.New(),
		ProjectID:   rc.Project.ID,
		Name:        in.Name,
		Description: in.Description,
		Addresses:   uniqueAddresses(in.Addresses),
	}
	if err := h.Runtime.Store.SandboxInbox.Put(c.Context(), box); err != nil {
		return response.Internal(c, err)
	}

	return response.Created(c, InboxResponse{SandboxInbox: box})
}

// UpdateInbox serves PATCH /api/v1/sandbox/inboxes/:id.
func (h *Handler) UpdateInbox(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	box, err := h.Runtime.Store.SandboxInbox.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if box == nil {
		return response.NotFound(c, "inbox not found")
	}

	in, resp, ok := validation.Bind[inboxUpdateInput](c)
	if !ok {
		return resp
	}

	if in.Name != "" && in.Name != box.Name {
		clash, err := h.Runtime.Store.SandboxInbox.GetByName(c.Context(), rc.Project.ID, in.Name)
		if err != nil {
			return response.Internal(c, err)
		}

		if clash != nil {
			return response.Conflict(c, "an inbox with this name already exists")
		}

		box.Name = in.Name
	}

	if in.Description != nil {
		box.Description = *in.Description
	}

	if in.Addresses != nil {
		box.Addresses = uniqueAddresses(in.Addresses)
	}

	box.UpdatedAt = new(time.Now().UTC())
	if err := h.Runtime.Store.SandboxInbox.Put(c.Context(), box); err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, InboxResponse{SandboxInbox: box})
}

// DeleteInbox serves DELETE /api/v1/sandbox/inboxes/:id. The captures
// the inbox showed stay where they are - it was a view of them, not a
// container.
func (h *Handler) DeleteInbox(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	box, err := h.Runtime.Store.SandboxInbox.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if box == nil {
		return response.NotFound(c, "inbox not found")
	}

	if err := h.Runtime.Store.SandboxInbox.Delete(c.Context(), rc.Project.ID, box.ID); err != nil {
		return response.Internal(c, err)
	}

	return response.NoContent(c)
}

// uniqueAddresses sorts and dedupes an address list that validation has
// already lowercased, so "A@x" and "a@x" typed twice store once.
func uniqueAddresses(addrs []string) []string {
	out := slices.Clone(addrs)
	slices.Sort(out)

	return slices.Compact(out)
}
