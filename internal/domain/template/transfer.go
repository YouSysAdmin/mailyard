// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package template

import (
	"fmt"

	"github.com/gofiber/fiber/v3"
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
func (h *Handler) Export(c fiber.Ctx) error {
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
			sheet, err := h.Runtime.Store.Stylesheet.Get(c.Context(), rc.Project.ID, *v.StylesheetID)
			if err != nil {
				return response.Internal(c, err)
			}

			if sheet != nil {
				tv.Stylesheet = &transferStylesheet{Name: sheet.Name, CSS: sheet.CSS}
			}
		}

		locs, err := h.Runtime.Store.Template.ListLocalizations(c.Context(), rc.Project.ID, v.ID)
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
func (h *Handler) Import(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	doc, resp, ok := validation.Bind[transferDoc](c)
	if !ok {
		return resp
	}

	if doc.Format != transferFormat {
		return response.BadRequest(c, "unsupported export format, want "+transferFormat)
	}

	existing, err := h.Runtime.Store.Template.GetByName(c.Context(), rc.Project.ID, doc.Template.Name)
	if err != nil {
		return response.Internal(c, err)
	}

	if existing != nil {
		return response.Conflict(c, "a template with this name already exists, rename it in the document")
	}

	if msg := checkVersions(doc.Versions); msg != "" {
		return response.BadRequest(c, msg)
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

	drafts := make([]*tmodel.Draft, 0, len(doc.Versions))
	for _, tv := range doc.Versions {
		d := &tmodel.Draft{
			Version: &tmodel.Version{ID: ids.New(), Version: tv.Version, SampleData: tv.SampleData},
			Active:  tv.Active,
		}
		if tv.Stylesheet != nil {
			d.Stylesheet = &sheetmodel.Stylesheet{
				ID:        ids.New(),
				ProjectID: rc.Project.ID,
				Name:      tv.Stylesheet.Name,
				CSS:       tv.Stylesheet.CSS,
			}
		}

		for _, tl := range tv.Localizations {
			d.Localizations = append(d.Localizations, &tmodel.Localization{
				ID:       ids.New(),
				Language: tl.Language,
				Subject:  tl.Subject,
				HTML:     tl.HTML,
				Text:     tl.Text,
			})
		}

		drafts = append(drafts, d)
	}

	if err := h.Runtime.Store.Template.Create(c.Context(), t, drafts); err != nil {
		return nameTaken(c, err)
	}

	return response.Created(c, TemplateResponse{Template: t})
}

// checkVersions refuses a document that names one version number
// twice, one language twice in a version, or more than one active
// version. Each would otherwise surface as a failed write after
// half the template was stored, or as a silent overwrite.
func checkVersions(versions []transferVersion) string {
	numbers := map[int]bool{}
	active := 0
	for i, v := range versions {
		if v.Version != 0 {
			if numbers[v.Version] {
				return fmt.Sprintf("versions[%d]: version %d appears more than once", i, v.Version)
			}

			numbers[v.Version] = true
		}

		if v.Active {
			active++
		}

		langs := map[string]bool{}
		for _, l := range v.Localizations {
			if langs[l.Language] {
				return fmt.Sprintf("versions[%d]: language %q appears more than once", i, l.Language)
			}

			langs[l.Language] = true
		}
	}

	if active > 1 {
		return "only one version may be active"
	}

	return ""
}
