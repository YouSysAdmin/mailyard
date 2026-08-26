// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"log/slog"
	"mime"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	slogfiber "github.com/samber/slog-fiber"

	"github.com/yousysadmin/mailyard/internal/core/authenticator"
	"github.com/yousysadmin/mailyard/internal/core/clientip"
	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/domain"
	authdomain "github.com/yousysadmin/mailyard/internal/domain/auth"
	"github.com/yousysadmin/mailyard/internal/domain/project"
	"github.com/yousysadmin/mailyard/pkg"
)

// requestContext builds the per-request *domain.RequestContext and
// stores it under domain.ContextKey so every downstream handler can
// pull it with domain.GetRequestContext(c). It also attaches the
// context to the slog-fiber access log as a lazily-evaluated group,
// so the log line written at request end carries the user requireAuth
// resolved mid-request.
//
// Registered after slog-fiber: GetRequestID reads the ID that
// middleware minted at request start.
//
// It is also where the caller's address is decided, once per request:
// resolver.Stamp stores it on the request so clientip.From answers the
// same thing to the rate limiter, the api key allowlist, the audit trail
// and every log line, without any of them re-reading a header.
func requestContext(rt *env.Runtime, resolver *clientip.Resolver) fiber.Handler {
	return func(c fiber.Ctx) error {
		rc := &domain.RequestContext{
			Ctx:        c,
			AppName:    pkg.AppName,
			AppVersion: pkg.Version,
			ClientIP:   resolver.Stamp(c),
			Path:       c.Path(),
			RequestID:  slogfiber.GetRequestID(c),
			SSL:        c.Secure() || strings.HasPrefix(strings.ToLower(rt.Config.Server.PublicURL), "https://"),
		}
		c.Locals(domain.ContextKey, rc)
		slogfiber.AddCustomAttributes(c, slog.Any("context", rc))

		return c.Next()
	}
}

// requireAuth gates protected /api/* routes on a valid session
// cookie.
// No-op when auth.disabled=true.
//
// The middleware also writes the resolved *user.User into the
// RequestContext so handlers (e.g. /api/auth/me) can pull it without
// a second DB read, and so the access log line carries the user.
//
// AUTHORIZATION TIER: mostly "in or out". Every authenticated,
// non-disabled user has full access to protected routes, except the
// user-management surface (/api/users), which additionally sits
// behind requireAdmin. Gate privileged route groups at registration
// time (routes.go) with requireAdmin - not with hand-rolled checks
// inside individual handlers, which creates a misleading "this is
// gated" signal across the rest.
func requireAuth(rt *env.Runtime) fiber.Handler {
	return func(c fiber.Ctx) error {
		if ok, resp := stampSession(c, rt); !ok {
			return resp
		}

		return c.Next()
	}
}

// stampSession resolves the session and writes the user onto the
// RequestContext, without advancing the chain.
//
// Split out of requireAuth so machineAuth can run it as one branch of
// a decision. Two return values for the reason verifySession has two:
// the response helpers write the status and return nil.
func stampSession(c fiber.Ctx, rt *env.Runtime) (bool, error) {
	if rt.Config.Auth.Disabled {
		return true, nil
	}

	// Tokens carry a jti naming a row in the sessions table, which
	// is what makes them revocable. A token without one predates
	// session tracking: resolveSession accepts it rather than
	// logging every existing operator out on upgrade. It simply is
	// not revocable, exactly as before.
	claims, ok := resolveSession(c, rt)
	if !ok {
		// One message for "no token", "bad token" and "revoked
		// session" alike. The previous version appended the JWT
		// parse error, which told an attacker probing with forged
		// tokens exactly which part they got wrong.
		return false, response.Unauthorized(c, "authentication required")
	}

	u, err := rt.Store.User.GetByID(c.Context(), claims.UserID)
	if err != nil {
		return false, response.Internal(c, err)
	}

	if u == nil || u.Disabled {
		return false, response.Unauthorized(c, "user not found or disabled")
	}

	if rc := domain.GetRequestContext(c); rc != nil {
		rc.User = u
		rc.SessionID = claims.SessionID
	}

	return true, nil
}

// sessionTouchInterval throttles last_seen_at writes.
const sessionTouchInterval = 5 * time.Minute

// requireAdmin gates a route group on the caller being admin-tier
// (User.IsAdmin, one stored flag). Stack it after
// requireAuth - it reads the user requireAuth resolved into the
// RequestContext. No-op when auth is disabled: with no user concept
// there is no tier to enforce.
func requireAdmin(rt *env.Runtime) fiber.Handler {
	return func(c fiber.Ctx) error {
		if rt.Config.Auth.Disabled {
			return c.Next()
		}

		if !domain.GetRequestContext(c).IsPlatformAdmin() {
			return response.Forbidden(c, "admin privileges required")
		}

		return c.Next()
	}
}

// requireProjectCreation gates making a new project on the platform
// setting, which is off by default - so an installation starts out as
// one where an admin makes the projects and everybody else arrives by
// invitation.
//
// Middleware rather than a check inside Create, per the rule: the gate
// is readable beside the route it guards, and it cannot be forgotten
// by whoever adds a second way in.
//
// It reads the same project.MayCreate the list endpoint answers the
// console with, so the button the console offers and the request the
// server accepts cannot drift apart.
func requireProjectCreation(rt *env.Runtime) fiber.Handler {
	return func(c fiber.Ctx) error {
		if !project.MayCreate(rt, domain.GetRequestContext(c)) {
			return response.Forbidden(c,
				"creating projects is restricted to platform administrators on this installation")
		}

		return c.Next()
	}
}

