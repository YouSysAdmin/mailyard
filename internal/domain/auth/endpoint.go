// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package auth

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/yousysadmin/mailyard/internal/core/clientip"
	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/authenticator"
	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/edition"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/memo"
	coreoidc "github.com/yousysadmin/mailyard/internal/core/oidc"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain"
	amodel "github.com/yousysadmin/mailyard/internal/models/audit"
	sessmodel "github.com/yousysadmin/mailyard/internal/models/session"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
)

// Handler owns the /app/api/auth surface: sign-in and sign-out,
// registration and its verification mail, password reset, passkeys,
// TOTP, OIDC start and callback, and the caller's own sessions.
//
// Session-only by construction - none of it is reachable with an API
// key, because none of it is an operation a machine performs.
type Handler struct {
	Runtime *env.Runtime

	// providers memoizes the login page's provider list - see Info.
	providers *memo.Value[[]LoginProvider]
}

// infoMemo is how long the provider list stands. Five seconds keeps
// "an operator who just enabled a provider sees the button on the next
// refresh" true to within a breath, and makes a flood of this open
// endpoint cost the database one query per five seconds rather than
// one per request.
const infoMemo = 5 * time.Second

// Login validates email + password against the users table, sets the
// session cookie, and returns the public user record.
// Uniform error for missing user / wrong password / disabled account so the
// response never leaks which leg failed.
func (h *Handler) Login(c fiber.Ctx) error {
	if !h.Runtime.Config.Auth.Local.Enabled {
		return response.BadRequest(c, "local login is disabled")
	}

	in, resp, ok := validation.Bind[loginInput](c)
	if !ok {
		return resp
	}

	u, err := h.Runtime.Store.User.Get(c.Context(), in.Email)
	if err != nil {
		return response.Internal(c, err)
	}

	// Run the bcrypt verify on every leg (including unknown email or
	// disabled account) so response timing does not leak which leg failed.
	// The dummy verify is a real cost-12 bcrypt over an unguessable secret -
	// it consumes the same time as the real path and always returns false.
	if u == nil || u.Disabled {
		_ = authenticator.VerifyDummyPassword(in.Password)
		h.recordLoginFailure(c, u, in.Email, "unknown or disabled account")

		return response.Unauthorized(c, "invalid credentials")
	}

	if !authenticator.VerifyPassword(u.PasswordHash, in.Password) {
		h.recordLoginFailure(c, u, in.Email, "wrong password")

		return response.Unauthorized(c, "invalid credentials")
	}

	// Signup verification. Checked only after the password, so the
	// flag leaks nothing to someone guessing credentials. The refusal
	// stands even if system mail has since been switched off - the
	// resend endpoint explains, and an admin can mark the account
	// verified by hand - because letting a config toggle silently
	// admit every pending stranger would be worse.
	if !u.EmailVerified {
		return c.Status(fiber.StatusUnauthorized).JSON(LoginChallenge{
			Error:                "confirm your email address first - check your mailbox for the link",
			RequiresVerification: true,
		})
	}

	// Second factor. The password already checked out, so telling the
	// client 2FA is required leaks nothing an attacker with valid
	// credentials would not learn on the next step anyway.
	if u.TOTPEnabled {
		if in.TOTPCode == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(LoginChallenge{
				Error:       "two-factor code required",
				Requires2FA: true,
			})
		}

		if !h.consumeSecondFactor(c, u, in.TOTPCode) {
			h.recordLoginFailure(c, u, in.Email, "wrong two-factor code")

			return response.Unauthorized(c, "invalid credentials")
		}
	}

	if err := h.startSession(c, u, ""); err != nil {
		return response.Internal(c, err)
	}

	if err := h.Runtime.Store.User.TouchLastLogin(c.Context(), u.Email); err != nil {
		slog.Warn("auth: touch last login failed", "user_id", u.ID, "err", err)
	}

	// No project is minted here, or anywhere else. Creating one per
	// sign-in leaves a shared deployment collecting an empty tenant per
	// account on top of the project its members were invited into.
	// Belonging to no project is an ordinary state, and the console
	// answers it with the projects page and a create button.
	slog.Info("auth: login", "user_id", u.ID)
	h.Runtime.Audit.Security(c, &amodel.Event{
		Type:       amodel.TypeLoginSucceeded,
		ActorID:    u.ID,
		ActorEmail: u.Email,
		Status:     fiber.StatusOK,
	})

	return response.Success(c, UserResponse{User: u})
}

