// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package inbound

import (
	"strconv"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/blob"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/keyset"
	"github.com/yousysadmin/mailyard/internal/core/paging"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	imodel "github.com/yousysadmin/mailyard/internal/models/inbound"
	webhookmodel "github.com/yousysadmin/mailyard/internal/models/webhook"
)

// Handler owns the /api/inbound-emails surface (and its /api/v1
// mirror).
type Handler struct {
	Runtime *env.Runtime
}

// List serves GET /api/v1/inbound-emails.
func (h *Handler) List(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	w := paging.WindowFrom(c)
	f := store.InboundFilter{
		Status:    c.Query("status"),
		Sender:    paging.Search(c, "sender"),
		Recipient: paging.Search(c, "recipient"),
		Search:    paging.Search(c, "search"),
		Limit:     w.Fetch(),
		Cursor:    w.Cursor,
	}

	rows, err := h.Runtime.Store.Inbound.List(c.Context(), rc.Project.ID, f)
	if err != nil {
		return response.Internal(c, err)
	}

	page, more := keyset.Cut(rows, w.Limit)
	if page == nil {
		page = []*imodel.Email{}
	}

	next := ""
	if more && len(page) > 0 {
		last := page[len(page)-1]
		next = keyset.Cursor{CreatedAt: last.ReceivedAt, ID: last.ID}.Encode()
	}

	return response.Success(c, ListResponse{InboundEmails: page, NextCursor: next})
}

// Stats serves GET /api/v1/inbound-emails/stats.
func (h *Handler) Stats(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	counts, err := h.Runtime.Store.Inbound.CountByStatus(c.Context(), rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, StatsResponse{Counts: counts})
}

// Get serves GET /api/v1/inbound-emails/:id.
func (h *Handler) Get(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	e, err := h.Runtime.Store.Inbound.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if e == nil {
		return response.NotFound(c, "inbound email not found")
	}

	e.HasRaw = len(e.Raw) > 0

	return response.Success(c, GetResponse{InboundEmail: e})
}

// EML serves GET /api/v1/inbound-emails/:id/eml, the message as an
// .eml file: the wire bytes when they were kept, otherwise rebuilt from
// the stored parts - see rebuild.
func (h *Handler) EML(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	e, err := h.Runtime.Store.Inbound.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if e == nil {
		return response.NotFound(c, "inbound email not found")
	}

	raw := e.Raw
	if len(raw) == 0 {
		// A refused message is kept as its envelope and the reason, and
		// the content sweep empties an old one down to the same. A file
		// built from that is an empty shell, not the message.
		if e.TextBody == "" && e.HTMLBody == "" && len(e.Attachments) == 0 {
			return response.NotFound(c, "nothing of this message's content is stored")
		}

		raw, err = rebuild(c.Context(), h.Runtime.Blob, e)
		if err != nil {
			return response.Internal(c, err)
		}
	}

	return response.Attachment(c, e.ID+".eml", "message/rfc822", raw)
}

// Retry re-fires the inbound.received webhook for one message, so an
// operator whose consumer was down can replay the notification
// without re-delivering the mail itself. The payload mirrors the one
// built at ingest (service.go).
func (h *Handler) Retry(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	e, err := h.Runtime.Store.Inbound.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if e == nil {
		return response.NotFound(c, "inbound email not found")
	}

	h.Runtime.Dispatch.Emit(c.Context(), e.ProjectID, webhookmodel.EventInboundReceived, e.Sender, map[string]any{
		"id":             e.ID,
		"domain":         e.Domain,
		"sender":         e.Sender,
		"bounce_address": e.BounceAddress,
		"recipients":     e.Recipients,
		"subject":        e.Subject,
		"message_id":     e.MessageID,
		"size":           e.Size,
		"received_at":    e.ReceivedAt,
	})

	return response.Success(c, EmittedResponse{Emitted: true})
}

// Delete serves DELETE /api/v1/inbound-emails/:id.
func (h *Handler) Delete(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	e, err := h.Runtime.Store.Inbound.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if e == nil {
		return response.NotFound(c, "inbound email not found")
	}

	if err := h.Runtime.Store.Inbound.Delete(c.Context(), rc.Project.ID, e.ID); err != nil {
		return response.Internal(c, err)
	}

	// Best effort: reclaim offloaded attachment blobs. The row is
	// gone either way, an orphaned blob is only wasted space.
	if h.Runtime.Blob != nil {
		for _, a := range e.Attachments {
			if a.StorageKey != "" {
				if derr := h.Runtime.Blob.Delete(c.Context(), a.StorageKey); derr != nil {
					h.Runtime.Log.Warn("inbound: blob cleanup failed", "key", a.StorageKey, "err", derr)
				}
			}
		}
	}

	return response.NoContent(c)
}

// Attachment streams one attachment's bytes, reading inline content
// or the blob store as appropriate.
func (h *Handler) Attachment(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	e, err := h.Runtime.Store.Inbound.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if e == nil {
		return response.NotFound(c, "inbound email not found")
	}

	idx, err := strconv.Atoi(c.Params("idx"))
	if err != nil || idx < 0 || idx >= len(e.Attachments) {
		return response.NotFound(c, "attachment not found")
	}

	a := e.Attachments[idx]
	raw, err := blob.Load(c.Context(), h.Runtime.Blob, a.StorageKey, a.Content, a.Filename)
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Attachment(c, a.Filename, a.ContentType, raw)
}
