// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package template

import (
	"github.com/yousysadmin/mailyard/internal/core/render"
	tmodel "github.com/yousysadmin/mailyard/internal/models/template"
)

// The wire types of this domain: what requests carry in and what
// responses carry out, in one file.
//
// They live here rather than beside the handlers that use them so a
// reader answering "what does this endpoint accept and return" has one
// place to look. The types in internal/models are the stored shapes -
// these are what crosses the wire, and the two are allowed to differ.

// ----------------------------------------------------------------------------
// Requests
// ----------------------------------------------------------------------------

// createInput makes a template. When Subject is set an initial
// version with a default-language localization is created and
// activated in the same call, so simple templates are usable with one
// request.
type createInput struct {
	Name            string `json:"name"             validate:"required,min=1,max=100"  normalize:"trim"`
	Description     string `json:"description"      validate:"omitempty,max=500"       normalize:"trim"`
	DefaultLanguage string `json:"default_language" validate:"omitempty,min=2,max=10"  normalize:"normalize"`
	SampleData      string `json:"sample_data"      validate:"omitempty,max=65536,json"`
	Subject         string `json:"subject"          validate:"omitempty,max=1000"`
	HTML            string `json:"html"             validate:"omitempty,max=1048576"`
	Text            string `json:"text"             validate:"omitempty,max=1048576"`
}

type updateInput struct {
	Name            string  `json:"name"             validate:"omitempty,min=1,max=100" normalize:"trim"`
	Description     *string `json:"description"      validate:"omitzero,max=500"`
	DefaultLanguage string  `json:"default_language" validate:"omitempty,min=2,max=10"  normalize:"normalize"`
	SampleData      *string `json:"sample_data"      validate:"omitzero,max=65536,json"`
}

type versionInput struct {
	StylesheetID string `json:"stylesheet_id" validate:"omitempty,uuid"`
	SampleData   string `json:"sample_data"   validate:"omitempty,max=65536,json"`
}

type localizationInput struct {
	Language string `json:"language" validate:"required,min=2,max=10"  normalize:"normalize"`
	Subject  string `json:"subject"  validate:"required,max=1000"`
	HTML     string `json:"html"     validate:"omitempty,max=1048576"`
	Text     string `json:"text"     validate:"omitempty,max=1048576"`
}

// previewInput renders arbitrary content without touching the store.
type previewInput struct {
	Subject string         `json:"subject" validate:"required,max=1000"`
	HTML    string         `json:"html"    validate:"omitempty,max=1048576"`
	Text    string         `json:"text"    validate:"omitempty,max=1048576"`
	CSS     string         `json:"css"     validate:"omitempty,max=262144"`
	Data    map[string]any `json:"data"`
}

// versionPreviewInput renders a stored version's localization.
type versionPreviewInput struct {
	Language string         `json:"language" validate:"omitempty,min=2,max=10" normalize:"normalize"`
	Data     map[string]any `json:"data"`
}

// attachmentInput uploads one file as base64 JSON, matching the send
// endpoints' attachment shape. Any file type is accepted - the size
// cap is sending.max_attachment_size.
type attachmentInput struct {
	Filename    string `json:"filename"     validate:"required,min=1,max=255"`
	ContentType string `json:"content_type" validate:"omitempty,max=255"`
	Content     string `json:"content"      validate:"required"`
}

// transferDoc is the self-contained export of one template:
// stylesheets referenced by versions are inlined so the document
// imports cleanly into another project or install.
type transferDoc struct {
	Format   string            `json:"format"   validate:"required,eqfield=Format|required"`
	Template transferTemplate  `json:"template" validate:"required"`
	Versions []transferVersion `json:"versions" validate:"omitempty,dive"`
}

type transferTemplate struct {
	Name            string `json:"name"             validate:"required,min=1,max=100" normalize:"trim"`
	Description     string `json:"description"      validate:"omitempty,max=500"`
	DefaultLanguage string `json:"default_language" validate:"omitempty,min=2,max=10"`
	SampleData      string `json:"sample_data"      validate:"omitempty,max=65536"`
}

type transferVersion struct {
	Version       int                    `json:"version"`
	Active        bool                   `json:"active"`
	SampleData    string                 `json:"sample_data" validate:"omitempty,max=65536"`
	Stylesheet    *transferStylesheet    `json:"stylesheet,omitempty"`
	Localizations []transferLocalization `json:"localizations" validate:"omitempty,dive"`
}

type transferStylesheet struct {
	Name string `json:"name" validate:"required,max=100"`
	CSS  string `json:"css"  validate:"omitempty,max=262144"`
}

type transferLocalization struct {
	Language string `json:"language" validate:"required,min=2,max=10"`
	Subject  string `json:"subject"  validate:"required,max=1000"`
	HTML     string `json:"html"     validate:"omitempty,max=1048576"`
	Text     string `json:"text"     validate:"omitempty,max=1048576"`
}

// ----------------------------------------------------------------------------
// Responses
// ----------------------------------------------------------------------------

// Response types for the routes reachable on the machine API, so the
// OpenAPI document is reflected from them rather than transcribed.

// ListResponse is every template in the project.
type ListResponse struct {
	Templates []*tmodel.Template `json:"templates"`
}

// GetResponse is one template together with its version history, so
// the caller can pick a version without a second call.
type GetResponse struct {
	Template *tmodel.Template  `json:"template"`
	Versions []*tmodel.Version `json:"versions"`
}

// Console response types. The machine API's live in responses.go -
// these are the authoring surface, which the console owns.

// TemplateResponse is one template.
type TemplateResponse struct {
	Template *tmodel.Template `json:"template"`
}

// VersionListResponse is a template's revision history.
type VersionListResponse struct {
	Versions []*tmodel.Version `json:"versions"`
}

// VersionResponse is one revision.
type VersionResponse struct {
	Version *tmodel.Version `json:"version"`
}

// ActiveVersionResponse confirms which revision a template now sends.
type ActiveVersionResponse struct {
	ActiveVersionID string `json:"active_version_id"`
}

// LocalizationListResponse is a version's translations.
type LocalizationListResponse struct {
	Localizations []*tmodel.Localization `json:"localizations"`
}

// LocalizationResponse is one translation. The renderable content
// lives here rather than on the version - a single-language template
// is one with a single localization.
type LocalizationResponse struct {
	Localization *tmodel.Localization `json:"localization"`
}

// RenderResponse is a rendered preview.
type RenderResponse struct {
	Preview *render.Output `json:"preview"`
}

// RenderLanguageResponse is a rendered preview that names which
// localization produced it, since the request may have fallen back to
// the default language.
type RenderLanguageResponse struct {
	Preview  *render.Output `json:"preview"`
	Language string         `json:"language"`
}

// AttachmentListResponse is the files appended to every send of a
// template.
type AttachmentListResponse struct {
	Attachments []*tmodel.Attachment `json:"attachments"`
}

// AttachmentResponse is one stored attachment.
type AttachmentResponse struct {
	Attachment *tmodel.Attachment `json:"attachment"`
}

// ExportResponse is the portable document for one template, with any
// stylesheet inlined so it imports into an installation that has never seen it.
type ExportResponse struct {
	Export transferDoc `json:"export"`
}
