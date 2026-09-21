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
	"net/http"
	"net/url"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/yousysadmin/mailyard/internal/core/safedial"
)

// Config is one resolved provider. The package stays free of imports
// back into core/env, so the Registry is handed what it needs at the
// cli/serve.go startup site.
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
// ID-token verifier. Built and cached by the Registry rather than at
// startup: discovery is a non-trivial cold path, and a provider row is
// editable at runtime, so the cache is keyed on the row instead.
type Provider struct {
	cfg      Config
	oauth2   *oauth2.Config
	verifier *gooidc.IDTokenVerifier

	// userInfoURL is set only on the manual-endpoint path, where
	// there is no discovery document and therefore no signing keys to
	// verify an ID token against. Exchange reads identity from here
	// instead. Empty on the discovery path.
	userInfoURL string

	// client is the guarded client the Registry built - see
	// NewHTTPClient. Carried on the Provider because Exchange runs
	// per sign-in, long after the context discovery used is gone.
	client *http.Client
}

// Verifies reports whether this provider can validate an ID token
// locally. False on the manual-endpoint path, where identity comes
// from the UserInfo endpoint over an already-authenticated channel.
func (p *Provider) Verifies() bool { return p.verifier != nil }

// HTTPTimeout bounds every call this package makes to an identity
// provider. There was none: all four legs ran on http.DefaultClient,
// which has no timeout at all, so an IdP that accepted a connection
// and then went quiet parked the sign-in goroutine indefinitely.
const HTTPTimeout = 15 * time.Second

// NewHTTPClient is the one client every outbound OIDC call uses -
// discovery, the JWKS fetch, the token exchange and the userinfo read.
//
// Built from safedial.Dialer rather than safedial.Client because that
// helper also refuses to FOLLOW REDIRECTS, and an issuer answering its
// discovery URL with a 301 to the canonical spelling is ordinary. The
// guard lives in the dialer's Control hook, which fires after each
// name resolves, so following a redirect is safe: every hop pays the
// check, and a host that resolves publicly once and privately the next
// time is refused on the dial rather than trusted from an earlier
// lookup.
//
// allowPrivate is auth.oidc.allow_private_targets - see there for why
// its default is the opposite of the webhook one.
func NewHTTPClient(allowPrivate bool) *http.Client {
	d := safedial.Dialer(HTTPTimeout, allowPrivate)
	d.KeepAlive = 30 * time.Second

	return &http.Client{
		Timeout: HTTPTimeout,
		Transport: &http.Transport{
			DialContext:           d.DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}

// withClient puts the guarded client where go-oidc and oauth2 both
// look for it.
//
// One wrap covers three of the four legs: gooidc.ClientContext stores
// it under the oauth2.HTTPClient key, which is the same key
// oauth2.Config.Exchange reads, and the RemoteKeySet behind
// Provider.Verifier binds the context it was created with - so
// discovery, the JWKS fetch and the token exchange all dial through
// it. The userinfo read is the fourth and calls the client directly.
//
// A nil client leaves the context alone, so a test that wants the
// default transport gets it.
func withClient(ctx context.Context, client *http.Client) context.Context {
	if client == nil {
		return ctx
	}

	return gooidc.ClientContext(ctx, client)
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
