// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package template

import (
	"bytes"
	"encoding/base64"
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/blob"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain"
	tmodel "github.com/yousysadmin/mailyard/internal/models/template"
)

// ListAttachments serves GET /api/v1/templates/:id/attachments.
func (h *Handler) ListAttachments(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	t, err := h.Runtime.Store.Template.Get(c.UserContext(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if t == nil {
		return response.NotFound(c, "template not found")
	}

	atts, err := h.Runtime.Store.Template.ListAttachments(c.UserContext(), rc.Project.ID, t.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if atts == nil {
		atts = []*tmodel.Attachment{}
	}

	return response.Success(c, AttachmentListResponse{Attachments: atts})
}

// UploadAttachment serves POST /api/v1/templates/:id/attachments.
func (h *Handler) UploadAttachment(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	t, err := h.Runtime.Store.Template.Get(c.UserContext(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if t == nil {
		return response.NotFound(c, "template not found")
	}

	in, resp, ok := validation.Bind[attachmentInput](c)
	if !ok {
		return resp
	}

	raw, err := base64.StdEncoding.DecodeString(in.Content)
	if err != nil {
		return response.BadRequest(c, "content must be valid base64")
	}

	if maxSize := h.Runtime.Config.Sending.MaxAttachmentSize; maxSize > 0 && int64(len(raw)) > maxSize {
		return response.BadRequest(c,
			fmt.Sprintf("attachment exceeds the %d byte limit (sending.max_attachment_size)", maxSize))
	}

	a := &tmodel.Attachment{
		ID:          ids.New(),
		ProjectID:   rc.Project.ID,
		TemplateID:  t.ID,
		Filename:    in.Filename,
		ContentType: in.ContentType,
		Size:        int64(len(raw)),
	}
	if h.Runtime.Blob != nil {
		key := fmt.Sprintf("templates/%s/%s_%s", t.ID, a.ID, blob.SanitizeFilename(in.Filename))
		if err := h.Runtime.Blob.Put(c.UserContext(), key, bytes.NewReader(raw), in.ContentType); err != nil {
			return response.Internal(c, err)
		}

		a.StorageKey = key
	} else {
		a.Content = in.Content
	}

	if err := h.Runtime.Store.Template.PutAttachment(c.UserContext(), a); err != nil {
		if a.StorageKey != "" {
			_ = h.Runtime.Blob.Delete(c.UserContext(), a.StorageKey)
		}

		return response.Internal(c, err)
	}

	return response.Created(c, AttachmentResponse{Attachment: a})
}

// DownloadAttachment streams the bytes from the blob store or the
// inline column.
func (h *Handler) DownloadAttachment(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	a, err := h.Runtime.Store.Template.GetAttachment(c.UserContext(), rc.Project.ID,
		c.Params("id"), c.Params("attId"))
	if err != nil {
		return response.Internal(c, err)
	}

	if a == nil {
		return response.NotFound(c, "attachment not found")
	}

	raw, err := blob.Load(c.UserContext(), h.Runtime.Blob, a.StorageKey, a.Content, a.Filename)
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Attachment(c, a.Filename, a.ContentType, raw)
}

// DeleteAttachment serves DELETE
// /api/v1/templates/:id/attachments/:attId.
func (h *Handler) DeleteAttachment(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	a, err := h.Runtime.Store.Template.GetAttachment(c.UserContext(), rc.Project.ID,
		c.Params("id"), c.Params("attId"))
	if err != nil {
		return response.Internal(c, err)
	}

	if a == nil {
		return response.NotFound(c, "attachment not found")
	}

	if err := h.Runtime.Store.Template.DeleteAttachment(c.UserContext(), rc.Project.ID,
		a.TemplateID, a.ID); err != nil {
		return response.Internal(c, err)
	}

	// Best effort blob cleanup. Emails sent earlier reference the
	// same key, so their attachment downloads stop working - the
	// delivered mail itself already carried the bytes.
	if a.StorageKey != "" && h.Runtime.Blob != nil {
		if derr := h.Runtime.Blob.Delete(c.UserContext(), a.StorageKey); derr != nil {
			h.Runtime.Log.Warn("template: blob cleanup failed", "key", a.StorageKey, "err", derr)
		}
	}

	return response.NoContent(c)
}