// Register creates an account from the public signup form and signs
// it in. The route only exists when auth.registration_enabled is set
// (routes.go), so with the default config this surface is absent, not
// merely refused.
//
// The duplicate-email answer is a deliberate enumeration trade-off:
// unlike password reset, a signup form that silently succeeds against
// an existing address strands the person on a login they cannot pass.
// The login-tier rate limit on the route is what keeps the oracle slow.
func (h *Handler) Register(c fiber.Ctx) error {
	in, resp, ok := validation.Bind[registerInput](c)
	if !ok {
		return resp
	}

	existing, err := h.Runtime.Store.User.Get(c.Context(), in.Email)
	if err != nil {
		return response.Internal(c, err)
	}

	if existing != nil {
		return response.Conflict(c, "an account with this email already exists")
	}

	hash, err := authenticator.HashPassword(in.Password)
	if err != nil {
		return response.Internal(c, err)
	}

	// Verification is possible only when the install can send mail.
	// Without system mail the account is live immediately - the old
	// behavior - because a gate nobody can pass is a lockout, not a
	// check.
	needsVerify := h.Runtime.SystemMail.Enabled()
	u := &usermodel.User{
		ID:            ids.New(),
		Email:         in.Email,
		AccountType:   usermodel.AccountLocal,
		PasswordHash:  hash,
		EmailVerified: !needsVerify,
	}
	if err := h.Runtime.Store.User.Put(c.Context(), u); err != nil {
		return response.Internal(c, err)
	}

	// No project here either. A self-registered account belongs to
	// nothing until somebody invites it or it creates one, which is
	// what the projects page is for.
	slog.Info("auth: registered", "user_id", u.ID, "verification_required", needsVerify)
	h.Runtime.Audit.Security(c, &amodel.Event{
		Type:       amodel.TypeRegistered,
		ActorID:    u.ID,
		ActorEmail: u.Email,
		Status:     fiber.StatusCreated,
	})

	if needsVerify {
		if err := h.sendVerificationMail(c.Context(), u, clientip.From(c)); err != nil {
			return response.Internal(c, err)
		}

		// No session: the mailbox has to answer first. The link signs
		// the account in when clicked.
		return response.Created(c, RegisterPendingResponse{
			VerificationRequired: true,
			Message:              "Check your mailbox for a confirmation link to finish signing up.",
		})
	}

	// Sign the account in right away. The form collected the password
	// seconds ago - bouncing to the login page to retype it would be pure ceremony.
	if err := h.startSession(c, u, ""); err != nil {
		return response.Internal(c, err)
	}

	if err := h.Runtime.Store.User.TouchLastLogin(c.Context(), u.Email); err != nil {
		slog.Warn("auth: touch last login failed", "user_id", u.ID, "err", err)
	}

	return response.Created(c, UserResponse{User: u})
}

// Logout revokes the session row and clears the cookie.
//
// Both halves matter. Clearing the cookie alone would leave the token
// usable by anyone who had already copied it, because the JWT itself
// stays signed and unexpired - revoking the row named by its jti is
// what actually ends the session, and rt.Sessions.Invalidate makes
// this node agree immediately instead of within the cache TTL.
func (h *Handler) Logout(c fiber.Ctx) error {
	// Deliberately not behind requireAuth: signing out must succeed
	// even when the session has already expired or been revoked
	// elsewhere, otherwise the console can get stuck holding a dead
	// cookie it cannot clear. So the handler resolves the session
	// itself instead of reading it from the request context.
	if sess, email := h.currentSession(c); sess != nil {
		if _, err := h.Runtime.Store.Session.Revoke(c.Context(), sess.UserID, sess.ID); err != nil {
			slog.Warn("auth: revoking session on logout failed", "session_id", sess.ID, "err", err)
		}

		h.Runtime.Sessions.Invalidate(sess.ID)
		h.Runtime.Audit.Security(c, &amodel.Event{
			Type:       amodel.TypeLogout,
			ActorID:    sess.UserID,
			ActorEmail: email,
			Status:     fiber.StatusNoContent,
		})
	}

	c.Cookie(buildSessionCookie(c, h.Runtime, "", -time.Hour))

	return response.NoContent(c)
}

// currentSession resolves the session behind the request's own
// cookie or bearer token. Returns nil for anything unusable - a
// logout with no valid token still clears the cookie and succeeds.
func (h *Handler) currentSession(c fiber.Ctx) (*sessmodel.Session, string) {
	raw := c.Cookies(SessionCookie)
	if raw == "" {
		raw, _ = strings.CutPrefix(c.Get(fiber.HeaderAuthorization), "Bearer ")
	}
	if raw == "" {
		return nil, ""
	}

	claims, err := authenticator.ParseToken(
		crypto.DeriveKey(h.Runtime.Config.Auth.JWTSecret, crypto.KeySession), raw)
	if err != nil || claims.SessionID == "" {
		return nil, ""
	}

	sess, err := h.Runtime.Store.Session.Get(c.Context(), claims.SessionID)
	if err != nil || sess == nil {
		return nil, ""
	}

	return sess, claims.Email
}

// recordLoginFailure files a failed sign-in. This is the one place
// the trail records a rejected request: for authentication, the
// failure IS the event an operator needs to see. The address is
// recorded even when no account matches - a burst against unknown
// addresses is exactly the pattern worth spotting.
func (h *Handler) recordLoginFailure(c fiber.Ctx, u *usermodel.User, attempted, reason string) {
	ev := &amodel.Event{
		Type:       amodel.TypeLoginFailed,
		ActorEmail: attempted,
		Status:     fiber.StatusUnauthorized,
		Detail:     reason,
	}
	if u != nil {
		ev.ActorID = u.ID
	}

	h.Runtime.Audit.Security(c, ev)
}

