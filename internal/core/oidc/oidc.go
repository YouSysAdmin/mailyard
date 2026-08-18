// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package oidc wires the operator-console SSO flow: discovery from
// the issuer, the authorization-code redirect with PKCE, and ID-token
// verification on callback. Also evaluates the allowlist surface
// (allowed_domains / allowed_emails / allowed_groups + email_verified).
//
// State / nonce / PKCE verifier ride the round-trip in a short-lived
// signed cookie - no server-side state, no extra tables. Cookie is
// HMAC-signed with auth.jwt_secret so we don't introduce a second
// long-lived secret.
package oidc

import (
	"context"
	"fmt"
	"net/url"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Config mirrors the YAML/env-resolved AuthOIDCConfig so the package
// stays free of imports back into core/env. Construct via FromEnv()
// at the cli/serve.go startup site.
type Config struct {
	Issuer               string
	ClientID             string
	ClientSecret         string
	RedirectURL          string
	Scopes               []string
	RequireEmailVerified bool
	AllowedDomains       []string
	AllowedEmails        []string
	GroupsClaim          string
	AllowedGroups        []string
}

// StateCookie is the short-lived cookie carrying state+nonce+verifier
// across the IdP round-trip. Cleared on callback success/failure.
const StateCookie = "mailyard_oidc_state"

// StateCookieTTL caps how long the user can sit on the IdP page
// before the round-trip cookie expires. 10 minutes mirrors the
// industry default and is generous for SSO flows that involve MFA
// prompts or push notifications.
const StateCookieTTL = 10 * time.Minute

// Provider bundles the IdP discovery result + an oauth2.Config and an
// ID-token verifier. Build once at startup and reuse across requests
// - discovery is a non-trivial cold path.
type Provider struct {
	cfg      Config
	oauth2   *oauth2.Config
	verifier *gooidc.IDTokenVerifier

	// userInfoURL is set only on the manual-endpoint path, where
	// there is no discovery document and therefore no signing keys to
	// verify an ID token against. Exchange reads identity from here
	// instead. Empty on the discovery path.
	userInfoURL string
}

// Verifies reports whether this provider can validate an ID token
// locally. False on the manual-endpoint path, where identity comes
// from the UserInfo endpoint over an already-authenticated channel.
func (p *Provider) Verifies() bool { return p.verifier != nil }

// New constructs a Provider against the issuer's discovery document.
// Returns an error if the issuer is unreachable or returns a malformed
// configuration - the operator should see this at startup, not at
// the first SSO attempt.
func New(ctx context.Context, cfg Config) (*Provider, error) {
	prov, err := gooidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery (%s): %w", cfg.Issuer, err)
	}

	return &Provider{
		cfg: cfg,
		oauth2: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     prov.Endpoint(),
			Scopes:       cfg.Scopes,
		},
		verifier: prov.Verifier(&gooidc.Config{ClientID: cfg.ClientID}),
	}, nil
}

// Config returns the resolved config - handlers consult it to read
// the allowlist surface + RequireEmailVerified.
func (p *Provider) Config() Config { return p.cfg }

// IssuerHost returns the host portion of the issuer URL for display
// purposes ("Sign in with <host>"). Falls back to the raw issuer.
func (p *Provider) IssuerHost() string {
	u, err := url.Parse(p.cfg.Issuer)
	if err != nil || u.Host == "" {
		return p.cfg.Issuer
	}

	return u.Host
}
