// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"context"

	"github.com/yousysadmin/mailyard/internal/core/render"
	coretracking "github.com/yousysadmin/mailyard/internal/core/tracking"
	templatedomain "github.com/yousysadmin/mailyard/internal/domain/template"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
	tmodel "github.com/yousysadmin/mailyard/internal/models/template"
)

// TemplateRef addresses a stored template for a send: by id or by
// name, with the requested localization language and the data the
// templates render against. Lenient renders missing data keys as
// zero values instead of failing - campaign sends use it because
// subscriber custom fields are heterogeneous, while transactional
// sends stay strict (a missing key is a caller bug).
type TemplateRef struct {
	ID       string
	Name     string
	Language string
	Data     map[string]any
	Lenient  bool
}

// RenderTemplate resolves the ref (template -> active version ->
// localization with language fallback -> stylesheet) and renders it.
// Sends render strict (missing data keys are caller errors).
func (s *Service) RenderTemplate(ctx context.Context, projID string, ref *TemplateRef) (*render.Output, *tmodel.Template, error) {
	var t *tmodel.Template
	var err error
	switch {
	case ref.ID != "":
		t, err = s.Store.Template.Get(ctx, projID, ref.ID)
	case ref.Name != "":
		t, err = s.Store.Template.GetByName(ctx, projID, ref.Name)
	default:
		return nil, nil, reqErrf("template_id or template_name is required")
	}

	if err != nil {
		return nil, nil, err
	}

	if t == nil {
		return nil, nil, reqErrf("template not found")
	}

	if t.ActiveVersionID == nil {
		return nil, nil, reqErrf("template %q has no active version", t.Name)
	}

	v, err := s.Store.Template.GetVersion(ctx, projID, t.ID, *t.ActiveVersionID)
	if err != nil {
		return nil, nil, err
	}

	if v == nil {
		return nil, nil, reqErrf("template %q active version is missing", t.Name)
	}

	loc, err := templatedomain.ResolveLocalization(ctx, s.Store.Template, projID, t, v.ID, ref.Language)
	if err != nil {
		return nil, nil, err
	}

	if loc == nil {
		return nil, nil, reqErrf("template %q has no localizations", t.Name)
	}

	css := ""
	if v.StylesheetID != nil {
		sheet, err := s.Store.Stylesheet.Get(ctx, projID, *v.StylesheetID)
		if err != nil {
			return nil, nil, err
		}

		if sheet != nil {
			css = sheet.CSS
		}
	}

	// The reserved {{ mailyard_* }} variables render as placeholders
	// that Send swaps for real per-message URLs once the email has an
	// id. Applied HERE, so every surface that renders a template gets
	// them - a transactional send offering half a template's syntax
	// and failing on the other half is not a distinction a caller can
	// see from the template they wrote.
	//
	// Written last, so a caller's own data cannot shadow a reserved
	// name. The campaign runner applies this to its own map as well,
	// because it renders a variant subject separately from here.
	data := coretracking.WithSystemVars(ref.Data)

	behavior := render.MissingKeyError
	if ref.Lenient {
		behavior = render.MissingKeyZero
	}

	r := &render.Renderer{MissingKeyBehavior: behavior}
	out, err := r.Render(&render.Input{
		Subject: loc.Subject, HTML: loc.HTML, Text: loc.Text, CSS: css,
	}, data)
	if err != nil {
		return nil, nil, reqErrf("template render failed: %v", err)
	}

	return out, t, nil
}

// StripSystemLinks removes the reserved-variable placeholders from a
// rendered output.
//
// For the surfaces that render a template and never send it: a preview,
// a dry run, a sandbox capture. None of them produces a message, so
// there is no id to bind a link to - and a placeholder left in place
// would show the reader `/__mailyard_web_view__` as an href, which is
// worse than showing nothing.
//
// Send does the opposite with the same placeholders once the id exists.
func StripSystemLinks(out *render.Output) {
	out.Subject = coretracking.SubstituteSystemLinks(out.Subject, coretracking.Links{})
	out.HTML = coretracking.SubstituteSystemLinks(out.HTML, coretracking.Links{})
	out.Text = coretracking.SubstituteSystemLinks(out.Text, coretracking.Links{})
}

