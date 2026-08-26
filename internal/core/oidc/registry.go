// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package oidc

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	opmodel "github.com/yousysadmin/mailyard/internal/models/oauthprovider"
)

// consoleAPIPrefix is where the console's own api is mounted, and it
// is a COPY of env.ConsolePath + "/api".
//
// A copy because this package is deliberately free of imports back
// into core/env - env constructs the Registry, so the dependency only
// runs one way. The copy is pinned by TestOIDCPathsFollowTheConsole,
// which lives in the external test package and so may import both:
// getting these out of step would register the wrong redirect URI with
// every identity provider, and the symptom is a callback 404 at the
// end of a round-trip that otherwise looked fine.
const consoleAPIPrefix = "/app/api"

// StartPath and CallbackPath are the sign-in routes for one provider
// slug. Defined here, next to the flow, so the route table and the
// admin screen's "register this redirect URI" hint cannot drift apart.
func StartPath(slug string) string { return consoleAPIPrefix + "/auth/oauth/" + slug + "/start" }

// CallbackPath is where a provider redirects back to, for the slug
// given. The IdP has this registered, so changing the shape
// invalidates every configured provider.
func CallbackPath(slug string) string {
	return consoleAPIPrefix + "/auth/oauth/" + slug + "/callback"
}

// Registry builds and caches a Provider per configured IdP.
//
// Discovery is an outbound HTTPS round-trip to the issuer, far too
// slow to repeat on every sign-in, but providers are now editable at
// runtime so it cannot be done once at startup either. The cache key
// includes the row's UpdatedAt, so an edit invalidates the entry by
// construction - there is no way to save a change and keep serving
// the old configuration.
type Registry struct {
	mu    sync.Mutex
	built map[string]*cached

	// publicURL is the externally reachable base, used to derive the
	// redirect URI. The IdP has to reach us by our external name,
	// which the inbound request's Host may not be behind a proxy.
	publicURL string
}

type cached struct {
	provider *Provider
	stamp    time.Time
}

// NewRegistry builds a Registry.
func NewRegistry(publicURL string) *Registry {
	return &Registry{
		built:     make(map[string]*cached),
		publicURL: strings.TrimRight(publicURL, "/"),
	}
}

// For returns a flow-ready Provider for the stored row, discovering
// against the issuer on first use.
func (r *Registry) For(ctx context.Context, p *opmodel.Provider) (*Provider, error) {
	r.mu.Lock()
	if c, ok := r.built[p.Slug]; ok && c.stamp.Equal(p.UpdatedAt) {
		r.mu.Unlock()

		return c.provider, nil
	}

	r.mu.Unlock()

	built, err := r.build(ctx, p)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	r.built[p.Slug] = &cached{provider: built, stamp: p.UpdatedAt}
	r.mu.Unlock()

	return built, nil
}

// Forget drops a cached provider, so a fresh discovery runs on the
// next sign-in. Called after an admin edit or delete.
func (r *Registry) Forget(slug string) {
	r.mu.Lock()
	delete(r.built, slug)
	r.mu.Unlock()
}

// RedirectURL is the callback this installation will present to the
// IdP, and the value the operator must register there.
func (r *Registry) RedirectURL(slug string) string {
	return r.publicURL + CallbackPath(slug)
}

