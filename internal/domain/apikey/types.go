// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package apikey

import (
	akmodel "github.com/yousysadmin/mailyard/internal/models/apikey"
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

// createInput mints a key. ExpiresAt is RFC 3339 and optional
// (keys without expiry are legal but the UI should nudge).
type createInput struct {
	Name string `json:"name" validate:"required,min=1,max=100" normalize:"trim"`

	// Permissions are catalogue strings ("emails:write"), or the
	// single wildcard "*". Omitting them mints a key that may do
	// nothing, which is the safe reading of an unstated intent.
	Permissions []string `json:"permissions" validate:"omitempty,max=42"`
	AllowedIPs  []string `json:"allowed_ips" validate:"omitempty,max=20,dive,ipcidr"`
	ExpiresAt   string   `json:"expires_at"  validate:"omitempty"`

	// Sandbox mints a key whose sends are captured into the project
	// sandbox rather than delivered. Fixed at creation and not
	// editable afterwards - a key that could be switched over would
	// let one careless PATCH turn every test into a real send, or
	// every real send into a test nobody notices.
	Sandbox bool `json:"sandbox"`
}

// ----------------------------------------------------------------------------
// Responses
// ----------------------------------------------------------------------------

// ListResponse is the project's keys. Only the prefix of each is
// stored, so a list can never hand back a usable credential.
type ListResponse struct {
	APIKeys []*akmodel.Key `json:"api_keys"`
}

// CreatedResponse carries the one and only sight of the token.
//
// Only hex(sha256(token)) is kept, so nobody - including an operator
// with database access - can recover it afterwards. The prefix on the
// key record exists so a list can still tell keys apart.
type CreatedResponse struct {
	APIKey *akmodel.Key `json:"api_key"`
	Token  string       `json:"token"`
}

// APIKeyResponse is one key record, without any secret.
type APIKeyResponse struct {
	APIKey *akmodel.Key `json:"api_key"`
}

// ----------------------------------------------------------------------------
// Platform credentials
// ----------------------------------------------------------------------------

// adminCreateInput mints a platform credential. No permissions field:
// a key on this surface is admin or it does not exist.
type adminCreateInput struct {
	Name       string   `json:"name"        validate:"required,min=1,max=100" normalize:"trim"`
	AllowedIPs []string `json:"allowed_ips" validate:"omitempty,max=20,dive,ipcidr"`
	ExpiresAt  string   `json:"expires_at"  validate:"omitempty"`
}

// AdminListResponse is every platform credential. Not project scoped -
// there is nothing to scope it by.
type AdminListResponse struct {
	AdminAPIKeys []*akmodel.Admin `json:"admin_api_keys"`
}

// AdminCreatedResponse carries the one and only sight of the token.
type AdminCreatedResponse struct {
	AdminAPIKey *akmodel.Admin `json:"admin_api_key"`
	Token       string         `json:"token"`
}

// AdminAPIKeyResponse is one platform credential, without any secret.
type AdminAPIKeyResponse struct {
	AdminAPIKey *akmodel.Admin `json:"admin_api_key"`
}
