// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/yousysadmin/mailyard/internal/core/clientip"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/core/safetext"

	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/env"
	coreoidc "github.com/yousysadmin/mailyard/internal/core/oidc"
	"github.com/yousysadmin/mailyard/internal/core/response"
	amodel "github.com/yousysadmin/mailyard/internal/models/audit"
	opmodel "github.com/yousysadmin/mailyard/internal/models/oauthprovider"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
)

// resolveProvider loads the provider named by the :slug path segment
// and builds its flow.
//
// A disabled provider is reported as missing rather than forbidden,
// for the same reason cross-project resources are: an unauthenticated
// caller should not be able to enumerate which providers exist. Hidden
// is not checked here - hidden only keeps a provider off the login
// page, it stays reachable by direct URL on purpose.
func (h *Handler) resolveProvider(c fiber.Ctx) (*opmodel.Provider, *coreoidc.Provider, error) {
	slug := c.Params("slug")
	row, err := h.Runtime.Store.OAuthProvider.GetBySlug(c.Context(), slug)
	if err != nil {
		return nil, nil, err
	}

	if row == nil || !row.Enabled {
		return nil, nil, errNoProvider
	}

	flow, err := h.Runtime.OAuth.For(c.Context(), row)
	if err != nil {
		return row, nil, err
	}

	return row, flow, nil
}

// errNoProvider is the sentinel for "no such enabled provider".
var errNoProvider = errors.New("provider not found")

// OAuthStart kicks off the SSO redirect for one provider: mints
// state/nonce/PKCE, sets the short-lived signed cookie, and 302s the
// browser at the IdP's authorization endpoint.
func (h *Handler) OAuthStart(c fiber.Ctx) error {
	row, flow, err := h.resolveProvider(c)
	if errors.Is(err, errNoProvider) {
		return redirectLogin(c, "sso_unknown_provider")
	}

	if err != nil {
		h.auditOIDCFailed(c, "", "provider "+c.Params("slug")+": "+err.Error())

		return redirectLogin(c, "sso_provider_error")
	}

	jwtSecret := []byte(crypto.DeriveKey(h.Runtime.Config.Auth.JWTSecret, crypto.KeyOIDCState))
	redirect, cookieValue := flow.Authorize(jwtSecret, inviteToken(c.Query("invite")))

	// The slug rides in the cookie, not just the URL, so the callback
	// cannot be pointed at a different provider than the one the
	// round-trip started with.
	c.Cookie(buildOIDCStateCookie(c, h.Runtime, row.Slug+"|"+cookieValue, coreoidc.StateCookieTTL))

	return c.Redirect().Status(fiber.StatusFound).To(redirect)
}