// SendWithTemplate renders the ref and sends the result. The rendered
// content is frozen into the email row, so later template edits never
// change what the log shows was sent.
func (s *Service) SendWithTemplate(ctx context.Context, projID, createdBy, apiKeyID string, ref *TemplateRef, req *SendRequest) (*emailmodel.Email, []string, error) {
	out, t, err := s.RenderTemplate(ctx, projID, ref)
	if err != nil {
		return nil, nil, err
	}

	req.Subject = out.Subject
	req.HTML = out.HTML
	req.Text = out.Text
	req.TemplateName = t.Name
	if err := s.AttachTemplateFiles(ctx, projID, t.ID, req); err != nil {
		return nil, nil, err
	}

	return s.Send(ctx, projID, createdBy, apiKeyID, req)
}

// AttachTemplateFiles appends the template's stored attachments to
// the request. Blob-backed files are referenced by storage key (the
// processor rehydrates them at send time), inline ones carry their
// base64 content directly.
func (s *Service) AttachTemplateFiles(ctx context.Context, projID, templateID string, req *SendRequest) error {
	atts, err := s.Store.Template.ListAttachments(ctx, projID, templateID)
	if err != nil {
		return err
	}

	for _, a := range atts {
		req.Attachments = append(req.Attachments, emailmodel.Attachment{
			Filename:    a.Filename,
			ContentType: a.ContentType,
			Size:        a.Size,
			StorageKey:  a.StorageKey,
			Content:     a.Content,
		})
	}

	return nil
}

// BatchItem is one send inside a batch: template mode (Data plus the
// batch-level template ref) or raw mode (Subject plus a body).
type BatchItem struct {
	To                    []string
	Language              string
	Data                  map[string]any
	Subject               string
	HTML                  string
	Text                  string
	ListUnsubscribeURL    string
	ListUnsubscribeMailto string
	ListUnsubscribePost   bool
}

// BatchResult reports one item's fate. Error is a human-readable
// message ("" on success).
type BatchResult struct {
	Index      int      `json:"index"`
	EmailID    string   `json:"email_id,omitempty"`
	Status     string   `json:"status,omitempty"`
	Suppressed []string `json:"suppressed_recipients,omitempty"`
	Error      string   `json:"error,omitempty"`
}

// SendBatch processes items independently: one bad item does not sink
// the rest, the report carries per-item outcomes. ref is nil for raw
// batches.
func (s *Service) SendBatch(ctx context.Context, projID, createdBy, apiKeyID, from string, ref *TemplateRef, items []BatchItem) []BatchResult {
	results := make([]BatchResult, len(items))
	for i, item := range items {
		results[i] = BatchResult{Index: i}
		req := &SendRequest{
			From:                  from,
			To:                    item.To,
			Subject:               item.Subject,
			HTML:                  item.HTML,
			Text:                  item.Text,
			ListUnsubscribeURL:    item.ListUnsubscribeURL,
			ListUnsubscribeMailto: item.ListUnsubscribeMailto,
			ListUnsubscribePost:   item.ListUnsubscribePost,
		}
		var e *emailmodel.Email
		var blocked []string
		var err error
		if ref != nil {
			itemRef := &TemplateRef{ID: ref.ID, Name: ref.Name, Language: ref.Language, Data: item.Data}
			if item.Language != "" {
				itemRef.Language = item.Language
			}

			e, blocked, err = s.SendWithTemplate(ctx, projID, createdBy, apiKeyID, itemRef, req)
		} else {
			e, blocked, err = s.Send(ctx, projID, createdBy, apiKeyID, req)
		}

		if err != nil {
			results[i].Error = err.Error()
			continue
		}

		results[i].EmailID = e.ID
		results[i].Status = e.Status
		results[i].Suppressed = blocked
	}

	return results
}