// Me returns the user resolved from the session cookie, or a hint
// that auth is disabled entirely. Shapes:
//   - 200 {"user": {...}}          authenticated
//   - 200 {"auth_disabled": true}  auth.disabled=true - no user concept
//   - 401 {"error": "..."}         auth is on but caller has no session
func (h *Handler) Me(c fiber.Ctx) error {
	if h.Runtime.Config.Auth.Disabled {
		return response.Success(c, AuthDisabledResponse{AuthDisabled: true})
	}

	rc := domain.GetRequestContext(c)
	if rc == nil || rc.User == nil {
		return response.Unauthorized(c, "not authenticated")
	}

	return response.Success(c, UserResponse{User: rc.User})
}

// Info returns the auth posture for the unauthenticated login page so
// it can render the right control (local form vs OIDC button vs
// nothing when auth.disabled). Open endpoint - not gated.
func (h *Handler) Info(c fiber.Ctx) error {
	cfg := h.Runtime.Config.Auth
	if cfg.Disabled {
		// The edition travels on this branch too - it is the same
		// question, and a console on an install with auth off reads it
		// from the same call.
		return response.Success(c, AuthDisabledResponse{AuthDisabled: true, Edition: edition.Name})
	}

	// Identity providers are read from the database, memoized for
	// infoMemo: one small query, but on an OPEN endpoint, where a flood
	// of it is pool pressure the signed-in requests queue behind.
	//
	// Only name, slug, and type are exposed. The list is public by
	// necessity - the login page needs it before anyone has signed in -
	// so it must not carry client ids, issuers, or allowlists.
	if h.providers == nil {
		h.providers = memo.New[[]LoginProvider](infoMemo)
	}

	list, err := h.providers.Get(func() ([]LoginProvider, error) {
		provs, err := h.Runtime.Store.OAuthProvider.ListLoginable(context.Background())
		if err != nil {
			return nil, err
		}

		list := make([]LoginProvider, 0, len(provs))
		for _, p := range provs {
			list = append(list, LoginProvider{
				Name:     p.Name,
				Slug:     p.Slug,
				Type:     p.Type,
				StartURL: coreoidc.StartPath(p.Slug),
			})
		}

		return list, nil
	})
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, AuthInfoResponse{
		Edition:      edition.Name,
		LocalEnabled: cfg.Local.Enabled,
		OIDCEnabled:  len(list) > 0,
		Providers:    list,

		// Forgot-password needs somewhere to send the link, so it is
		// only offered when system mail is configured. The login page
		// hides the link rather than showing one that always errors.
		PasswordResetEnabled: cfg.Local.Enabled && h.Runtime.SystemMail.Enabled(),
		RegistrationEnabled:  cfg.Local.Enabled && cfg.RegistrationEnabled,

		// Whether the sign-in page should offer the passkey button at
		// all. It says nothing about whether any account has one -
		// that would be an enumeration oracle on an open endpoint.
		PasskeysEnabled: h.passkeysAvailable(),
	})
}

func sessionTTL(rt *env.Runtime) time.Duration {
	if rt.Config.Auth.SessionTTL == "" {
		return 12 * time.Hour
	}

	d, err := time.ParseDuration(rt.Config.Auth.SessionTTL)
	if err != nil || d <= 0 {
		return 12 * time.Hour
	}

	return d
}

// buildSessionCookie centralizes the cookie shape so login + logout
// produce a matching pair.
// HttpOnly + SameSite=Strict + Secure when the operator advertises
// the tool over HTTPS. Path=/ so the SPA + every /api/* call shares it.
//
// Secure comes from server.public_url OR the connection - see
// cookieSecure for why it takes both.
func buildSessionCookie(c fiber.Ctx, rt *env.Runtime, value string, ttl time.Duration) *fiber.Cookie {
	return &fiber.Cookie{
		Name:     SessionCookie,
		Value:    value,
		Path:     "/",
		HTTPOnly: true,
		SameSite: "Strict",
		Secure:   cookieSecure(c, rt),
		Expires:  time.Now().Add(ttl),
	}
}

// cookieSecure reports whether the cookie may only travel over TLS:
// the operator's advertised public URL is HTTPS, OR this request
// arrived over it. The first half is what keeps the flag on behind a
// TLS-terminating proxy that talks plain HTTP upstream. The second is
// what protects an HTTPS install whose public_url was left empty or on
// http - without it the session cookie went out without Secure and
// one plain-http fetch to the host leaked it. c.Secure() reads the
// forwarded scheme only from a trusted proxy, so it cannot be turned
// OFF by a header. False for plain-http local dev.
func cookieSecure(c fiber.Ctx, rt *env.Runtime) bool {
	return c.Secure() || strings.HasPrefix(strings.ToLower(rt.Config.Server.PublicURL), "https://")
}
