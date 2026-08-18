// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package template is the stored email template: a named container
// (Template) holding immutable numbered Versions, each localized per
// language (Localization). Exactly one version may be active - the
// one template sends resolve.
package template

import "time"

// Template is the named container. Name is unique per project and
// is how the send API addresses it. SampleData is a JSON object
// string editors use to preview.
type Template struct {
	ID              string     `json:"id"`
	ProjectID       string     `json:"project_id"`
	Name            string     `json:"name"`
	Description     string     `json:"description,omitempty"`
	DefaultLanguage string     `json:"default_language"`
	ActiveVersionID *string    `json:"active_version_id,omitempty"`
	SampleData      string     `json:"sample_data,omitempty"`
	CreatedBy       string     `json:"created_by,omitempty"`
	LastEditedBy    string     `json:"last_edited_by,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       *time.Time `json:"updated_at,omitempty"`
}

// Version is one numbered revision of a template. Content lives in
// its per-language Localizations. StylesheetID points at the CSS
// inlined into every localization's HTML at render time.
type Version struct {
	ID           string    `json:"id"`
	TemplateID   string    `json:"template_id"`
	Version      int       `json:"version"`
	StylesheetID *string   `json:"stylesheet_id,omitempty"`
	SampleData   string    `json:"sample_data,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// Localization is the renderable content of one version in one
// language. Subject is required, HTML and Text are each optional but
// at least one should be present to produce a sendable email.
type Localization struct {
	ID        string     `json:"id"`
	VersionID string     `json:"version_id"`
	Language  string     `json:"language"`
	Subject   string     `json:"subject"`
	HTML      string     `json:"html,omitempty"`
	Text      string     `json:"text,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// Attachment is a file bound to a template, included on every send
// that renders it. Content is inline base64 only when no blob store
// is configured and is never serialized to API responses - the
// download endpoint streams the bytes instead.
type Attachment struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	TemplateID  string    `json:"template_id"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content_type,omitempty"`
	Size        int64     `json:"size"`
	StorageKey  string    `json:"storage_key,omitempty"`
	Content     string    `json:"-"`
	CreatedAt   time.Time `json:"created_at"`
}