// OAuthCallback completes the SSO round-trip: verifies the state
// cookie, exchanges the code, validates the ID token, runs the
// allowlist, finds-or-creates the user, mints a session JWT cookie,
// then redirects to the console. Allowlist denials show a generic
// "access denied" so the failure mode does not leak which leg of the
// allowlist refused.
func (h *Handler) OAuthCallback(c fiber.Ctx) error {
	row, flow, err := h.resolveProvider(c)
	if errors.Is(err, errNoProvider) {
		return redirectLogin(c, "sso_unknown_provider")
	}

	if err != nil {
		h.auditOIDCFailed(c, "", "provider "+c.Params("slug")+": "+err.Error())

		return redirectLogin(c, "sso_provider_error")
	}

	if errParam := c.Query("error"); errParam != "" {
		desc := c.Query("error_description")
		h.auditOIDCFailed(c, "", fmt.Sprintf("idp returned error: %s (%s)", errParam, desc))
		c.Cookie(buildOIDCStateCookie(c, h.Runtime, "", -time.Hour))

		return redirectLogin(c, "sso_idp_error")
	}

	state := c.Query("state")
	code := c.Query("code")
	if state == "" || code == "" {
		h.auditOIDCFailed(c, "", "callback missing state or code")
		c.Cookie(buildOIDCStateCookie(c, h.Runtime, "", -time.Hour))

		return redirectLogin(c, "sso_bad_callback")
	}

	rawCookie := c.Cookies(coreoidc.StateCookie)
	if rawCookie == "" {
		h.auditOIDCFailed(c, "", "state cookie missing on callback")

		return redirectLogin(c, "sso_state_missing")
	}

	cookieSlug, cookieValue, ok := strings.Cut(rawCookie, "|")
	if !ok || cookieSlug != row.Slug {
		// The round-trip started at a different provider. Refuse rather
		// than validating this code against the wrong client.
		h.auditOIDCFailed(c, "", "state cookie belongs to provider "+cookieSlug+", callback is "+row.Slug)
		c.Cookie(buildOIDCStateCookie(c, h.Runtime, "", -time.Hour))

		return redirectLogin(c, "sso_state_mismatch")
	}

	jwtSecret := []byte(crypto.DeriveKey(h.Runtime.Config.Auth.JWTSecret, crypto.KeyOIDCState))
	claims, invite, err := flow.Exchange(c.Context(), jwtSecret, state, code, cookieValue)
	c.Cookie(buildOIDCStateCookie(c, h.Runtime, "", -time.Hour))
	if err != nil {
		h.auditOIDCFailed(c, "", "exchange/verify: "+err.Error())

		return redirectLogin(c, "sso_token_invalid")
	}

	if err := flow.Admit(claims); err != nil {
		h.auditOIDCDenied(c, claims, err.Error())

		return redirectLogin(c, "sso_access_denied")
	}

	u, err := h.findOrCreateOAuthUser(c, row, claims)
	if err != nil {
		h.auditOIDCFailed(c, claims.Email, "user provisioning: "+err.Error())

		// A refused sign-in is not a server fault - auto_register off
		// with no matching account is a configured outcome.
		return redirectLogin(c, "sso_no_account")
	}

	if u.Disabled {
		h.auditOIDCDenied(c, claims, "user disabled")

		return redirectLogin(c, "sso_access_denied")
	}

	if err := h.startSession(c, u, row.ID); err != nil {
		return response.Internal(c, err)
	}

	if err := h.Runtime.Store.User.TouchLastLogin(c.Context(), u.Email); err != nil {
		slog.Warn("auth: touch last login failed", "user_id", u.ID, "err", err)
	}

	// no project is created or joined here, and that is the whole
	// model. Signing in proves who somebody is; it never decides what
	// they may reach.
	//
	// A provider admits a person. An invitation admits them to a project,
	// and carries their role. Keeping those separate is what stops
	// satisfying an allowlist from being a privilege grant, and it means
	// somebody the provider admits simply belongs to nothing until they
	// are invited - which the projects page answers.
	slog.Info("auth: oauth login", "user_id", u.ID, "provider", row.Slug)
	h.Runtime.Audit.Security(c, &amodel.Event{
		Type:       amodel.TypeOIDCLogin,
		ActorID:    u.ID,
		ActorEmail: u.Email,
		Status:     fiber.StatusFound,
		Detail:     "provider " + row.Slug,
	})

	// Back to the invitation the sign-in started from, if it started
	// from one. Otherwise the console root.
	//
	// This is what makes an invitation work in one pass. Accepting needs
	// a session, so a person with no account clicks the mailed link, gets
	// a 401 and is sent to the login page. Land this leg on a fixed path
	// with a start leg that accepts no parameters and the token is
	// dropped on the way, leaving them to go back to their email and
	// click again - which, since a project is reached only by invitation,
	// is the whole first-run experience.
	if invite != "" {
		return c.Redirect().Status(fiber.StatusFound).To(env.ConsolePath + "/invitations?token=" + invite)
	}

	return c.Redirect().Status(fiber.StatusFound).To(env.ConsolePath + "/")
}

// inviteToken keeps only what an invitation token can be: 64 hex
// characters, the shape randomToken() produces.
//
// Anything else becomes empty, so a caller cannot steer the
// post-sign-in redirect. That is the entire defence and it is why this
// carries a TOKEN rather than a return path - the destination is built
// here from a validated value, so there is no open redirect to reason
// about. It also guarantees no dot, which the state serialization
// depends on.
func inviteToken(raw string) string {
	const want = 64
	if len(raw) != want {
		return ""
	}

	for _, r := range raw {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
		default:
			return ""
		}
	}

	return raw
}

