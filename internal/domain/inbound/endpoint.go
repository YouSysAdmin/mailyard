// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package inbound

import (
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/yousysadmin/mailyard/internal/core/blob"
	"github.com/yousysadmin/mailyard/internal/core/env"
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
func (h *Handler) List(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	f := store.InboundFilter{
		Status: c.Query("status"),
		Limit:  paging.From(c).Limit,
	}
	if before := c.Query("before"); before != "" {
		t, err := time.Parse(time.RFC3339, before)
		if err != nil {
			return response.BadRequest(c, "before must be an RFC 3339 timestamp")
		}

		f.Before = &t
		// The id half of the cursor - optional, for the reason on the
		// email log: without it a tie in received_at across a page
		// boundary drops the tied rows from both pages.
		f.BeforeID = c.Query("before_id")
	}

	emails, err := h.Runtime.Store.Inbound.List(c.UserContext(), rc.Project.ID, f)
	if err != nil {
		return response.Internal(c, err)
	}

	if emails == nil {
		emails = []*imodel.Email{}
	}

	return response.Success(c, ListResponse{InboundEmails: emails})
}

// Stats serves GET /api/v1/inbound-emails/stats.
func (h *Handler) Stats(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	counts, err := h.Runtime.Store.Inbound.CountByStatus(c.UserContext(), rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, StatsResponse{Counts: counts})
}

// Get serves GET /api/v1/inbound-emails/:id.
func (h *Handler) Get(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	e, err := h.Runtime.Store.Inbound.Get(c.UserContext(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if e == nil {
		return response.NotFound(c, "inbound email not found")
	}

	e.HasRaw = len(e.Raw) > 0

	return response.Success(c, GetResponse{InboundEmail: e})
}

// Raw streams the stored wire bytes. Raw is only retained when MIME
// parsing failed (see service.go) - for a cleanly parsed message the
// answer is an honest 404 rather than a reconstruction pretending to
// be the original.
func (h *Handler) Raw(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	e, err := h.Runtime.Store.Inbound.Get(c.UserContext(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if e == nil {
		return response.NotFound(c, "inbound email not found")
	}

	if len(e.Raw) == 0 {
		return response.NotFound(c, "raw bytes are retained only for messages that failed to parse")
	}

	c.Set(fiber.HeaderContentType, "message/rfc822")
	c.Set(fiber.HeaderContentDisposition, `attachment; filename="`+e.ID+`.eml"`)

	return c.Send(e.Raw)
}

// Retry re-fires the inbound.received webhook for one message, so an
// operator whose consumer was down can replay the notification
// without re-delivering the mail itself. The payload mirrors the one
// built at ingest (service.go).
func (h *Handler) Retry(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	e, err := h.Runtime.Store.Inbound.Get(c.UserContext(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if e == nil {
		return response.NotFound(c, "inbound email not found")
	}

	domainName := ""
	if d, derr := h.Runtime.Store.Domain.Get(c.UserContext(), rc.Project.ID, e.DomainID); derr == nil && d != nil {
		domainName = d.Domain
	}

	h.Runtime.Dispatch.Emit(c.UserContext(), e.ProjectID, webhookmodel.EventInboundReceived, e.Sender, map[string]any{
		"id":          e.ID,
		"domain":      domainName,
		"sender":      e.Sender,
		"recipients":  e.Recipients,
		"subject":     e.Subject,
		"message_id":  e.MessageID,
		"size":        e.Size,
		"received_at": e.ReceivedAt,
	})

	return response.Success(c, EmittedResponse{Emitted: true})
}

// Delete serves DELETE /api/v1/inbound-emails/:id.
func (h *Handler) Delete(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	e, err := h.Runtime.Store.Inbound.Get(c.UserContext(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if e == nil {
		return response.NotFound(c, "inbound email not found")
	}

	if err := h.Runtime.Store.Inbound.Delete(c.UserContext(), rc.Project.ID, e.ID); err != nil {
		return response.Internal(c, err)
	}

	// Best effort: reclaim offloaded attachment blobs. The row is
	// gone either way, an orphaned blob is only wasted space.
	if h.Runtime.Blob != nil {
		for _, a := range e.Attachments {
			if a.StorageKey != "" {
				if derr := h.Runtime.Blob.Delete(c.UserContext(), a.StorageKey); derr != nil {
					h.Runtime.Log.Warn("inbound: blob cleanup failed", "key", a.StorageKey, "err", derr)
				}
			}
		}
	}

	return response.NoContent(c)
}

// Attachment streams one attachment's bytes, reading inline content
// or the blob store as appropriate.
func (h *Handler) Attachment(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	e, err := h.Runtime.Store.Inbound.Get(c.UserContext(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if e == nil {
		return response.NotFound(c, "inbound email not found")
	}

	idx, err := c.ParamsInt("idx")
	if err != nil || idx < 0 || idx >= len(e.Attachments) {
		return response.NotFound(c, "attachment not found")
	}

	a := e.Attachments[idx]
	raw, err := blob.Load(c.UserContext(), h.Runtime.Blob, a.StorageKey, a.Content, a.Filename)
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Attachment(c, a.Filename, a.ContentType, raw)
}