func (r *Registry) build(ctx context.Context, p *opmodel.Provider) (*Provider, error) {
	// Say this out loud once per build rather than silently accepting
	// it. The sign-in path links an IdP identity to an existing local
	// account by email address, so with verification off the IdP is
	// trusted to never hand out an address it has not confirmed. That
	// is a real assumption for a private Keycloak and a bad one for a
	// public provider, and the operator is the only one who can tell
	// the difference.
	if !p.RequireEmailVerified {
		slog.Warn("oidc: provider does not require email_verified, so an unverified address from this IdP can take over a local account with the same email",
			"provider", p.Slug)
	}

	cfg := Config{
		ClientID:             p.ClientID,
		ClientSecret:         p.ClientSecret,
		RedirectURL:          r.RedirectURL(p.Slug),
		Scopes:               p.EffectiveScopes(),
		RequireEmailVerified: p.RequireEmailVerified,
		AllowedDomains:       p.AllowedDomains,
		AllowedEmails:        p.AllowedEmails,
		GroupsClaim:          p.GroupsClaim,
		AllowedGroups:        p.AllowedGroups,
	}

	issuer := p.EffectiveIssuer()
	if issuer != "" {
		cfg.Issuer = issuer
		prov, err := gooidc.NewProvider(ctx, issuer)

		// A trailing slash is the classic way to lose an hour here.
		// The spec compares issuers as EXACT strings, and providers
		// disagree with their own operators about the slash -
		// JumpCloud's canonical issuer is
		// "https://oauth.id.jumpcloud.com/", which nobody types. The
		// discovery document is the authority on its own issuer, so
		// when the only complaint is that mismatch, retry once with
		// the flipped spelling. Gated on the mismatch error rather
		// than retrying every failure, or an unreachable issuer would
		// wait out two timeouts to report one outage.
		if err != nil && strings.Contains(err.Error(), "did not match") {
			flipped := strings.TrimSuffix(issuer, "/")
			if flipped == issuer {
				flipped = issuer + "/"
			}

			if prov2, err2 := gooidc.NewProvider(ctx, flipped); err2 == nil {
				prov, err = prov2, nil
				cfg.Issuer = flipped
			}
		}

		if err == nil {
			return &Provider{
				cfg:      cfg,
				oauth2:   oauthConfig(cfg, prov.Endpoint()),
				verifier: prov.Verifier(&gooidc.Config{ClientID: p.ClientID}),
			}, nil
		}

		// Discovery failed. Explicit endpoints are the documented
		// fallback, so use them when the operator supplied them and
		// only report the discovery error when they did not.
		if p.AuthURL == "" || p.TokenURL == "" {
			return nil, fmt.Errorf("discovery against %s failed and no auth_url/token_url is configured: %w", issuer, err)
		}
	}

	if p.AuthURL == "" || p.TokenURL == "" {
		return nil, fmt.Errorf("provider %q has neither an issuer nor explicit endpoints", p.Slug)
	}

	// Manual endpoints. Without a discovery document there are no
	// signing keys to fetch, so the ID token cannot be verified
	// locally and Exchange falls back to the UserInfo endpoint - see
	// Provider.Exchange.
	return &Provider{
		cfg: cfg,
		oauth2: oauthConfig(cfg, oauth2.Endpoint{
			AuthURL:  p.AuthURL,
			TokenURL: p.TokenURL,
		}),
		userInfoURL: p.UserInfoURL,
	}, nil
}

func oauthConfig(cfg Config, ep oauth2.Endpoint) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     ep,
		Scopes:       cfg.Scopes,
	}
}

// TestResult is what the admin "test" button reports.
type TestResult struct {
	OK          bool     `json:"ok"`
	Error       string   `json:"error,omitempty"`
	Issuer      string   `json:"issuer,omitempty"`
	AuthURL     string   `json:"auth_url,omitempty"`
	TokenURL    string   `json:"token_url,omitempty"`
	Discovered  bool     `json:"discovered"`
	RedirectURL string   `json:"redirect_url"`
	Scopes      []string `json:"scopes"`
	Warnings    []string `json:"warnings,omitempty"`
}

// Test resolves the provider without caching the result and reports
// what discovery returned. It cannot verify the client secret - only
// a real sign-in exercises that - so the result says what was
// reachable, not that logins will succeed.
func (r *Registry) Test(ctx context.Context, p *opmodel.Provider) TestResult {
	res := TestResult{
		RedirectURL: r.RedirectURL(p.Slug),
		Scopes:      p.EffectiveScopes(),
		Issuer:      p.EffectiveIssuer(),
	}
	if p.ClientID == "" {
		res.Warnings = append(res.Warnings, "no client id is set")
	}

	if p.ClientSecret == "" {
		res.Warnings = append(res.Warnings, "no client secret is set")
	}

	built, err := r.build(ctx, p)
	if err != nil {
		res.Error = err.Error()

		return res
	}

	res.OK = true
	res.AuthURL = built.oauth2.Endpoint.AuthURL
	res.TokenURL = built.oauth2.Endpoint.TokenURL
	res.Discovered = built.verifier != nil
	if !res.Discovered {
		res.Warnings = append(res.Warnings,
			"no discovery document, so the id token cannot be verified locally and the userinfo endpoint is used instead")
		if p.UserInfoURL == "" {
			res.Warnings = append(res.Warnings,
				"set a userinfo_url, otherwise sign-in has no way to read the user's identity")
		}
	}

	// The two admission settings whose unsafe value is silent. The
	// process log says both at build time, but the person deciding is
	// looking at this screen, not at the log.
	if !p.RequireEmailVerified {
		res.Warnings = append(res.Warnings,
			"require_email_verified is off, so an address this IdP has not verified links to the local account carrying it")
	}

	if p.AutoRegister && len(p.AllowedDomains) == 0 && len(p.AllowedEmails) == 0 && len(p.AllowedGroups) == 0 {
		res.Warnings = append(res.Warnings,
			"auto_register is on with no allowed_domains, allowed_emails or allowed_groups, so every account this IdP holds can create one here")
	}

	if !p.Enabled {
		res.Warnings = append(res.Warnings, "provider is disabled, so it is not offered at sign-in")
	}

	if p.Hidden {
		res.Warnings = append(res.Warnings, "provider is hidden, so it is reachable only by direct URL")
	}

	return res
}