// findOrCreateOAuthUser maps the IdP claims to a local users row.
//
// Lookup precedence: the (provider, subject) identity link, which is
// the most stable key and survives an email change at the IdP -> the
// email address, which links a pre-existing local account to this IdP
// on first sign-in -> create, when the provider allows it.
func (h *Handler) findOrCreateOAuthUser(c fiber.Ctx, prov *opmodel.Provider, claims *coreoidc.Claims) (*usermodel.User, error) {
	ctx := c.Context()
	email := strings.ToLower(strings.TrimSpace(claims.Email))

	ident, err := h.Runtime.Store.OAuthIdentity.GetBySubject(ctx, prov.ID, claims.Subject)
	if err != nil {
		return nil, err
	}

	if ident != nil {
		u, err := h.Runtime.Store.User.GetByID(ctx, ident.UserID)
		if err != nil {
			return nil, err
		}

		if u != nil {
			h.linkIdentity(ctx, prov, u, claims)

			return u, nil
		}
		// The identity outlived its user. Fall through and re-resolve
		// by email rather than failing the sign-in.
	}

	if email != "" {
		existing, err := h.Runtime.Store.User.Get(ctx, email)
		if err != nil {
			return nil, err
		}

		if existing != nil {
			// An UNVERIFIED local account is not linked. It used to be,
			// and marked verified on the spot - the IdP had proved the
			// mailbox, after all. But the row was created by whoever
			// typed the address at registration, with a password of
			// their choosing: register victim@corp.com before the
			// victim ever signs in, wait for them to arrive through
			// SSO, and the attacker's password now opens the victim's
			// account. The IdP proved the person owns the MAILBOX, not
			// that they created this ROW. Refused, and the person
			// verifies through the mail they were sent - or an admin
			// removes the squatter.
			if !existing.EmailVerified {
				slog.Warn("auth: refusing to link to an unverified local account",
					"email", email, "provider", prov.Slug)

				return nil, fmt.Errorf("an unverified account already exists for %s", email)
			}

			h.linkIdentity(ctx, prov, existing, claims)

			return existing, nil
		}
	}

	if email == "" {
		return nil, fmt.Errorf("no email claim and no existing identity for sub %q", claims.Subject)
	}

	if !prov.AutoRegister {
		return nil, fmt.Errorf("no account for %s and auto-registration is off for provider %s", email, prov.Slug)
	}

	// The first user of the installation gets admin so whoever set the
	// IdP up can administer the app. Everyone after that is a plain
	// user - satisfying an allowlist must never be a privilege grant.
	count, err := h.Runtime.Store.User.Count(ctx)
	if err != nil {
		return nil, err
	}

	admin := count == 0

	// Verified by construction: the IdP asserted this email, which is
	// a stronger proof of the mailbox than our own confirmation link.
	fresh := &usermodel.User{
		ID:            ids.New(),
		Email:         email,
		AccountType:   usermodel.AccountOIDC,
		Admin:         admin,
		CreatedAt:     time.Now().UTC(),
		EmailVerified: true,
	}
	if err := h.Runtime.Store.User.Put(ctx, fresh); err != nil {
		return nil, err
	}

	h.linkIdentity(ctx, prov, fresh, claims)

	return fresh, nil
}

// linkIdentity records or refreshes the external identity. A failure
// here does not fail the sign-in: the user is already authenticated
// and the next attempt resolves by email again, so the cost is a
// slower lookup rather than a lockout.
func (h *Handler) linkIdentity(ctx context.Context, prov *opmodel.Provider, u *usermodel.User, claims *coreoidc.Claims) {
	err := h.Runtime.Store.OAuthIdentity.Link(ctx, &opmodel.Identity{
		UserID:     u.ID,
		ProviderID: prov.ID,
		Subject:    claims.Subject,
		Email:      claims.Email,
		Name:       claims.Name,
	})
	if err != nil {
		slog.Warn("auth: oauth identity link failed",
			"user_id", u.ID, "provider", prov.Slug, "err", err)
	}
}

// auditOIDCFailed records a sign-in attempt that broke down before an
// allow/deny decision could be made: unknown provider, a bad exchange,
// a state mismatch.
//
// It writes to the security log as well as the server log. A burst of
// state mismatches or token-verify failures against a provider is what a
// replay attempt or a misconfigured IdP looks like, so it belongs in the
// trail its
// sibling auditOIDCDenied was filling.
func (h *Handler) auditOIDCFailed(c fiber.Ctx, email, reason string) {
	// The address is masked in the LOG and kept whole in the audit
	// event below: the trail is the record, the log is shipped.
	slog.Warn("auth: oauth login failed", "email", safetext.MaskAddress(email), "reason", reason, "client_ip", clientip.From(c))
	h.Runtime.Audit.Security(c, &amodel.Event{
		Type:       amodel.TypeOIDCDenied,
		ActorEmail: email,
		Status:     fiber.StatusFound,
		Detail:     reason,
	})
}

func (h *Handler) auditOIDCDenied(c fiber.Ctx, claims *coreoidc.Claims, reason string) {
	slog.Warn("auth: oauth denied", "email", safetext.MaskAddress(claims.Email), "sub", claims.Subject, "reason", reason, "client_ip", clientip.From(c))
	h.Runtime.Audit.Security(c, &amodel.Event{
		Type:       amodel.TypeOIDCDenied,
		ActorEmail: claims.Email,
		Status:     fiber.StatusFound,
		Detail:     reason,
	})
}

// redirectLogin sends the browser back to /login with an error code
// the SPA can render as a banner. Generic codes - never include the
// raw failure reason in the URL.
func redirectLogin(c fiber.Ctx, code string) error {
	return c.Redirect().Status(fiber.StatusFound).To(env.ConsolePath + "/login?err=" + code)
}

func buildOIDCStateCookie(c fiber.Ctx, rt *env.Runtime, value string, ttl time.Duration) *fiber.Cookie {
	return &fiber.Cookie{
		Name:     coreoidc.StateCookie,
		Value:    value,
		Path:     "/",
		HTTPOnly: true,
		SameSite: "Lax", // Lax (not Strict) so the IdP redirect carries it back
		Secure:   cookieSecure(c, rt),
		Expires:  time.Now().Add(ttl),
	}
}
