// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/clientip"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/iplimit"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/domain"
	akmodel "github.com/yousysadmin/mailyard/internal/models/apikey"
	"github.com/yousysadmin/mailyard/internal/models/permission"
)

// touchInterval throttles last_used_at writes so a busy key does not
// turn every request into an UPDATE.
const touchInterval = time.Minute

// stampAPIKey validates a presented API key (apikey.Prefix, no session
// fallback) and writes the caller onto the RequestContext, without
// advancing the chain. A project-bound key IS its project, so a header
// naming a different one is rejected rather than silently ignored.
//
// All rejection legs return the same 401 body so a probe cannot
// distinguish unknown prefix / bad secret / revoked / IP-blocked.
//
// It is a helper rather than a middleware of its own because
// machineAuth runs it as ONE BRANCH of a decision - the other being a
// session - and a middleware would have had to advance the chain
// before the other branch could be tried. Two return values for the
// reason verifySession has two: the response helpers write the status
// and return nil, so a single error return would let a refused request
// fall through.
func stampAPIKey(c fiber.Ctx, rt *env.Runtime) (bool, error) {
	rc := domain.GetRequestContext(c)
	raw := bearerToken(c)
	if raw == "" || !strings.HasPrefix(raw, akmodel.Prefix) {
		return false, response.Unauthorized(c, "api key required")
	}

	k, err := rt.Store.APIKey.GetByPrefix(c.Context(), akmodel.TokenPrefix(raw))
	if err != nil {
		return false, response.Internal(c, err)
	}

	now := time.Now().UTC()
	if k == nil || !akmodel.HashEquals(raw, k.KeyHash) || !k.IsValid(now) || !k.AllowsIP(clientip.From(c)) {
		slog.Warn("apikey: rejected", "prefix", akmodel.TokenPrefix(raw), "client_ip", clientip.From(c))

		return false, response.Unauthorized(c, "invalid api key")
	}

	// both ways a caller can name a project, because stampProject accepts
	// both: the header, and `?project_id=`. Only the header was checked,
	// so a caller who templated the query parameter into their URLs had it
	// silently ignored and operated on the key's own project instead -
	// naming project B and writing to project A, with a 2xx and nothing
	// anywhere saying otherwise. Not an escalation (the key never reached
	// past its own project) but the worst kind of wrong answer, since the
	// caller has no way to notice.
	for _, named := range []string{c.Get(ProjectHeader), c.Query("project_id")} {
		if named != "" && named != k.ProjectID {
			return false, response.Forbidden(c, "this api key is bound to a different project")
		}
	}

	proj, err := rt.Store.Project.Get(c.Context(), k.ProjectID)
	if err != nil {
		return false, response.Internal(c, err)
	}

	if proj == nil {
		return false, response.Unauthorized(c, "invalid api key")
	}

	if rc == nil {
		return false, response.Internal(c, errors.New("request context middleware not installed"))
	}

	rc.APIKey = k
	rc.Project = proj

	// rc.ProjectOwner stays false, and there is nothing to set it from.
	// A key has no membership row, so there is no role to read, and what
	// ownership still governs is deleting the project - not something a
	// machine credential should reach. The zero value is the honest one.
	//
	// rc.Permissions is what the key actually grants, and it is the field
	// every tenant gate consults.
	rc.Permissions = permission.ForKey(k.Permissions, k.Sandbox)

	if k.LastUsedAt == nil || now.Sub(*k.LastUsedAt) > touchInterval {
		if err := rt.Store.APIKey.TouchLastUsed(c.Context(), k.ID, now); err != nil {
			slog.Warn("apikey: touch last used failed", "key_id", k.ID, "err", err)
		}
	}

	return true, nil
}

