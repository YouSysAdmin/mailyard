// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package template

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/render"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	tmodel "github.com/yousysadmin/mailyard/internal/models/template"
)

// Handler owns the /api/templates surface (plus versions and
// localizations). Mounted behind requireAuth + requireProject.
type Handler struct {
	Runtime *env.Runtime
}

// putError translates the two refusals PutVersion and PutLocalization
// answer with. The ownership probe reports a vanished (or foreign)
// template as sql.ErrNoRows, which reached response.Internal and
// turned a delete racing an edit into a 500 - and two concurrent
// version creates race MAX(version)+1 into a duplicate key, which is
// a retry for the loser, not an internal error.
func putError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return response.NotFound(c, "template not found")
	case database.UniqueViolation(err, "template_versions_template_id_version_key"):
		return response.Conflict(c, "another version was created at the same time - retry")
	case database.UniqueViolation(err, "template_localizations_version_id_language_key"):
		return response.Conflict(c, "a localization for this language was created at the same time - retry")
	}

	return response.Internal(c, err)
}

// List serves GET /api/v1/templates.
func (h *Handler) List(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	ts, err := h.Runtime.Store.Template.List(c.Context(), rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if ts == nil {
		ts = []*tmodel.Template{}
	}

	return response.Success(c, ListResponse{Templates: ts})
}

// Get serves GET /api/v1/templates/:id.
func (h *Handler) Get(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	t, err := h.Runtime.Store.Template.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if t == nil {
		return response.NotFound(c, "template not found")
	}

	versions, err := h.Runtime.Store.Template.ListVersions(c.Context(), rc.Project.ID, t.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if versions == nil {
		versions = []*tmodel.Version{}
	}

	return response.Success(c, GetResponse{Template: t, Versions: versions})
}

// Create serves POST /api/v1/templates.
func (h *Handler) Create(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[createInput](c)
	if !ok {
		return resp
	}

	existing, err := h.Runtime.Store.Template.GetByName(c.Context(), rc.Project.ID, in.Name)
	if err != nil {
		return response.Internal(c, err)
	}

	if existing != nil {
		return response.Conflict(c, "a template with this name already exists")
	}

	t := &tmodel.Template{
		ID:              ids.New(),
		ProjectID:       rc.Project.ID,
		Name:            in.Name,
		Description:     in.Description,
		DefaultLanguage: in.DefaultLanguage,
		SampleData:      in.SampleData,
		CreatedBy:       callerID(rc),
	}
	if t.DefaultLanguage == "" {
		t.DefaultLanguage = "en"
	}

	if err := h.Runtime.Store.Template.Put(c.Context(), t); err != nil {
		return response.Internal(c, err)
	}

	if in.Subject != "" {
		v := &tmodel.Version{ID: ids.New(), TemplateID: t.ID}
		if err := h.Runtime.Store.Template.PutVersion(c.Context(), rc.Project.ID, v); err != nil {
			return putError(c, err)
		}

		l := &tmodel.Localization{
			ID:        ids.New(),
			VersionID: v.ID,
			Language:  t.DefaultLanguage,
			Subject:   in.Subject,
			HTML:      in.HTML,
			Text:      in.Text,
		}
		if err := h.Runtime.Store.Template.PutLocalization(c.Context(), rc.Project.ID, l); err != nil {
			return putError(c, err)
		}

		if err := h.Runtime.Store.Template.SetActiveVersion(c.Context(), rc.Project.ID, t.ID, v.ID); err != nil {
			return response.Internal(c, err)
		}

		t.ActiveVersionID = new(v.ID)
	}

	return response.Created(c, TemplateResponse{Template: t})
}

// Update serves PATCH /api/v1/templates/:id.
func (h *Handler) Update(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	t, err := h.Runtime.Store.Template.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if t == nil {
		return response.NotFound(c, "template not found")
	}

	in, resp, ok := validation.Bind[updateInput](c)
	if !ok {
		return resp
	}

	if in.Name != "" && in.Name != t.Name {
		other, err := h.Runtime.Store.Template.GetByName(c.Context(), rc.Project.ID, in.Name)
		if err != nil {
			return response.Internal(c, err)
		}

		if other != nil {
			return response.Conflict(c, "a template with this name already exists")
		}

		t.Name = in.Name
	}

	if in.Description != nil {
		t.Description = *in.Description
	}

	if in.DefaultLanguage != "" {
		t.DefaultLanguage = in.DefaultLanguage
	}

	if in.SampleData != nil {
		t.SampleData = *in.SampleData
	}

	t.LastEditedBy = callerID(rc)
	t.UpdatedAt = new(time.Now().UTC())
	if err := h.Runtime.Store.Template.Put(c.Context(), t); err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, TemplateResponse{Template: t})
}

// Delete serves DELETE /api/v1/templates/:id.
func (h *Handler) Delete(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	t, err := h.Runtime.Store.Template.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if t == nil {
		return response.NotFound(c, "template not found")
	}

	// Offloaded attachment objects first, because template_attachments
	// cascades off templates and the storage key lives only in the row
	// going away. Nothing else would ever find them: retention does not
	// look at this table, so a stranded object here is permanent.
	// DeleteAttachment already does this for the one-attachment path.
	ctx := c.Context()
	keys, err := h.Runtime.Store.Template.StorageKeysForTemplate(ctx, rc.Project.ID, t.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if h.Runtime.Blob != nil {
		for _, k := range keys {
			if derr := h.Runtime.Blob.Delete(ctx, k); derr != nil {
				return response.Internal(c, fmt.Errorf("delete attachment %s: %w", k, derr))
			}
		}
	}

	if err := h.Runtime.Store.Template.Delete(ctx, rc.Project.ID, t.ID); err != nil {
		return response.Internal(c, err)
	}

	return response.NoContent(c)
}

// ListVersions serves GET /api/v1/templates/:id/versions.
func (h *Handler) ListVersions(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	versions, err := h.Runtime.Store.Template.ListVersions(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if versions == nil {
		versions = []*tmodel.Version{}
	}

	return response.Success(c, VersionListResponse{Versions: versions})
}

// CreateVersion serves POST /api/v1/templates/:id/versions.
func (h *Handler) CreateVersion(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	t, err := h.Runtime.Store.Template.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if t == nil {
		return response.NotFound(c, "template not found")
	}

	in, resp, ok := validation.Bind[versionInput](c)
	if !ok {
		return resp
	}

	v := &tmodel.Version{
		ID:         ids.New(),
		TemplateID: t.ID,
		SampleData: in.SampleData,
	}
	if in.StylesheetID != "" {
		if ok, resp := h.checkStylesheet(c, rc.Project.ID, in.StylesheetID); !ok {
			return resp
		}

		v.StylesheetID = new(in.StylesheetID)
	}

	if err := h.Runtime.Store.Template.PutVersion(c.Context(), rc.Project.ID, v); err != nil {
		return putError(c, err)
	}

	return response.Created(c, VersionResponse{Version: v})
}

// UpdateVersion serves PATCH
// /api/v1/templates/:id/versions/:versionId.
func (h *Handler) UpdateVersion(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	v, err := h.Runtime.Store.Template.GetVersion(c.Context(), rc.Project.ID, c.Params("id"), c.Params("versionId"))
	if err != nil {
		return response.Internal(c, err)
	}

	if v == nil {
		return response.NotFound(c, "version not found")
	}

	in, resp, ok := validation.Bind[versionInput](c)
	if !ok {
		return resp
	}

	if in.StylesheetID != "" {
		if ok, resp := h.checkStylesheet(c, rc.Project.ID, in.StylesheetID); !ok {
			return resp
		}

		v.StylesheetID = new(in.StylesheetID)
	}

	if in.SampleData != "" {
		v.SampleData = in.SampleData
	}

	if err := h.Runtime.Store.Template.PutVersion(c.Context(), rc.Project.ID, v); err != nil {
		return putError(c, err)
	}

	return response.Success(c, VersionResponse{Version: v})
}

// DeleteVersion refuses to drop the active version - deactivate (or
// activate another) first, so the send path never loses its target.
func (h *Handler) DeleteVersion(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	t, err := h.Runtime.Store.Template.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if t == nil {
		return response.NotFound(c, "template not found")
	}

	versionID := c.Params("versionId")
	if t.ActiveVersionID != nil && *t.ActiveVersionID == versionID {
		return response.Conflict(c, "this version is active, activate another version first")
	}

	v, err := h.Runtime.Store.Template.GetVersion(c.Context(), rc.Project.ID, t.ID, versionID)
	if err != nil {
		return response.Internal(c, err)
	}

	if v == nil {
		return response.NotFound(c, "version not found")
	}

	if err := h.Runtime.Store.Template.DeleteVersion(c.Context(), rc.Project.ID, t.ID, versionID); err != nil {
		return response.Internal(c, err)
	}

	return response.NoContent(c)
}

// Activate serves POST /api/v1/templates/:id/activate/:versionId.
func (h *Handler) Activate(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	err := h.Runtime.Store.Template.SetActiveVersion(c.Context(),
		rc.Project.ID, c.Params("id"), c.Params("versionId"))
	if err != nil {
		if errors.Is(err, ErrVersionMismatch) {
			return response.NotFound(c, "version not found for this template")
		}

		return response.Internal(c, err)
	}

	return response.Success(c, ActiveVersionResponse{ActiveVersionID: c.Params("versionId")})
}

// ListLocalizations serves GET
// /api/v1/templates/:id/versions/:versionId/localizations.
func (h *Handler) ListLocalizations(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	ls, err := h.Runtime.Store.Template.ListLocalizations(c.Context(), rc.Project.ID, c.Params("versionId"))
	if err != nil {
		return response.Internal(c, err)
	}

	if ls == nil {
		ls = []*tmodel.Localization{}
	}

	return response.Success(c, LocalizationListResponse{Localizations: ls})
}

// PutLocalization creates or replaces the version's content for one
// language (upsert keyed on version + language).
func (h *Handler) PutLocalization(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	v, err := h.Runtime.Store.Template.GetVersion(c.Context(), rc.Project.ID, c.Params("id"), c.Params("versionId"))
	if err != nil {
		return response.Internal(c, err)
	}

	if v == nil {
		return response.NotFound(c, "version not found")
	}

	in, resp, ok := validation.Bind[localizationInput](c)
	if !ok {
		return resp
	}

	l := &tmodel.Localization{
		ID:        ids.New(),
		VersionID: v.ID,
		Language:  in.Language,
		Subject:   in.Subject,
		HTML:      in.HTML,
		Text:      in.Text,
		UpdatedAt: new(time.Now().UTC()),
	}
	if err := h.Runtime.Store.Template.PutLocalization(c.Context(), rc.Project.ID, l); err != nil {
		return putError(c, err)
	}

	stored, err := h.Runtime.Store.Template.GetLocalization(c.Context(), rc.Project.ID, v.ID, in.Language)
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, LocalizationResponse{Localization: stored})
}

// DeleteLocalization serves DELETE
// /api/v1/templates/:id/localizations/:localizationId.
func (h *Handler) DeleteLocalization(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	l, err := h.Runtime.Store.Template.GetLocalizationByID(c.Context(), rc.Project.ID, c.Params("localizationId"))
	if err != nil {
		return response.Internal(c, err)
	}

	if l == nil {
		return response.NotFound(c, "localization not found")
	}

	if err := h.Runtime.Store.Template.DeleteLocalization(c.Context(), rc.Project.ID, l.ID); err != nil {
		return response.Internal(c, err)
	}

	return response.NoContent(c)
}

// Preview renders ad hoc content (nothing stored). Missing data keys
// render as zero values so authors see partial output while editing.
func (h *Handler) Preview(c fiber.Ctx) error {
	in, resp, ok := validation.Bind[previewInput](c)
	if !ok {
		return resp
	}

	r := &render.Renderer{MissingKeyBehavior: render.MissingKeyZero}
	out, err := r.Render(&render.Input{
		Subject: in.Subject, HTML: in.HTML, Text: in.Text, CSS: in.CSS,
	}, in.Data)
	if err != nil {
		return response.BadRequest(c, "render failed: "+err.Error())
	}

	return response.Success(c, RenderResponse{Preview: out})
}

// PreviewVersion renders a stored version's localization, using the
// version's sample data when the request carries none.
func (h *Handler) PreviewVersion(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	t, err := h.Runtime.Store.Template.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if t == nil {
		return response.NotFound(c, "template not found")
	}

	v, err := h.Runtime.Store.Template.GetVersion(c.Context(), rc.Project.ID, t.ID, c.Params("versionId"))
	if err != nil {
		return response.Internal(c, err)
	}

	if v == nil {
		return response.NotFound(c, "version not found")
	}

	in, resp, ok := validation.Bind[versionPreviewInput](c)
	if !ok {
		return resp
	}

	loc, err := ResolveLocalization(c.Context(), h.Runtime.Store.Template, rc.Project.ID, t, v.ID, in.Language)
	if err != nil {
		return response.Internal(c, err)
	}

	if loc == nil {
		return response.NotFound(c, "no localization found for this version")
	}

	data := in.Data
	if data == nil {
		data = sampleData(v.SampleData, t.SampleData)
	}

	css, err := h.versionCSS(c, rc.Project.ID, v)
	if err != nil {
		return response.Internal(c, err)
	}

	r := &render.Renderer{MissingKeyBehavior: render.MissingKeyZero}
	out, err := r.Render(&render.Input{
		Subject: loc.Subject, HTML: loc.HTML, Text: loc.Text, CSS: css,
	}, data)
	if err != nil {
		return response.BadRequest(c, "render failed: "+err.Error())
	}

	return response.Success(c, RenderLanguageResponse{Preview: out, Language: loc.Language})
}

// ResolveLocalization picks the version's content for a requested
// language: exact match, then the template default, then "en", then
// the first available. Returns (nil, nil) when the version has no
// localizations at all.
func ResolveLocalization(ctx context.Context, ts store.TemplateStore, projID string, t *tmodel.Template, versionID, language string) (*tmodel.Localization, error) {
	tryLangs := make([]string, 0, 3)
	if language != "" {
		tryLangs = append(tryLangs, language)
	}

	if t.DefaultLanguage != "" {
		tryLangs = append(tryLangs, t.DefaultLanguage)
	}

	tryLangs = append(tryLangs, "en")
	seen := map[string]bool{}
	for _, lang := range tryLangs {
		if seen[lang] {
			continue
		}

		seen[lang] = true
		l, err := ts.GetLocalization(ctx, projID, versionID, lang)
		if err != nil {
			return nil, err
		}

		if l != nil {
			return l, nil
		}
	}

	all, err := ts.ListLocalizations(ctx, projID, versionID)
	if err != nil {
		return nil, err
	}

	if len(all) == 0 {
		return nil, nil
	}

	return all[0], nil
}

// versionCSS loads the stylesheet a version references, tolerating a
// deleted stylesheet (renders unstyled rather than failing).
func (h *Handler) versionCSS(c fiber.Ctx, projID string, v *tmodel.Version) (string, error) {
	if v.StylesheetID == nil {
		return "", nil
	}

	sheet, err := h.Runtime.Store.Stylesheet.Get(c.Context(), projID, *v.StylesheetID)
	if err != nil {
		return "", err
	}

	if sheet == nil {
		return "", nil
	}

	return sheet.CSS, nil
}

// checkStylesheet 404s cross-project or missing stylesheet ids at
// the edge instead of leaving a dangling reference.
//
// It reports whether the stylesheet exists in this project,
// and the response to send when it does not.
//
// Two returns, and the bool is the one that matters. A lone error will
// not do: `response.*` writes the status and returns nil, so every
// refusal here would come back nil, the caller's `if err != nil` would
// never fire, and CreateVersion would store the id anyway. The caller
// would see 201, since the 404 body is written first and the created one
// overwrites it - both go to the same fasthttp response.
//
// Same trap as verifySession, passkeySelf, enrolmentScope and
// refuseCAOverAnAssignedName.
func (h *Handler) checkStylesheet(c fiber.Ctx, projID, id string) (bool, error) {
	sheet, err := h.Runtime.Store.Stylesheet.Get(c.Context(), projID, id)
	if err != nil {
		return false, response.Internal(c, err)
	}

	if sheet == nil {
		return false, response.NotFound(c, "stylesheet not found")
	}

	return true, nil
}

// sampleData parses the first non-empty JSON sample (version first,
// template fallback) into a data map for previews.
func sampleData(candidates ...string) map[string]any {
	for _, raw := range candidates {
		if raw == "" {
			continue
		}

		var m map[string]any
		if err := json.Unmarshal([]byte(raw), &m); err == nil {
			return m
		}
	}

	return map[string]any{}
}

// callerID returns the authenticated user's id, or "" when auth is
// disabled.
func callerID(rc *domain.RequestContext) string {
	if rc == nil || rc.User == nil {
		return ""
	}

	return rc.User.ID
}
