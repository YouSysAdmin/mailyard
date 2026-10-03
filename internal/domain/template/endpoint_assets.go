// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package template

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/blob"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/core/paging"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain"
	tmodel "github.com/yousysadmin/mailyard/internal/models/template"
)

// AssetPath is where builder images are served, under the origin root.
const AssetPath = "/assets"

// assetTypes are the image types a builder upload may be, as
// http.DetectContentType names them. SVG is not one: opened directly it
// is a document that runs script.
var assetTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

// ListAssets serves GET /api/v1/template-assets.
func (h *Handler) ListAssets(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	pg := paging.Optional(c)
	found, total, err := h.Runtime.Store.Template.FindAssets(c.Context(), rc.Project.ID, pg.Limit, pg.Offset)
	if err != nil {
		return response.Internal(c, err)
	}

	out := make([]Asset, 0, len(found))
	for _, a := range found {
		out = append(out, h.assetView(a))
	}

	return response.Success(c, AssetListResponse{Assets: out, Total: total})
}

// UploadAsset serves POST /api/v1/template-assets. The same bytes
// uploaded again answer the image already stored, with 200.
func (h *Handler) UploadAsset(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	if h.publicURL() == "" {
		return response.BadRequest(c,
			"server.public_url is not set, and an image in a message needs an absolute address to load from")
	}

	in, resp, ok := validation.Bind[assetInput](c)
	if !ok {
		return resp
	}

	raw, err := base64.StdEncoding.DecodeString(in.Content)
	if err != nil || len(raw) == 0 {
		return response.BadRequest(c, "content must be valid base64")
	}

	if maxSize := h.Runtime.Config.Sending.MaxAttachmentSize; maxSize > 0 && int64(len(raw)) > maxSize {
		return response.BadRequest(c,
			fmt.Sprintf("the image exceeds the %d byte limit (sending.max_attachment_size)", maxSize))
	}

	ctype := http.DetectContentType(raw)
	if !assetTypes[ctype] {
		return response.BadRequest(c, "only PNG, JPEG, GIF and WebP images can be uploaded")
	}

	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	ctx := c.Context()
	existing, err := h.Runtime.Store.Template.GetAssetBySHA256(ctx, rc.Project.ID, digest)
	if err != nil {
		return response.Internal(c, err)
	}

	if existing != nil {
		return response.Success(c, AssetResponse{Asset: h.assetView(existing)})
	}

	a := &tmodel.Asset{
		ID:          ids.New(),
		ProjectID:   rc.Project.ID,
		Filename:    in.Filename,
		ContentType: ctype,
		Size:        int64(len(raw)),
		SHA256:      digest,
		PublicToken: newAssetToken(),
	}
	if h.Runtime.Blob != nil {
		key := fmt.Sprintf("template-assets/%s/%s_%s", a.ProjectID, a.ID, blob.SanitizeFilename(in.Filename))
		if err := h.Runtime.Blob.Put(ctx, key, bytes.NewReader(raw), ctype); err != nil {
			return response.Internal(c, err)
		}

		a.StorageKey = key
	} else {
		a.Content = in.Content
	}

	created, err := h.Runtime.Store.Template.CreateAsset(ctx, a)
	if (err != nil || !created) && a.StorageKey != "" {
		if derr := h.Runtime.Blob.Delete(ctx, a.StorageKey); derr != nil {
			slog.Warn("template asset: drop an unused upload", "key", a.StorageKey, "err", derr)
		}
	}

	if err != nil {
		return response.Internal(c, err)
	}

	if !created {
		// A concurrent upload of the same bytes won.
		existing, err = h.Runtime.Store.Template.GetAssetBySHA256(ctx, rc.Project.ID, digest)
		if err != nil || existing == nil {
			return response.Internal(c, fmt.Errorf("read back the image a concurrent upload stored: %w", err))
		}

		return response.Success(c, AssetResponse{Asset: h.assetView(existing)})
	}

	return response.Created(c, AssetResponse{Asset: h.assetView(a)})
}

// DeleteAsset serves DELETE /api/v1/template-assets/:id. Refused with
// 409 while a template still references the image or a message waiting
// to be sent embeds it.
func (h *Handler) DeleteAsset(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	ctx := c.Context()
	a, err := h.Runtime.Store.Template.GetAsset(ctx, rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if a == nil {
		return response.NotFound(c, "image not found")
	}

	gone, err := h.Runtime.Store.Template.DeleteAsset(ctx, rc.Project.ID, a.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if !gone {
		pending, err := h.Runtime.Store.Template.AssetPending(ctx, rc.Project.ID, a.ID)
		if err != nil {
			return response.Internal(c, err)
		}

		if pending {
			return response.Conflict(c, "a message waiting to be sent embeds this image, delete it once the message is sent")
		}

		return response.Conflict(c, "a template still uses this image, remove it from the template first")
	}

	// The row is gone, so nothing names the object any more. A failed
	// delete only leaves bytes behind.
	if a.StorageKey != "" && h.Runtime.Blob != nil {
		if err := h.Runtime.Blob.Delete(ctx, a.StorageKey); err != nil {
			slog.Warn("template asset: delete the stored image", "key", a.StorageKey, "err", err)
		}
	}

	return response.NoContent(c)
}

// ServeAsset serves GET /assets/:token, publicly: mail clients fetch
// it with no session. The token is the only authorization and is
// tied to no secret, so a rekey never breaks an image already
// delivered.
func (h *Handler) ServeAsset(c fiber.Ctx) error {
	token := c.Params("token")
	if token == "" || len(token) > 64 {
		return response.NotFound(c, "not found")
	}

	a, err := h.Runtime.Store.Template.GetAssetByToken(c.Context(), token)
	if err != nil {
		return response.Internal(c, err)
	}

	if a == nil {
		return response.NotFound(c, "not found")
	}

	raw, err := blob.Load(c.Context(), h.Runtime.Blob, a.StorageKey, a.Content, a.Filename)
	if err != nil {
		return response.Internal(c, err)
	}

	c.Set(fiber.HeaderContentType, a.ContentType)
	c.Set(fiber.HeaderXContentTypeOptions, "nosniff")
	c.Set(fiber.HeaderCacheControl, "public, max-age=31536000, immutable")
	c.Set(fiber.HeaderContentDisposition,
		fmt.Sprintf("inline; filename=%q", blob.SanitizeFilename(a.Filename)))

	return c.Send(raw)
}

func (h *Handler) publicURL() string {
	return strings.TrimRight(h.Runtime.Config.Server.PublicURL, "/")
}

func (h *Handler) assetView(a *tmodel.Asset) Asset {
	return Asset{
		ID:          a.ID,
		Filename:    a.Filename,
		ContentType: a.ContentType,
		Size:        a.Size,
		URL:         h.publicURL() + AssetPath + "/" + a.PublicToken,
		CreatedAt:   a.CreatedAt,
	}
}

// newAssetToken is 32 random bytes, URL-safe.
func newAssetToken() string {
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)

	return base64.RawURLEncoding.EncodeToString(raw)
}
