// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package oauthprovider

import "time"

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

// upsertInput is the create and update body.
//
// ClientSecret is a pointer so an update can distinguish "leave the
// stored secret alone" (field absent) from "set it to this" (field
// present). Without that, every edit of an unrelated field from a UI
// that cannot read the secret back would blank it.
type upsertInput struct {
	Name     string  `json:"name"      validate:"required,min=1,max=100" normalize:"trim"`
	Slug     string  `json:"slug"      validate:"omitempty,max=60"       normalize:"trim"`
	Type     string  `json:"type"      validate:"omitempty,max=20"       normalize:"trim"`
	ClientID string  `json:"client_id" validate:"omitempty,max=400"      normalize:"trim"`
	Secret   *string `json:"client_secret" validate:"omitzero,max=1000"`

	Issuer      string `json:"issuer"       validate:"omitempty,url,max=400" normalize:"trim"`
	AuthURL     string `json:"auth_url"     validate:"omitempty,url,max=400" normalize:"trim"`
	TokenURL    string `json:"token_url"    validate:"omitempty,url,max=400" normalize:"trim"`
	UserInfoURL string `json:"userinfo_url" validate:"omitempty,url,max=400" normalize:"trim"`

	Scopes []string `json:"scopes" validate:"omitempty,max=20"`

	Enabled      *bool `json:"enabled"`
	Hidden       *bool `json:"hidden"`
	AutoRegister *bool `json:"auto_register"`

	RequireEmailVerified *bool    `json:"require_email_verified"`
	AllowedDomains       []string `json:"allowed_domains" validate:"omitempty,max=50"`
	AllowedEmails        []string `json:"allowed_emails"  validate:"omitempty,max=200"`
	GroupsClaim          string   `json:"groups_claim"    validate:"omitempty,max=100" normalize:"trim"`
	AllowedGroups        []string `json:"allowed_groups"  validate:"omitempty,max=100"`
}

// ----------------------------------------------------------------------------
// Responses
// ----------------------------------------------------------------------------

// ProviderView is one provider as the API describes it.
//
// A declared struct rather than a fiber.Map, and not for tidiness. Two
// things keep the secret off the wire: json:"-" on the model, and this
// view naming its fields explicitly. Only the first is checkable through
// a map - TestTheDocumentPublishesNoSecrets finds sealed columns by
// reflection, and reflection sees nothing inside a map. Declared, the
// one response carrying an identity provider's configuration is covered
// like every other.
//
// has_secret rather than the secret itself, so the console can render
// "configured" without ever receiving it.
type ProviderView struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	Slug                 string   `json:"slug"`
	Type                 string   `json:"type"`
	ClientID             string   `json:"client_id"`
	HasSecret            bool     `json:"has_secret"`
	Issuer               string   `json:"issuer"`
	AuthURL              string   `json:"auth_url"`
	TokenURL             string   `json:"token_url"`
	UserInfoURL          string   `json:"userinfo_url"`
	Scopes               []string `json:"scopes"`
	Enabled              bool     `json:"enabled"`
	Hidden               bool     `json:"hidden"`
	AutoRegister         bool     `json:"auto_register"`
	RequireEmailVerified bool     `json:"require_email_verified"`
	AllowedDomains       []string `json:"allowed_domains"`
	AllowedEmails        []string `json:"allowed_emails"`
	GroupsClaim          string   `json:"groups_claim"`
	AllowedGroups        []string `json:"allowed_groups"`

	// Usable tells the operator why an enabled provider is still not on
	// the login page, which is otherwise a silent puzzle.
	Usable      bool      `json:"usable"`
	CallbackURL string    `json:"callback_url"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ListResponse is the configured identity providers.
type ListResponse struct {
	Providers []ProviderView `json:"providers"`
}

// ProviderResponse is one provider.
type ProviderResponse struct {
	Provider ProviderView `json:"provider"`
}

// DeletedResponse confirms a removal.
type DeletedResponse struct {
	Deleted bool `json:"deleted"`
}

// TestResponse is the outcome of probing a provider's discovery
// document, so an operator finds out here rather than from a user who
// cannot sign in.
type TestResponse struct {
	Test any `json:"test"`
}
