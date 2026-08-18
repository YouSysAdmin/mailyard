// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package template

import (
	"github.com/gofiber/fiber/v2"
	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain"
	sheetmodel "github.com/yousysadmin/mailyard/internal/models/stylesheet"
	tmodel "github.com/yousysadmin/mailyard/internal/models/template"
)

// transferFormat versions the export document so future shape changes
// can stay importable.
const transferFormat = "mailyard-template-v1"

// Export returns the template as a portable JSON document.
func (h *Handler) Export(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	t, err := h.Runtime.Store.Template.Get(c.UserContext(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if t == nil {
		return response.NotFound(c, "template not found")
	}

	versions, err := h.Runtime.Store.Template.ListVersions(c.UserContext(), rc.Project.ID, t.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	doc := transferDoc{
		Format: transferFormat,
		Template: transferTemplate{
			Name:            t.Name,
			Description:     t.Description,
			DefaultLanguage: t.DefaultLanguage,
			SampleData:      t.SampleData,
		},
	}
	for _, v := range versions {
		tv := transferVersion{
			Version:    v.Version,
			Active:     t.ActiveVersionID != nil && *t.ActiveVersionID == v.ID,
			SampleData: v.SampleData,
		}
		if v.StylesheetID != nil {
			sheet, err := h.Runtime.Store.Stylesheet.Get(c.UserContext(), rc.Project.ID, *v.StylesheetID)
			if err != nil {
				return response.Internal(c, err)
			}

			if sheet != nil {
				tv.Stylesheet = &transferStylesheet{Name: sheet.Name, CSS: sheet.CSS}
			}
		}

		locs, err := h.Runtime.Store.Template.ListLocalizations(c.UserContext(), rc.Project.ID, v.ID)
		if err != nil {
			return response.Internal(c, err)
		}

		for _, l := range locs {
			tv.Localizations = append(tv.Localizations, transferLocalization{
				Language: l.Language, Subject: l.Subject, HTML: l.HTML, Text: l.Text,
			})
		}

		doc.Versions = append(doc.Versions, tv)
	}

	return response.Success(c, ExportResponse{Export: doc})
}

// Import creates a fresh template from an exported document. Names
// must not collide - rename in the document to import a copy.
// Stylesheets are re-created (new ids) so the import never mutates
// existing sheets.
func (h *Handler) Import(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	doc, resp, ok := validation.Bind[transferDoc](c)
	if !ok {
		return resp
	}

	if doc.Format != transferFormat {
		return response.BadRequest(c, "unsupported export format, want "+transferFormat)
	}

	existing, err := h.Runtime.Store.Template.GetByName(c.UserContext(), rc.Project.ID, doc.Template.Name)
	if err != nil {
		return response.Internal(c, err)
	}

	if existing != nil {
		return response.Conflict(c, "a template with this name already exists, rename it in the document")
	}

	t := &tmodel.Template{
		ID:              ids.New(),
		ProjectID:       rc.Project.ID,
		Name:            doc.Template.Name,
		Description:     doc.Template.Description,
		DefaultLanguage: doc.Template.DefaultLanguage,
		SampleData:      doc.Template.SampleData,
		CreatedBy:       callerID(rc),
	}
	if t.DefaultLanguage == "" {
		t.DefaultLanguage = "en"
	}

	if err := h.Runtime.Store.Template.Put(c.UserContext(), t); err != nil {
		return response.Internal(c, err)
	}

	var activeID string
	for _, tv := range doc.Versions {
		v := &tmodel.Version{
			ID:         ids.New(),
			TemplateID: t.ID,
			Version:    tv.Version,
			SampleData: tv.SampleData,
		}
		if tv.Stylesheet != nil {
			sheet := &sheetmodel.Stylesheet{
				ID:        ids.New(),
				ProjectID: rc.Project.ID,
				Name:      tv.Stylesheet.Name,
				CSS:       tv.Stylesheet.CSS,
			}
			if err := h.Runtime.Store.Stylesheet.Put(c.UserContext(), sheet); err != nil {
				return response.Internal(c, err)
			}

			v.StylesheetID = new(sheet.ID)
		}

		if err := h.Runtime.Store.Template.PutVersion(c.UserContext(), rc.Project.ID, v); err != nil {
			return putError(c, err)
		}

		for _, tl := range tv.Localizations {
			l := &tmodel.Localization{
				ID:        ids.New(),
				VersionID: v.ID,
				Language:  tl.Language,
				Subject:   tl.Subject,
				HTML:      tl.HTML,
				Text:      tl.Text,
			}
			if err := h.Runtime.Store.Template.PutLocalization(c.UserContext(), rc.Project.ID, l); err != nil {
				return putError(c, err)
			}
		}

		if tv.Active {
			activeID = v.ID
		}
	}

	if activeID != "" {
		if err := h.Runtime.Store.Template.SetActiveVersion(c.UserContext(), rc.Project.ID, t.ID, activeID); err != nil {
			return response.Internal(c, err)
		}

		t.ActiveVersionID = new(activeID)
	}

	return response.Created(c, TemplateResponse{Template: t})
}
