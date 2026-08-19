// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package contact

import (
	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/paging"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/domain"
	cmodel "github.com/yousysadmin/mailyard/internal/models/contact"
)

// Handler serves the read-only /api/contacts surface. There are no
// create, update, or delete routes on purpose - contacts are written
// by the delivery worker, and letting an operator edit a tally would
// make it a number nobody can trust.
type Handler struct {
	Runtime *env.Runtime
}

const (
	defaultPageSize = 20
	maxPageSize     = 200
)

// List serves GET /api/v1/contacts.
func (h *Handler) List(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	search := paging.Search(c, "search")
	pg := paging.FromWith(c, defaultPageSize, maxPageSize)
	limit, offset := pg.Limit, pg.Offset

	contacts, err := h.Runtime.Store.Contact.List(c.Context(), rc.Project.ID, search, limit, offset)
	if err != nil {
		return response.Internal(c, err)
	}

	total, err := h.Runtime.Store.Contact.Count(c.Context(), rc.Project.ID, search)
	if err != nil {
		return response.Internal(c, err)
	}

	if contacts == nil {
		contacts = []*cmodel.Contact{}
	}

	if err := h.markSuppressed(c, rc.Project.ID, contacts); err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, ListResponse{
		Contacts: contacts,
		Total:    total,
		Limit:    limit,
		Offset:   offset,
	})
}

// Get serves GET /api/v1/contacts/:id.
func (h *Handler) Get(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	ct, err := h.Runtime.Store.Contact.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if ct == nil {
		return response.NotFound(c, "contact not found")
	}

	if err := h.markSuppressed(c, rc.Project.ID, []*cmodel.Contact{ct}); err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, GetResponse{Contact: ct})
}

// markSuppressed resolves the suppression flag for a page of
// contacts. Computed here rather than stored on the row so it can
// never drift from the list that actually governs sending: removing
// a suppression takes effect on the very next read.
//
// One batched call rather than a lookup per row - a page of 200
// contacts should not be 200 queries.
func (h *Handler) markSuppressed(c fiber.Ctx, projID string, contacts []*cmodel.Contact) error {
	if len(contacts) == 0 {
		return nil
	}

	emails := make([]string, 0, len(contacts))
	for _, ct := range contacts {
		emails = append(emails, ct.Email)
	}

	_, blocked, err := h.Runtime.Store.Suppression.FilterSuppressed(c.Context(), projID, emails)
	if err != nil {
		return err
	}

	if len(blocked) == 0 {
		return nil
	}

	set := make(map[string]struct{}, len(blocked))
	for _, e := range blocked {
		set[e] = struct{}{}
	}

	for _, ct := range contacts {
		if _, ok := set[ct.Email]; ok {
			ct.Suppressed = true
		}
	}

	return nil
}