// machineAuth is the /api/v1 gate: an API key, or the session the
// browser already holds.
//
// Two credentials on one surface, because the split that matters is
// what an operation IS - remotely usable, or a browser ceremony - not
// who calls it. Mounting the product twice would mean every route
// registered twice and every response described twice.
//
// Accepting the cookie is safe by construction: it is SameSite=Strict
// (buildSessionCookie), so a browser never attaches it cross-site and
// there is no CSRF to mitigate. It also confines cookie auth to our own
// origin, so a third-party browser app still needs a token.
//
// The branches differ only in tenancy: a key names its project, a
// session names it by header. Downstream reads rc.Permissions and
// cannot tell them apart.
//
// authFailures is the per-IP budget for REJECTED credentials, and it is
// what caps unauthenticated abuse here. The per-credential limiter on
// the group cannot: it runs before this gate and keys on the presented
// token, so a fresh random token per request is a fresh bucket per
// request - rate limited on paper, unlimited in fact, and each request
// still costing a key lookup.
//
// Charged only on 401. A wrong credential costs the IP budget, an
// internal error does not (a database blip would otherwise lock out
// every caller), and a working credential is never charged.
func machineAuth(rt *env.Runtime, authFailures *iplimit.Limiter) fiber.Handler {
	// Charge the budget when the helper answered 401 and nothing else.
	// The response helpers write the status and return nil, so the
	// status IS the discriminator - there is no error to inspect.
	charge := func(c fiber.Ctx) {
		if c.Response().StatusCode() == fiber.StatusUnauthorized {
			authFailures.Allow(clientip.From(c))
		}
	}

	return func(c fiber.Ctx) error {
		if authFailures.Exceeded(clientip.From(c)) {
			slog.Warn("apikey: authentication budget exhausted", "client_ip", clientip.From(c))

			return response.TooManyRequests(c, "too many failed authentication attempts")
		}

		// The prefix decides, not the presence of a header. A bearer
		// that is not a key falls through to the session path and is
		// parsed as a JWT, which is what a CLI passing its session
		// token wants.
		switch raw := bearerToken(c); {
		case strings.HasPrefix(raw, akmodel.AdminPrefix):
			if ok, resp := stampAdminKey(c, rt); !ok {
				charge(c)

				return resp
			}

			// A platform credential names no project. It may still
			// address one by header, and stampProject grants it
			// owner-equivalent access there exactly as a platform-admin
			// session gets - see projectAccessOf. Without a header it
			// simply has no project, which is right for /admin.
			if c.Get(ProjectHeader) != "" || c.Query("project_id") != "" {
				if ok, resp := stampProject(c, rt); !ok {
					return resp
				}
			}

			return c.Next()
		case strings.HasPrefix(raw, akmodel.Prefix):
			if ok, resp := stampAPIKey(c, rt); !ok {
				charge(c)

				return resp
			}

			return c.Next()
		}

		if ok, resp := stampSession(c, rt); !ok {
			charge(c)

			return resp
		}

		if ok, resp := stampProject(c, rt); !ok {
			return resp
		}

		return c.Next()
	}
}

// stampAdminKey validates a platform credential and writes it onto
// the RequestContext.
//
// It sets no user and no permission set. A platform credential is not
// a member of anything, and the catalogue has no resource that means
// "may create a user" - so what it grants is decided by requireAdmin
// on the /admin group, and by nothing else.
//
// Same uniform 401 on every rejection leg as the tenant path, for the
// same reason: a probe must not learn which part it got wrong.
func stampAdminKey(c fiber.Ctx, rt *env.Runtime) (bool, error) {
	rc := domain.GetRequestContext(c)
	if rc == nil {
		return false, response.Internal(c, errors.New("request context middleware not installed"))
	}

	raw := bearerToken(c)
	k, err := rt.Store.AdminAPIKey.GetByPrefix(c.Context(), akmodel.TokenPrefix(raw))
	if err != nil {
		return false, response.Internal(c, err)
	}

	now := time.Now().UTC()
	if k == nil || !akmodel.HashEquals(raw, k.KeyHash) || !k.IsValid(now) || !k.AllowsIP(clientip.From(c)) {
		slog.Warn("apikey: admin key rejected", "prefix", akmodel.TokenPrefix(raw), "client_ip", clientip.From(c))

		return false, response.Unauthorized(c, "invalid api key")
	}

	rc.AdminAPIKey = k
	if k.LastUsedAt == nil || now.Sub(*k.LastUsedAt) > touchInterval {
		if err := rt.Store.AdminAPIKey.TouchLastUsed(c.Context(), k.ID, now); err != nil {
			slog.Warn("apikey: touch last used failed", "key_id", k.ID, "err", err)
		}
	}

	return true, nil
}

// bearerToken pulls the Authorization bearer value ("" when absent).
func bearerToken(c fiber.Ctx) string {
	if token, ok := strings.CutPrefix(c.Get("Authorization"), "Bearer "); ok {
		return token
	}

	return ""
}
