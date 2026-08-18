// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package sandbox

import (
	sbmodel "github.com/yousysadmin/mailyard/internal/models/sandbox"
	scmodel "github.com/yousysadmin/mailyard/internal/models/smtpcredential"
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

type credentialInput struct {
	Name string `json:"name" validate:"required,min=1,max=100" normalize:"trim"`
}

// ----------------------------------------------------------------------------
// Responses
// ----------------------------------------------------------------------------

// ListResponse is a page of captured mail plus the windows that bound
// it, so the screen can say how long a message survives without
// reading the settings endpoint.
type ListResponse struct {
	SandboxEmails []*sbmodel.Email `json:"sandbox_emails"`
	Total         int              `json:"total"`
	Settings      RetentionInfo    `json:"settings"`
}

// RetentionInfo is what bounds the capture table. RetentionDays is
// decided per message at capture time, so changing it governs new mail
// only - MaxMessages is the ring buffer that actually bounds the table.
type RetentionInfo struct {
	RetentionDays int `json:"retention_days"`
	MaxMessages   int `json:"max_messages"`
}

// EmailResponse is one captured message.
type EmailResponse struct {
	SandboxEmail *sbmodel.Email `json:"sandbox_email"`
}

// DeletedResponse reports how many captures were cleared.
type DeletedResponse struct {
	Deleted int64 `json:"deleted"`
}

// SettingsResponse tells the sandbox screen how to connect and what it
// may show.
//
// SandboxOnly is a rendering hint, not a gate: the permission is
// enforced server-side on every request, so a caller who edits this
// has changed their own screen and nothing else. It says the caller
// can reach the sandbox and nothing else in the project, so this page
// is their whole console and should say so rather than sit behind a
// navigation of links that all answer 403.
//
// It is computed from the permissions the caller holds rather than from
// a role name, so a project can write "contractor - sandbox only" for
// itself and the screen recognises it without knowing what it is called.
type SettingsResponse struct {
	Submission    SubmissionInfo `json:"submission"`
	RetentionDays int            `json:"retention_days"`
	MaxMessages   int            `json:"max_messages"`
	SandboxOnly   bool           `json:"sandbox_only"`
}

// SubmissionInfo is where a developer points their SMTP client.
type SubmissionInfo struct {
	Enabled  bool   `json:"enabled"`
	Host     string `json:"host"`
	Addr     string `json:"addr"`
	STARTTLS bool   `json:"starttls"`
}

// CredentialListResponse is the sandbox credentials with the listener
// details beside them.
type CredentialListResponse struct {
	SMTPCredentials []*scmodel.Credential `json:"smtp_credentials"`
	Submission      SandboxListenerInfo   `json:"submission"`
}

// SandboxListenerInfo mirrors the submission listener for the sandbox
// screen.
type SandboxListenerInfo struct {
	Enabled  bool   `json:"enabled"`
	Host     string `json:"host"`
	Port     string `json:"port"`
	STARTTLS bool   `json:"starttls"`
}

// CredentialCreatedResponse carries the plaintext password once.
type CredentialCreatedResponse struct {
	SMTPCredential *scmodel.Credential `json:"smtp_credential"`
	Password       string              `json:"password"`
	Submission     SandboxListenerInfo `json:"submission"`
}

// CredentialResponse is one credential without the password.
type CredentialResponse struct {
	SMTPCredential *scmodel.Credential `json:"smtp_credential"`
}
