// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpcredential

import (
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

// createInput mints a credential. Unlike an API key there are no
// scopes and no expiry - submission is the only thing it grants.
type createInput struct {
	Name       string   `json:"name"        validate:"required,min=1,max=100" normalize:"trim"`
	AllowedIPs []string `json:"allowed_ips" validate:"omitempty,max=20,dive,ipcidr"`

	// SMTPGroup routes everything submitted with this credential to a
	// named server pool, by slug. Empty uses the project's default
	// group. An SMTP client has nowhere to put a routing field, so
	// this is the only place the choice can be made.
	SMTPGroup string `json:"smtp_group" validate:"omitempty,max=100" normalize:"normalize"`

	// Sandbox mints a credential whose submissions are captured into
	// the project sandbox rather than delivered. Fixed at creation:
	// see the same field on the API key input.
	Sandbox bool `json:"sandbox"`
}

// ----------------------------------------------------------------------------
// Responses
// ----------------------------------------------------------------------------

// ListResponse is the project's submission credentials together with
// the listener settings, so the screen can render connection details
// without a second call to a config endpoint.
type ListResponse struct {
	SMTPCredentials []*scmodel.Credential `json:"smtp_credentials"`
	Submission      ListenerInfo          `json:"submission"`
}

// ListenerInfo is what a client needs to connect: where the submission
// listener is and whether it upgrades.
type ListenerInfo struct {
	Enabled  bool   `json:"enabled"`
	Host     string `json:"host"`
	Port     string `json:"port"`
	STARTTLS bool   `json:"starttls"`
}

// CreatedResponse carries the one and only sight of the password.
// Only its hash is stored, so it cannot be recovered afterwards.
type CreatedResponse struct {
	SMTPCredential *scmodel.Credential `json:"smtp_credential"`
	Password       string              `json:"password"`
	Submission     ListenerInfo        `json:"submission"`
}

// CredentialResponse is one credential record, without the password.
type CredentialResponse struct {
	SMTPCredential *scmodel.Credential `json:"smtp_credential"`
}
