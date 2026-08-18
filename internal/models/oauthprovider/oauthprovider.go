// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package oauthprovider is the runtime-configured identity provider
// record and the link between an external identity and a local user.
//
// Providers are platform level, not per project. A user account is
// a platform entity that then holds project memberships, so "who
// may sign in" cannot sensibly be a tenant decision. Per-project
// SSO is a separate feature.
package oauthprovider

import (
	"strings"
	"time"
)

// Types. oidc discovers its endpoints from the issuer. google is the
// same protocol with endpoints already known, so an operator only has
// to paste a client id and secret.
const (
	TypeOIDC   = "oidc"
	TypeGoogle = "google"
)

// GoogleIssuer is the discovery issuer for the google type. Held here
// rather than in the flow package so the model stays the single
// answer to "what does type google mean".
const GoogleIssuer = "https://accounts.google.com"

// ValidTypes enumerates assignable types for input validation.
var ValidTypes = map[string]struct{}{
	TypeOIDC: {}, TypeGoogle: {},
}

// DefaultScopes is what a provider gets when the operator names none.
// openid is required for an ID token at all, email is what we key the
// local account on.
var DefaultScopes = []string{"openid", "email", "profile"}

// Provider is one configured identity provider.
//
// ClientSecret carries json:"-" so it cannot leave through the API,
// and the store encrypts it at rest through the crypto service. The
// admin UI reports whether a secret is set, never its value.
type Provider struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	Type         string `json:"type"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"-"`

	// Issuer drives discovery. Required for oidc, implied for google.
	Issuer string `json:"issuer,omitempty"`

	// AuthURL / TokenURL / UserInfoURL are the manual fallback for an
	// IdP that does not publish a discovery document. Ignored when
	// discovery succeeds.
	AuthURL     string `json:"auth_url,omitempty"`
	TokenURL    string `json:"token_url,omitempty"`
	UserInfoURL string `json:"userinfo_url,omitempty"`

	Scopes []string `json:"scopes"`

	Enabled bool `json:"enabled"`

	// Hidden keeps a working provider off the login page, for staged
	// rollout or a break-glass provider reached by direct URL.
	Hidden bool `json:"hidden"`

	// AutoRegister creates a local user on first sign-in. Off means
	// the account must already exist, which is how an operator runs
	// invite-only SSO.
	AutoRegister bool `json:"auto_register"`

	// Admission controls, evaluated against the verified ID token.
	// All empty means the IdP itself is the only gate.
	RequireEmailVerified bool     `json:"require_email_verified"`
	AllowedDomains       []string `json:"allowed_domains"`
	AllowedEmails        []string `json:"allowed_emails"`
	GroupsClaim          string   `json:"groups_claim,omitempty"`
	AllowedGroups        []string `json:"allowed_groups"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// HasSecret reports whether a client secret is stored, so the console
// can show "set" without the value ever being serialized.
func (p *Provider) HasSecret() bool { return p.ClientSecret != "" }

// EffectiveIssuer resolves the issuer to discover against. Type
// google carries its own, so an operator never has to know it.
func (p *Provider) EffectiveIssuer() string {
	if p.Type == TypeGoogle {
		return GoogleIssuer
	}

	return p.Issuer
}

// EffectiveScopes returns the configured scopes, or the default set.
func (p *Provider) EffectiveScopes() []string {
	if len(p.Scopes) == 0 {
		return DefaultScopes
	}

	return p.Scopes
}

// Usable reports whether the provider is complete enough to attempt a
// sign-in. A provider that is enabled but not usable would offer the
// user a button that can only fail, so the login list filters on this
// as well as Enabled.
func (p *Provider) Usable() bool {
	if p.ClientID == "" || p.ClientSecret == "" {
		return false
	}

	if p.EffectiveIssuer() != "" {
		return true
	}

	// No issuer means no discovery, so the endpoints must be explicit.
	return p.AuthURL != "" && p.TokenURL != ""
}

// NormalizeSlug reduces a name to a URL-safe slug. The slug appears
// in /api/auth/oauth/<slug>/start, so it has to survive a path
// segment without escaping.
func NormalizeSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == ' ' || r == '.':
			// Collapse runs of separators so "my  idp" and "my-idp"
			// land on the same slug rather than two near-identical ones.
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}

	return strings.TrimRight(b.String(), "-")
}

// Identity links an external subject to a local user.
//
// This is a table rather than a column on users because the subject
// is only unique WITHIN an issuer. Two providers can legitimately
// hand out the same sub, and one person can hold an identity at
// several providers, so the pair (provider, subject) is the key.
type Identity struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	ProviderID  string    `json:"provider_id"`
	Subject     string    `json:"subject"`
	Email       string    `json:"email,omitempty"`
	Name        string    `json:"name,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	LastLoginAt time.Time `json:"last_login_at"`
}