// extractToken pulls the session token from the cookie first, then
// falls back to a Bearer header for CLI / curl callers.
func extractToken(c fiber.Ctx) string {
	if v := c.Cookies(authdomain.SessionCookie); v != "" {
		return v
	}

	return bearerToken(c)
}

// requirePageAuth is requireAuth for a browser-facing page rather
// than an API call.
//
// The API helpers answer an unauthenticated request with a JSON 401,
// which is right for XHR and useless in an address bar - the reader
// gets a blob of JSON instead of a login form. This sends them to the
// console login with a return path instead, and falls back to the
// JSON answer for callers that did not ask for HTML.
func requirePageAuth(rt *env.Runtime) fiber.Handler {
	return func(c fiber.Ctx) error {
		if rt.Config.Auth.Disabled {
			return c.Next()
		}

		if pageSessionIsLive(c, rt) {
			return c.Next()
		}

		if !wantsHTML(c) {
			return response.Unauthorized(c, "authentication required")
		}

		return c.Redirect().Status(fiber.StatusFound).To(env.ConsolePath + "/login?next=" + url.QueryEscape(c.OriginalURL()))
	}
}

// pageSessionIsLive validates the request's token and, when it names
// a session, that the session has not been revoked. Same rules as the
// API path - it shares resolveSession - but reports a plain bool so
// the caller decides between a redirect and a 401.
func pageSessionIsLive(c fiber.Ctx, rt *env.Runtime) bool {
	_, ok := resolveSession(c, rt)

	return ok
}

// resolveSession is the one place that turns a request into a
// verified session: parse the token, then confirm the row behind its
// jti is still live, warming the cache on the way.
//
// It exists because three call sites - the API gate, the docs page
// gate, and logout - each carried their own copy, and they had
// already drifted on the case that matters most: what an empty jti
// means. A token minted before session tracking has none and is
// therefore unrevocable, which every copy has to treat as "accept" or
// an upgrade signs everybody out. One copy getting that backwards is
// a lockout, and one copy getting it backwards the other way is an
// unrevocable session nobody can kill.
//
// ok=false means the caller must reject. claims is returned even then
// when the token itself parsed, so a caller can log who was refused.
func resolveSession(c fiber.Ctx, rt *env.Runtime) (claims *authenticator.Claims, ok bool) {
	raw := extractToken(c)
	if raw == "" {
		return nil, false
	}

	claims, err := authenticator.ParseToken(
		crypto.DeriveKey(rt.Config.Auth.JWTSecret, crypto.KeySession), raw)
	if err != nil {
		return nil, false
	}

	if claims.SessionID == "" {
		return claims, true
	}

	now := time.Now().UTC()
	if _, hit := rt.Sessions.Lookup(claims.SessionID, now); hit {
		return claims, true
	}

	sess, err := rt.Store.Session.Get(c.Context(), claims.SessionID)
	// A missing row means the session was purged after expiry, which
	// is the same answer as revoked: this token is finished.
	if err != nil || sess == nil || !sess.Active(now) {
		return claims, false
	}

	rt.Sessions.Store(sess.ID, sess.UserID, sess.ExpiresAt, now)

	// Refresh last_seen_at at most once per touch interval. Writing on
	// every request would put a write in front of every read.
	if now.Sub(sess.LastSeenAt) > sessionTouchInterval {
		if terr := rt.Store.Session.Touch(c.Context(), sess.ID, now); terr != nil {
			slog.Warn("auth: session touch failed", "session_id", sess.ID, "err", terr)
		}
	}

	return claims, true
}

// wantsHTML reports whether the caller is a browser navigating, as
// opposed to a script. Accept is the only signal available before the
// response is written.
func wantsHTML(c fiber.Ctx) bool {
	return strings.Contains(c.Get(fiber.HeaderAccept), "text/html")
}

// requireJSONBody refuses a body that is not application/json.
//
// It sits on login and register, the two open routes that CREATE a
// session or an account, and it exists for login CSRF. SameSite=Strict
// keeps a cross-site request from carrying the victim's cookie, but a
// top-level form POST from evil.example SETS one: the response is a
// navigation, so the browser stores the attacker's session cookie and
// the victim is now signed into the attacker's account, pasting SMTP
// credentials into a project the attacker reads. An HTML form can send
// text/plain, multipart or urlencoded and nothing else - it cannot
// produce application/json - and the decoder never checked, so a form
// field named `{"email":"a","password":"b","x":"` was a valid body.
// The console, curl and every SDK say application/json, so nothing
// legitimate is refused.
func requireJSONBody(c fiber.Ctx) error {
	mediaType, _, err := mime.ParseMediaType(c.Get(fiber.HeaderContentType))
	if err != nil || mediaType != fiber.MIMEApplicationJSON {
		return response.Coded(c, fiber.StatusUnsupportedMediaType, "the body must be application/json")
	}

	return c.Next()
}
