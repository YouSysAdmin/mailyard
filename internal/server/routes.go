// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"io/fs"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/gofiber/fiber/v3/middleware/static"
	docsite "github.com/yousysadmin/mailyard/docs"
	coreaudit "github.com/yousysadmin/mailyard/internal/core/audit"
	"github.com/yousysadmin/mailyard/internal/core/clientip"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/iplimit"
	"github.com/yousysadmin/mailyard/internal/core/metrics"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/analytics"
	"github.com/yousysadmin/mailyard/internal/domain/apikey"
	auditdomain "github.com/yousysadmin/mailyard/internal/domain/audit"
	"github.com/yousysadmin/mailyard/internal/domain/auth"
	"github.com/yousysadmin/mailyard/internal/domain/bounce"
	"github.com/yousysadmin/mailyard/internal/domain/campaign"
	"github.com/yousysadmin/mailyard/internal/domain/certificate"
	"github.com/yousysadmin/mailyard/internal/domain/contact"
	"github.com/yousysadmin/mailyard/internal/domain/data"
	"github.com/yousysadmin/mailyard/internal/domain/domains"
	"github.com/yousysadmin/mailyard/internal/domain/email"
	"github.com/yousysadmin/mailyard/internal/domain/eventstream"
	"github.com/yousysadmin/mailyard/internal/domain/health"
	"github.com/yousysadmin/mailyard/internal/domain/inbound"
	"github.com/yousysadmin/mailyard/internal/domain/language"
	"github.com/yousysadmin/mailyard/internal/domain/notification"
	"github.com/yousysadmin/mailyard/internal/domain/oauthprovider"
	"github.com/yousysadmin/mailyard/internal/domain/plan"
	"github.com/yousysadmin/mailyard/internal/domain/project"
	"github.com/yousysadmin/mailyard/internal/domain/relaynode"
	"github.com/yousysadmin/mailyard/internal/domain/sandbox"
	"github.com/yousysadmin/mailyard/internal/domain/sender"
	"github.com/yousysadmin/mailyard/internal/domain/sesfeedback"
	"github.com/yousysadmin/mailyard/internal/domain/setting"
	"github.com/yousysadmin/mailyard/internal/domain/smtpcredential"
	"github.com/yousysadmin/mailyard/internal/domain/smtpserver"
	"github.com/yousysadmin/mailyard/internal/domain/stylesheet"
	"github.com/yousysadmin/mailyard/internal/domain/subscriber"
	"github.com/yousysadmin/mailyard/internal/domain/subscriberlist"
	"github.com/yousysadmin/mailyard/internal/domain/suppression"
	"github.com/yousysadmin/mailyard/internal/domain/template"
	"github.com/yousysadmin/mailyard/internal/domain/trackingpage"
	"github.com/yousysadmin/mailyard/internal/domain/unsubscribelist"
	"github.com/yousysadmin/mailyard/internal/domain/user"
	"github.com/yousysadmin/mailyard/internal/domain/webhook"
	amodel "github.com/yousysadmin/mailyard/internal/models/audit"
	perm "github.com/yousysadmin/mailyard/internal/models/permission"
	smodel "github.com/yousysadmin/mailyard/internal/models/setting"
	"github.com/yousysadmin/mailyard/web"
)

// The SES receiver and the two relay node budgets were CONSTANTS here,
// with the reasoning that sizes them written above each one. They are
// `ratelimit.*` config keys now, and the reasoning went with them to
// RateLimitConfig - a number an operator has to be able to change does
// not belong in a binary, and the one that governs FORWARDED MAIL is
// the one an operator meets first: a node has already answered 250 by
// the time this refuses, so the fleet outgrowing it loses mail.
//
// Nothing else changed. The three defaults are what the constants were.

func registerRoutes(app *fiber.App, rt *env.Runtime, healthOnly bool) {
	// CORS, off by default. Registered first so preflights are
	// answered before any auth middleware can reject the OPTIONS
	// request that carries no credentials by definition.
	if rt.Config.CORS.Enabled {
		app.Use(cors.New(cors.Config{
			AllowOrigins:     rt.Config.CORS.AllowedOrigins,
			AllowMethods:     rt.Config.CORS.AllowedMethods,
			AllowHeaders:     rt.Config.CORS.AllowedHeaders,
			ExposeHeaders:    rt.Config.CORS.ExposeHeaders,
			AllowCredentials: rt.Config.CORS.AllowCredentials,
			MaxAge:           rt.Config.CORS.MaxAge,
		}))
	}

	// Probes, both open. Liveness answers "is the process wedged",
	// readiness answers "can this instance serve traffic". Keeping
	// them separate means a database outage drains the instance
	// instead of restarting it.
	hh := &health.Handler{Runtime: rt}
	app.Get("/healthz", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})
	app.Get("/readyz", hh.Ready)

	// Prometheus scrape endpoint - opt in, optionally bearer-gated.
	// Registered up here, above the early return below, because the
	// worker role wants metrics more than any other node does.
	if rt.Config.Metrics.Enabled {
		mh := adaptor.HTTPHandler(metrics.HTTPHandler())
		app.Get("/metrics", func(c fiber.Ctx) error {
			if tok := rt.Config.Metrics.Token; tok != "" {
				// Constant time, like every other credential check in
				// this codebase (api keys, relay passwords). A plain !=
				// returns as soon as two bytes differ, which leaks the
				// length of the matching prefix - and this endpoint is
				// reachable by anyone who can route to the process.
				want := []byte("Bearer " + tok)
				got := []byte(c.Get(fiber.HeaderAuthorization))
				if subtle.ConstantTimeCompare(want, got) != 1 {
					return c.SendStatus(fiber.StatusUnauthorized)
				}
			}

			return mh(c)
		})
	}

	// Everything past this point is the api role. A worker node stops
	// here: probes and metrics, no console, no API, no tracking.
	if healthOnly {
		return
	}

	// The console lives at env.ConsolePath - send the bare root there.
	app.Get("/", func(c fiber.Ctx) error {
		return c.Redirect().Status(fiber.StatusFound).To(env.ConsolePath + "/")
	})

	// Public tracking surface - open pixel, click redirects, hosted
	// unsubscribe, view in browser. No session or key auth: every
	// route is authorized by its HMAC-signed URL. Registered before
	// the /api groups so no auth middleware ever shadows it.
	trh := &trackingpage.Handler{Runtime: rt}
	trk := app.Group("/tracking", noStoreCache)
	trk.Get("/open/:file", trh.Open)
	trk.Get("/click/:id/:hash", trh.Click)
	trk.Get("/unsubscribe/:token", trh.UnsubscribePage)
	trk.Post("/unsubscribe/:token", trh.UnsubscribeConfirm)
	trk.Get("/view/:token", trh.WebView)

	// Amazon SES bounce and complaint notifications, delivered by SNS.
	// Public because SNS presents no session and no API key: the
	// authentication is the SNS signature plus the topic allowlist.
	//
	// Registered always rather than behind ses.enabled, which would be a
	// second switch for what the data already says: the allowlist comes
	// from the servers, so with no server carrying a topic it accepts
	// nothing.
	//
	// Rate limited, which the gated version never was. A refusal is
	// cheap, but cheap times unbounded is not.
	sesh := sesfeedback.New(rt, rt.SESTopics)
	app.Post("/webhooks/ses", noStoreCache,
		perMinute(rt, rt.Config.RateLimit.SESWebhookPerMinute, nil), sesh.Receive)

	// Relay node enrolment, and the one authority the console-facing
	// relay routes below share with it. Minting the CA on first use, so
	// two concurrent enrolments cannot each generate one.
	relayCA := registerRelayEnrolment(app, rt)

	// The console's own api, beside the console it belongs to: the
	// sign-in ceremonies, session and credential management, the event
	// stream. Describing a passkey ceremony in a versioned public
	// contract would freeze the shape of our own login page.
	//
	// The split is by what an operation IS, not by who calls it, which
	// keeps this surface small - the product's ordinary work is remotely
	// usable by definition and belongs on /api/v1.
	//
	// It may grow routes of its own for shapes the public contract
	// should not promise, like a per-page aggregate.
	// TestNoRouteExistsOnBothPrefixes stops it becoming a second copy.
	appAPI := app.Group(env.ConsolePath+"/api", noStoreCache)

	// Auth endpoints - open (login obviously can't require an
	// existing session - logout / me are cheap enough to not gate).
	// /auth/login carries a per-IP rate limit so a leaked URL can't
	// be hammered for slow brute-force - bcrypt cost-12 already makes
	// it slow, this caps the absolute throughput.
	ah := &auth.Handler{Runtime: rt}
	loginLimiter := perMinute(rt, rt.Config.RateLimit.LoginPerMinute, nil)
	appAPI.Post("/auth/login", loginLimiter, requireJSONBody, ah.Login)
	appAPI.Post("/auth/logout", ah.Logout)
	// Forgot-password. Open by definition (the caller has no session).
	// Each route gets its own bucket at the login ceiling rather than
	// sharing one: draining the reset budget must not lock a user out
	// of signing in, and vice versa. Mail flooding is capped
	// separately per account inside the handler.
	appAPI.Post("/auth/password-reset/request", perMinute(rt, rt.Config.RateLimit.LoginPerMinute, nil), ah.PasswordResetRequest)
	appAPI.Post("/auth/password-reset/confirm", perMinute(rt, rt.Config.RateLimit.LoginPerMinute, nil), ah.PasswordResetConfirm)
	// Self-signup. Registered only when the operator opted in, so on
	// the default config the route does not exist at all. Login-tier
	// rate limit: the endpoint answers whether an address has an
	// account, and the limiter is what keeps that oracle slow.
	if rt.Config.Auth.RegistrationEnabled && rt.Config.Auth.Local.Enabled && !rt.Config.Auth.Disabled {
		appAPI.Post("/auth/register", perMinute(rt, rt.Config.RateLimit.LoginPerMinute, nil), requireJSONBody, ah.Register)
	}

	// Signup verification. Registered even when signup itself has been
	// switched back off: an account minted while it was on must still
	// be able to confirm its link. Both handlers self-check the
	// feature state and answer uniformly, the limiter keeps the
	// resend oracle slow.
	appAPI.Post("/auth/verify-email", perMinute(rt, rt.Config.RateLimit.LoginPerMinute, nil), ah.VerifyEmailConfirm)
	appAPI.Post("/auth/verify-email/resend", perMinute(rt, rt.Config.RateLimit.LoginPerMinute, nil), ah.VerifyEmailResend)
	appAPI.Get("/auth/me", requireAuth(rt), ah.Me)
	// Changing your own password. Rate limited as well as gated by the
	// session, since it takes the current password and so is a place to
	// guess one.
	//
	// Its own bucket: a Fiber limiter shares per-IP counters across every
	// route it is mounted on, so sharing loginLimiter would let a
	// stuffing burst against /auth/login from one office NAT refuse the
	// authenticated password change for everybody behind it.
	//
	// maintenanceMode, like every authenticated mutation on this prefix,
	// and it must sit after requireAuth - the platform-admin exemption is
	// read off the request context.
	//
	// The open ceremonies below stay open: signing in is how an
	// administrator reaches the switch to turn maintenance back off.
	appAPI.Post("/auth/password", requireAuth(rt), maintenanceMode(rt),
		perMinute(rt, rt.Config.RateLimit.LoginPerMinute, nil), ah.ChangePassword)
	// Session management: the caller's own sign-ins. Scoped by user
	// inside the handlers, so no extra role gate.
	appAPI.Get("/auth/sessions", requireAuth(rt), ah.ListSessions)
	appAPI.Delete("/auth/sessions/:id", requireAuth(rt), maintenanceMode(rt), ah.RevokeSession)
	appAPI.Post("/auth/sessions/revoke-others", requireAuth(rt), maintenanceMode(rt), ah.RevokeOtherSessions)
	appAPI.Get("/auth/info", ah.Info)
	// Passkeys. The two login legs are open and carry the login rate
	// limit - they are how somebody signs in. Everything else manages
	// credentials on an account that is already signed in.
	//
	// These two do share loginLimiter with /auth/login, deliberately:
	// they are alternative ways to do the same thing, and separate
	// buckets would hand an attacker three budgets for one goal.
	appAPI.Post("/auth/passkey/login/begin", loginLimiter, ah.PasskeyLoginBegin)
	appAPI.Post("/auth/passkey/login/finish", loginLimiter, ah.PasskeyLoginFinish)
	appAPI.Get("/auth/passkeys", requireAuth(rt), ah.PasskeyList)
	appAPI.Post("/auth/passkeys/register/begin", requireAuth(rt), maintenanceMode(rt), ah.PasskeyRegisterBegin)
	appAPI.Post("/auth/passkeys/register/finish", requireAuth(rt), maintenanceMode(rt), ah.PasskeyRegisterFinish)
	appAPI.Patch("/auth/passkeys/:id", requireAuth(rt), maintenanceMode(rt), ah.PasskeyRename)
	appAPI.Post("/auth/passkeys/:id/delete", requireAuth(rt), maintenanceMode(rt), ah.PasskeyDelete)
	appAPI.Post("/auth/2fa/setup", requireAuth(rt), maintenanceMode(rt), ah.TOTPSetup)
	appAPI.Post("/auth/2fa/enable", requireAuth(rt), maintenanceMode(rt), ah.TOTPEnable)
	appAPI.Post("/auth/2fa/disable", requireAuth(rt), maintenanceMode(rt), ah.TOTPDisable)
	appAPI.Get("/auth/2fa/recovery-codes", requireAuth(rt), ah.RecoveryCodesStatus)
	appAPI.Post("/auth/2fa/recovery-codes", requireAuth(rt), maintenanceMode(rt), ah.RecoveryCodesRegenerate)

	// SSO start + callback are open by definition (the user has no
	// session yet) and always registered: providers live in the
	// database now, so there is no startup-time answer to "is SSO on".
	// An unknown or disabled slug bounces back to the login page.
	// The callback rate-limiter prevents replay loops and is cheap
	// insurance for a public endpoint.
	oauthLimiter := perMinute(rt, rt.Config.RateLimit.OIDCPerMinute, nil)
	appAPI.Get("/auth/oauth/:slug/start", oauthLimiter, ah.OAuthStart)
	appAPI.Get("/auth/oauth/:slug/callback", oauthLimiter, ah.OAuthCallback)

	// The product surface. Everything usable remotely lives here:
	// sending, templates, campaigns, domains, and platform
	// administration under /admin. The console calls it too, with its
	// session - see machineAuth.
	//
	// Two limiters, and both are needed. The per-key one caps a runaway
	// integration without touching other tenants. It cannot cap anything
	// else: it buckets by whatever token arrived, so a caller sending a
	// fresh random bearer every request gets a fresh bucket every
	// request. The per-IP failure budget inside machineAuth is what
	// covers that.
	th := &template.Handler{Runtime: rt}
	eh := &email.Handler{Runtime: rt}
	suph := &suppression.Handler{Runtime: rt}
	bh := &bounce.Handler{Runtime: rt}
	whh := &webhook.Handler{Runtime: rt}
	slh := &subscriberlist.Handler{Runtime: rt}
	ih := &inbound.Handler{Runtime: rt}
	cth := &contact.Handler{Runtime: rt}
	ulh := &unsubscribelist.Handler{Runtime: rt}
	anh := &analytics.Handler{Runtime: rt}
	gdh := &data.Handler{Runtime: rt}
	// Bucket per presented credential, hashed.
	//
	// Keying on the raw key_prefix would be a DoS handle: it is shown in
	// the console and this limiter runs before machineAuth, so anyone
	// holding one could drain that key's budget with junk. Hashing the
	// full token confines the bucket to the holder and keeps credentials
	// out of the limiter's key space.
	//
	// The session cookie gets its own bucket, or every operator behind
	// one office NAT would share a budget.
	v1Limiter := perMinute(rt, rt.Config.RateLimit.APIPerMinute, func(c fiber.Ctx) string {
		if token := bearerToken(c); token != "" {
			sum := sha256.Sum256([]byte(token))

			return "k:" + hex.EncodeToString(sum[:8])
		}

		if cookie := c.Cookies(auth.SessionCookie); cookie != "" {
			sum := sha256.Sum256([]byte(cookie))

			return "s:" + hex.EncodeToString(sum[:8])
		}

		return "ip:" + clientip.From(c)
	})
	// Per-IP budget for REJECTED credentials, spent inside machineAuth.
	//
	// LoginPerMinute rather than a key of its own: this counts credential
	// attempts from one address, which is what that setting already
	// means on the console's login routes. One limiter instance, because
	// the budget is the IP's and not the route's.
	v1AuthFailures := iplimit.New(rt.Config.RateLimit.LoginPerMinute, time.Minute)
	v1 := app.Group("/api/v1", noStoreCache, v1Limiter,
		machineAuth(rt, v1AuthFailures), maintenanceMode(rt), auditWrites(rt))

	// The machine surface is gated by the same two tokens as the
	// console: permOn(resource) on the group, permRead/permWrite on the
	// route. One vocabulary, not a separate set of scopes for keys.
	//
	// The groups are registered further down and carry no
	// requireProject: machineAuth already resolved the project, and a
	// second call would be a second membership read per request.

	// Everything below requires an authenticated operator when
	// auth.disabled is false.
	// Sub-group so we add the middleware once instead of per-route.
	// Platform administration, under the versioned surface rather than
	// a prefix of its own: standing an installation up is exactly what
	// an operator scripts, and it takes the same credential.
	//
	// requireAdmin still gates each group - machineAuth resolves who
	// the caller is, and being an admin is a property of that user.
	adm := v1.Group("/admin", requireAdmin(rt))

	// The console's own status widget, on the console's own api. It
	// reports whether this instance can reach its dependencies, which
	// is an operator question asked from a browser - /healthz is the
	// probe an orchestrator uses and is open.
	sHealth := &health.Handler{Runtime: rt}
	appAPI.Get("/health", requireAuth(rt), sHealth.Status)

	// Audit trails. The project log is project-admin tier, the
	// security log is the caller's own account activity.
	adh := &auditdomain.Handler{Runtime: rt}
	v1.Get("/audit-log", permOn(perm.ResourceAudit), permRead, adh.ProjectLog)
	// before /audit-log/:id, or `export` is read as an event id - which
	// answers 404 rather than exporting, since an id that is not a uuid
	// names nothing.
	v1.Get("/audit-log/export", permOn(perm.ResourceAudit), permRead, adh.ProjectLogExport)
	v1.Get("/audit-log/:id", permOn(perm.ResourceAudit), permRead, adh.ProjectEvent)
	appAPI.Get("/security-log", requireAuth(rt), adh.SecurityLog)
	appAPI.Get("/security-log/export", requireAuth(rt), adh.SecurityLogExport)

	// Platform settings and the scheduled-job view. Admin tier -
	// these are properties of the installation.
	seth := &setting.Handler{Runtime: rt}
	adm.Get("/settings", seth.List)
	adm.Put("/settings", seth.Update)
	adm.Get("/jobs", seth.Jobs)
	adm.Post("/jobs/:name/run", seth.RunJob)

	// Platform mail status and connection test. Admin tier - the test
	// makes an outbound connection to an operator-chosen host.
	adm.Get("/system-mail", ah.SystemMailStatus)
	adm.Post("/system-mail/test", ah.SystemMailTest)

	// Usage plans - platform admin CRUD plus a per-project usage
	// readout for any member.
	ph := &plan.Handler{Runtime: rt}
	plans := adm.Group("/plans")
	plans.Get("/", ph.List)
	plans.Post("/", ph.Create)
	plans.Patch("/:id", ph.Update)
	plans.Delete("/:id", ph.Delete)
	adm.Patch("/projects/:id/plan", ph.Assign)
	v1.Get("/usage", permOn(perm.ResourceAnalytics), permRead, ph.Usage)

	// Identity providers - platform admin. These decide who may sign
	// in to the whole installation, so they are the same tier as user
	// management rather than a project setting.
	oph := &oauthprovider.Handler{Runtime: rt}
	oauthProviders := adm.Group("/oauth-providers")
	oauthProviders.Get("/", oph.List)
	oauthProviders.Post("/", oph.Create)
	oauthProviders.Get("/:id", oph.Get)
	oauthProviders.Patch("/:id", oph.Update)
	oauthProviders.Delete("/:id", oph.Delete)
	oauthProviders.Post("/:id/test", oph.Test)

	// User management - admin tier on top of the session gate.
	uh := &user.Handler{Runtime: rt}
	// Platform credentials: the /api/v1/admin surface's own key type,
	// so an installation can be stood up by a script rather than by a
	// browser session. They are listed and minted here and nowhere
	// else - a project key cannot reach this group, because it is not
	// admin no matter what permissions it holds.
	akh := &apikey.AdminHandler{Runtime: rt}
	admKeys := adm.Group("/api-keys")
	admKeys.Get("/", akh.List)
	admKeys.Post("/", akh.Create)
	admKeys.Post("/:id/revoke", akh.Revoke)
	admKeys.Delete("/:id", akh.Delete)

	users := adm.Group("/users")
	users.Get("/", uh.List)
	users.Post("/", uh.Create)
	users.Get("/:id", uh.Get)
	users.Patch("/:id", uh.Update)
	users.Delete("/:id", uh.Delete)
	users.Get("/:id/projects", uh.Projects)
	users.Delete("/:id/2fa", uh.ResetTOTP)
	users.Delete("/:id/passkeys", uh.ResetPasskeys)
	users.Post("/:id/revoke-sessions", uh.RevokeSessions)

	// Projects (tenancy) - session gate only. These routes address
	// the project by path id, so role checks live in the handlers
	// instead of a requireProject layer (which resolves the
	// X-Mailyard-Project-Id header for resource domains).
	wh := &project.Handler{Runtime: rt}
	// The permission catalogue the console grid renders. Static,
	// identical across projects, nothing secret - machineAuth on the
	// group is the whole gate, so a key reaches it as well as a session.
	v1.Get("/permissions", wh.Catalog)

	proj := v1.Group("/projects")
	proj.Get("/", wh.List)
	// Who may make a new one is a platform setting, off by default -
	// see requireProjectCreation. Everything else on this group is
	// governed by membership in a project that already exists.
	proj.Post("/", requireProjectCreation(rt), wh.Create)
	proj.Get("/:id", wh.Get)
	proj.Patch("/:id", wh.Update)
	proj.Delete("/:id", wh.Delete)
	proj.Get("/:id/members", wh.ListMembers)
	proj.Post("/:id/members", wh.AddMember)
	proj.Patch("/:id/members/:userId", wh.UpdateMember)
	proj.Delete("/:id/members/:userId", wh.RemoveMember)
	// The project's roles - the permission lists it writes for itself,
	// and the only kind of role there is. Same handler-enforced pattern
	// as the member routes (path-id addressing), same resource:
	// defining who may do what IS member management.
	proj.Get("/:id/roles", wh.ListRoles)
	proj.Post("/:id/roles", wh.CreateRole)
	proj.Patch("/:id/roles/:roleId", wh.UpdateRole)
	proj.Delete("/:id/roles/:roleId", wh.DeleteRole)
	// Which role a member carries when they have none of their own.
	// The console shows it on the settings screen, but it decides what
	// every unassigned member may do, so it is gated on members like
	// the routes above rather than on settings.
	proj.Put("/:id/default-role", wh.SetDefaultRole)
	proj.Get("/:id/invitations", wh.ListInvitations)
	proj.Post("/:id/invitations", wh.CreateInvitation)
	proj.Delete("/:id/invitations/:invId", wh.DeleteInvitation)
	// Accepting an invitation is the one way into a project now, and it
	// is authorized by the token rather than by membership - the caller
	// is by definition not a member yet. The handler checks that the
	// invitation is pending, unexpired, and issued to this account's
	// email address.
	//
	v1.Post("/invitations/:token/accept", wh.AcceptInvitation)
	v1.Post("/invitations/:token/decline", wh.DeclineInvitation)

	// SMTP servers - tenant-scoped: requireProject resolves the
	// X-Mailyard-Project-Id header (falling back to the personal
	// project) and guarantees membership. Reads are open to any
	// member. Mutations are project-admin tier: servers hold relay
	// credentials and decide where the project's mail physically
	// goes, which is infrastructure, not content. Editors write
	// templates and send mail - they do not repoint the pipe.
	sh := &smtpserver.Handler{Runtime: rt}
	smtp := v1.Group("/smtp-servers", permOn(perm.ResourceSMTP))
	smtp.Get("/", permRead, sh.List)
	smtp.Get("/:id", permRead, sh.Get)
	smtp.Post("/", permWrite, sh.Create)
	smtp.Patch("/:id", permWrite, sh.Update)
	smtp.Delete("/:id", permDelete, sh.Delete)
	smtp.Post("/:id/test", permWrite, sh.Test)
	smtp.Post("/:id/enable", permWrite, sh.Enable)
	smtp.Post("/:id/disable", permWrite, sh.Disable)

	// Server groups: the named pools a send is routed to, and the unit
	// failover happens within. Same tier as the servers themselves -
	// a group decides where mail physically leaves from.
	gh := &smtpserver.GroupHandler{Runtime: rt}
	groups := v1.Group("/smtp-server-groups", permOn(perm.ResourceSMTP))
	groups.Get("/", permRead, gh.List)
	groups.Get("/:id", permRead, gh.Get)
	groups.Post("/", permWrite, gh.Create)
	groups.Patch("/:id", permWrite, gh.Update)
	groups.Delete("/:id", permDelete, gh.Delete)

	// The platform-owned SMTP pool: the fallback used by projects that
	// own no server at all. Platform admin, and deliberately not under
	// requireProject - these rows belong to no tenant, and no
	// project-scoped route returns them. A project gets delivery
	// through the pool, never sight of it.
	ssh := &smtpserver.SharedHandler{Runtime: rt}
	// Relay node administration and a project's own nodes.
	//
	// Registered unconditionally, unlike the enrolment endpoints above.
	// Those are gated on relay_nodes.enabled because they are PUBLIC -
	// an unused public surface is one nobody watches. These sit behind
	// requireAdmin and requireProject, so that reasoning does not
	// apply, and gating them meant a console menu entry that answered
	// 404 whenever the feature was off. A page that exists and says
	// the feature is off beats a link that fails.
	{
		rnAdmin := &relaynode.Handler{Runtime: rt, CA: relayCA}
		// A project's own nodes. Project admin, not platform admin:
		// approving a tenant's egress machine is their decision, and
		// making it ours would mean a support ticket for every node.
		// Registered before the admin group so the literal path is not
		// swallowed by it.
		mine := v1.Group("/my/relay-nodes", permOn(perm.ResourceSMTP))
		mine.Get("/", permRead, rnAdmin.ListMine)
		mine.Post("/:id/approve", permWrite, rnAdmin.ApproveMine)
		mine.Post("/:id/suspend", permWrite, rnAdmin.SuspendMine)
		mine.Delete("/:id", permDelete, rnAdmin.DeleteMine)

		nodes := adm.Group("/relay-nodes")
		nodes.Get("/", rnAdmin.List)
		nodes.Post("/:id/approve", rnAdmin.Approve)
		nodes.Post("/:id/suspend", rnAdmin.Suspend)
		// The whole fleet's identity, at once. Before /:id, or Fiber
		// reads "authority" as a node id and answers 404 - a
		// destructive endpoint reporting nothing done.
		nodes.Delete("/authority", rnAdmin.ResetAuthority)
		nodes.Delete("/:id", rnAdmin.Delete)
	}

	// Certificates: what the installation serves, and what it holds.
	// Platform admin and unscoped by project - a listener is not a
	// tenant resource, and the private halves in this table are the
	// installation's.
	certh := &certificate.Handler{Runtime: rt}
	certs := adm.Group("/certificates")
	certs.Get("/", certh.List)
	certs.Get("/system", certh.System)
	certs.Post("/", certh.Upload)
	certs.Post("/generate", certh.Generate)
	// An AUTHORITY, so an installation can put one certificate into
	// its clients' trust stores rather than one per listener. Its own
	// route rather than a flag on the one above: a CA takes no hosts
	// and a leaf takes an issuer, so half the fields would be
	// conditionally meaningless either way round.
	certs.Post("/generate-ca", certh.GenerateCA)
	certs.Get("/acme", certh.ACME)
	certs.Post("/acme/order", certh.Order)
	certs.Post("/acme/renew", certh.Renew)
	certs.Delete("/:name", certh.Delete)
	// The public half, so an authority can be installed on the clients
	// that have to trust it. JSON rather than a file - see PEMResponse.
	// Registered after the literal paths above, so /acme is not read as
	// a name.
	certs.Get("/:name/pem", certh.PEM)

	shared := adm.Group("/shared-smtp-servers")
	shared.Get("/", ssh.List)
	shared.Get("/:id", ssh.Get)
	shared.Post("/", ssh.Create)
	shared.Patch("/:id", ssh.Update)
	shared.Delete("/:id", ssh.Delete)
	shared.Post("/:id/test", ssh.Test)

	// Templates (+ versions + localizations), stylesheets, languages.
	// Stylesheets and languages share the templates resource. Nobody
	// says "may edit stylesheets but not templates" - they are parts
	// of authoring one thing, and a permission per table would be a
	// grid nobody reads.
	tpl := v1.Group("/templates", permOn(perm.ResourceTemplates))
	tpl.Get("/", permRead, th.List)
	tpl.Post("/", permWrite, th.Create)
	// A POST that renders and stores nothing. Declared read so a
	// viewer keeps it, which is the reason the action is a token on
	// the route rather than derived from the HTTP method.
	tpl.Post("/preview", permRead, th.Preview)
	tpl.Post("/import", permWrite, th.Import)
	tpl.Get("/:id", permRead, th.Get)
	tpl.Patch("/:id", permWrite, th.Update)
	// Destructive, and it stayed narrower than write through two
	// designs: admin-only before permissions existed, then
	// `permWrite, projAdmin` once they did, because two actions could
	// not say "edit but not remove". The catalogue grew the third
	// action and this is one token again.
	tpl.Delete("/:id", permDelete, th.Delete)
	tpl.Get("/:id/export", permRead, th.Export)
	tpl.Get("/:id/attachments", permRead, th.ListAttachments)
	tpl.Post("/:id/attachments", permWrite, th.UploadAttachment)
	tpl.Get("/:id/attachments/:attId/download", permRead, th.DownloadAttachment)
	tpl.Delete("/:id/attachments/:attId", permDelete, th.DeleteAttachment)
	tpl.Get("/:id/versions", permRead, th.ListVersions)
	tpl.Post("/:id/versions", permWrite, th.CreateVersion)
	tpl.Patch("/:id/versions/:versionId", permWrite, th.UpdateVersion)
	tpl.Delete("/:id/versions/:versionId", permDelete, th.DeleteVersion)
	tpl.Post("/:id/activate/:versionId", permWrite, th.Activate)
	tpl.Get("/:id/versions/:versionId/localizations", permRead, th.ListLocalizations)
	tpl.Put("/:id/versions/:versionId/localizations", permWrite, th.PutLocalization)
	tpl.Post("/:id/versions/:versionId/preview", permRead, th.PreviewVersion)
	tpl.Delete("/:id/localizations/:localizationId", permDelete, th.DeleteLocalization)

	csh := &stylesheet.Handler{Runtime: rt}
	sheets := v1.Group("/stylesheets", permOn(perm.ResourceTemplates))
	sheets.Get("/", permRead, csh.List)
	sheets.Get("/:id", permRead, csh.Get)
	sheets.Post("/", permWrite, csh.Create)
	sheets.Put("/:id", permWrite, csh.Update)
	sheets.Delete("/:id", permDelete, csh.Delete)

	lh := &language.Handler{Runtime: rt}
	langs := v1.Group("/languages", permOn(perm.ResourceTemplates))
	langs.Get("/", permRead, lh.List)
	langs.Post("/", permWrite, lh.Create)
	langs.Put("/:id", permWrite, lh.Update)
	langs.Delete("/:id", permDelete, lh.Delete)

	// Emails - the send pipeline and the delivery log.
	//
	// The two accepting routes are registered first, on their own group,
	// because which resource they touch depends on the credential: a
	// sandbox key's mail is captured, so it is judged on sandbox:write
	// and holds nothing on emails. Fiber matches in registration order,
	// which is what lets one prefix carry two gates - as /webhooks/bounce
	// also does.
	//
	// Everything below describes or touches real mail, so a sandbox
	// credential is turned away from all of it. That is what closed
	// the hole: requiring emails:write handed a sandbox key the whole
	// write surface, and retry - which honours no flag - would
	// re-queue a real failed message for real delivery.
	accept := v1.Group("/emails", permOnSending())
	accept.Post("/send", permWrite, eh.Send)
	accept.Post("/send-template", permWrite, eh.SendTemplate)

	emails := v1.Group("/emails", refuseSandboxCredential, permOn(perm.ResourceEmails))
	emails.Post("/batch", permWrite, eh.Batch)
	// Both render or look something up and queue nothing, so both are
	// reads however they are spelled. The machine surface already
	// marks the same two scopeRead.
	emails.Post("/preview", permRead, eh.RenderPreview)
	emails.Post("/verify", permRead, eh.Verify)
	emails.Get("/", permRead, eh.List)
	emails.Get("/stats", permRead, eh.Stats)
	emails.Get("/limits", permRead, eh.Limits)
	emails.Get("/:id", permRead, eh.Get)
	emails.Get("/:id/tracked-links", permRead, eh.TrackedLinks)
	emails.Get("/:id/attachments/:idx", permRead, eh.Attachment)
	emails.Get("/:id/status", permRead, eh.Status)
	emails.Post("/:id/retry", permWrite, eh.Retry)

	// Send-test lives on the templates surface but is a send, so the
	// email handler owns it. refuseSandboxCredential is on the ROUTE
	// because this is not under the /emails group that carries it: a
	// sandbox key can hold templates:write (ForKey ADDS the sandbox
	// grants to whatever the key carries), and SendTest calls the
	// service directly, past both sandbox apply points - so without
	// this a sandbox credential sent real mail here.
	tpl.Post("/:id/send-test", refuseSandboxCredential, permWrite, eh.SendTest)

	// API keys - the machine credential management surface. Creating
	// or revoking a credential is project-admin tier.
	kh := &apikey.Handler{Runtime: rt}
	keys := v1.Group("/api-keys", permOn(perm.ResourceAPIKeys))
	keys.Get("/", permRead, kh.List)
	keys.Post("/", permWrite, kh.Create)
	keys.Post("/:id/revoke", permWrite, kh.Revoke)
	keys.Delete("/:id", permDelete, kh.Delete)

	// SMTP relay credentials - the submission credential management
	// surface, same tier as API keys.
	creds := v1.Group("/smtp-credentials", permOn(perm.ResourceAPIKeys))
	sch := &smtpcredential.Handler{Runtime: rt}
	creds.Get("/", permRead, sch.List)
	creds.Post("/", permWrite, sch.Create)
	creds.Post("/:id/revoke", permWrite, sch.Revoke)
	creds.Delete("/:id", permDelete, sch.Delete)

	// Suppressions and bounces - recipient hygiene.
	sups := v1.Group("/suppressions", permOn(perm.ResourceSuppressions))
	sups.Get("/", permRead, suph.List)
	sups.Post("/", permWrite, suph.Create)
	sups.Delete("/", permDelete, suph.Delete)
	bounces := v1.Group("/bounces", permOn(perm.ResourceBounces))
	bounces.Get("/", permRead, bh.List)
	// By address, like the suppression delete above, and for the same
	// reason: the question is about a recipient, not about one report.
	// It clears the HISTORY only - the block is a suppression row.
	bounces.Delete("/", permDelete, bh.Delete)

	// Outgoing webhooks - event subscriptions with HMAC-signed
	// deliveries. Admin tier: registering one points a copy of the
	// project's event data at an external URL.
	// Bounce report intake. Registered before the /webhooks group
	// because it is not one: a provider posting a bounce report is
	// writing a bounce, and Fiber matches in registration order, so
	// this route answers before the group prefix claims the path. It
	// carried the webhooks scope only because the URL says webhooks -
	// that was the coarse vocabulary picking the gate.
	v1.Post("/webhooks/bounce", permOn(perm.ResourceBounces), permWrite, bh.Ingest)

	hooks := v1.Group("/webhooks", permOn(perm.ResourceWebhook))
	hooks.Get("/", permRead, whh.List)
	hooks.Post("/", permWrite, whh.Create)
	hooks.Delete("/:id", permDelete, whh.Delete)
	hooks.Post("/:id/enable", permWrite, whh.Enable)
	hooks.Get("/:id/deliveries", permRead, whh.Deliveries)

	// Data portability and erasure. Export is any member's read of
	// their own project. Erasure is destructive and irreversible,
	// so it is project-admin tier.
	// The export is a read, but not an ordinary one: a single call
	// returns every record the project holds. Behind requireProject
	// alone any member could take the lot, so it gets a resource of its
	// own that nobody below admin holds.
	dataGroup := v1.Group("/data", permOn(perm.ResourceData))
	dataGroup.Get("/export", permRead, gdh.ExportData)
	// Erasure, by POST because each carries a body of criteria. The
	// data resource has no write action at all - a project's data is
	// exported or erased, and there is nothing in between for a write
	// to mean.
	dataGroup.Post("/delete-contacts", permDelete, gdh.DeleteContacts)
	dataGroup.Post("/delete-email-logs", permDelete, gdh.DeleteEmailLogs)

	// Reporting. Read-only aggregation over the project.
	v1.Get("/dashboard/stats", permOn(perm.ResourceAnalytics), permRead, anh.DashboardStats)
	v1.Get("/analytics", permOn(perm.ResourceAnalytics), permRead, anh.Analytics)

	// Unsubscribe lists - transactional opt-out scopes. No members:
	// an address is opted out when a suppression row points here.
	uls := v1.Group("/unsubscribe-lists", permOn(perm.ResourceSuppressions))
	uls.Get("/", permRead, ulh.List)
	uls.Post("/", permWrite, ulh.Create)
	uls.Get("/:id", permRead, ulh.Get)
	uls.Patch("/:id", permWrite, ulh.Update)
	uls.Delete("/:id", permDelete, ulh.Delete)

	// Contacts - addresses the project has actually delivered to. The
	// delivery worker writes these, not the operator, so there is no
	// create or update - only the clean-up.
	cts := v1.Group("/contacts", permOn(perm.ResourceContacts))
	cts.Get("/", permRead, cth.List)
	cts.Delete("/", permDelete, cth.DeleteInactive)
	cts.Get("/:id", permRead, cth.Get)
	cts.Delete("/:id", permDelete, cth.Delete)

	// In-app notifications. Any member: they describe the project,
	// and every member is someone who might act on one. Read state is
	// shared, so marking one read clears it for the whole project.
	nh := &notification.Handler{Runtime: rt}
	notes := v1.Group("/notifications", permOn(perm.ResourceNotifications))
	notes.Get("/", permRead, nh.List)
	notes.Get("/unread", permRead, nh.Unread)
	// Every preset that can read an alert can also clear one. Marking
	// something read is not a privilege - the state is shared by
	// design, so one member clearing it clears it for everybody, and
	// a role that could see an alert but never dismiss it would leave
	// the badge lit for the whole project.
	notes.Post("/read-all", permWrite, nh.MarkAllRead)
	notes.Post("/:id/read", permWrite, nh.MarkRead)
	notes.Delete("/:id", permDelete, nh.Delete)

	// Live activity feed over server-sent events. Authenticated by the
	// ordinary session cookie: EventSource cannot set headers but does
	// send cookies same-origin, so there is no reason to accept a
	// token in the query string and write a JWT into every access log.
	// It lives on the console api and cannot move to /api/v1: an
	// EventSource sends cookies but no headers, so this is the one
	// route that could never take the machine surface's credential.
	// OpenAPI cannot describe a stream usefully either.
	esh := &eventstream.Handler{Runtime: rt}
	appAPI.Get("/events/stream", requireAuth(rt), requireProject(rt),
		permOn(perm.ResourceNotifications), permRead, esh.Stream)
	appAPI.Get("/events/stats", requireAuth(rt), requireAdmin(rt), esh.Stats)

	// Subscribers - the marketing audience.
	subh := &subscriber.Handler{Runtime: rt}
	subs := v1.Group("/subscribers", permOn(perm.ResourceSubscribers))
	subs.Get("/", permRead, subh.List)
	subs.Post("/", permWrite, subh.Create)
	subs.Post("/import", permWrite, subh.Import)
	subs.Post("/import/csv", permWrite, subh.ImportCSV)
	subs.Get("/:id", permRead, subh.Get)
	subs.Patch("/:id", permWrite, subh.Update)
	subs.Delete("/:id", permDelete, subh.Delete)

	// Subscriber lists - static membership and dynamic segments.
	lists := v1.Group("/subscriber-lists", permOn(perm.ResourceSubscribers))
	lists.Get("/", permRead, slh.List)
	lists.Post("/", permWrite, slh.Create)
	// Counts who a segment would match and writes nothing.
	lists.Post("/preview-segment", permRead, slh.PreviewSegment)
	// Sign an address up by email rather than by subscriber id. No
	// console counterpart: a form on somebody's website has an address
	// and nothing else.
	lists.Post("/subscribe", permWrite, slh.Subscribe)
	lists.Get("/:id", permRead, slh.Get)
	lists.Patch("/:id", permWrite, slh.Update)
	lists.Delete("/:id", permDelete, slh.Delete)
	lists.Get("/:id/members", permRead, slh.ListMembers)
	lists.Post("/:id/members", permWrite, slh.AddMember)
	lists.Delete("/:id/members/:subscriberId", permDelete, slh.RemoveMember)
	lists.Post("/:id/unsubscribe", permWrite, slh.UnsubscribeByEmail)
	lists.Post("/:id/resubscribe", permWrite, slh.ResubscribeByEmail)

	// Approved sender addresses - creation gated on verified domains.
	// Admin tier like the domains they hang off: the sender list is
	// the project's From policy (enforced by strict_senders), not
	// content an editor drafts.
	snh := &sender.Handler{Runtime: rt}
	snd := v1.Group("/senders", permOn(perm.ResourceSenders))
	snd.Get("/", permRead, snh.List)
	snd.Post("/", permWrite, snh.Create)
	snd.Delete("/:id", permDelete, snh.Delete)

	// Inbound routing domains - claim + DNS TXT verification.
	// Project-admin tier throughout: a claimed domain governs DKIM
	// signing, sender approval, and inbound routing for the whole
	// project, and verifying one is an infrastructure act done by
	// whoever controls the DNS.
	dh := &domains.Handler{Runtime: rt}
	doms := v1.Group("/domains", permOn(perm.ResourceDomains))
	doms.Get("/", permRead, dh.List)
	doms.Post("/", permWrite, dh.Create)
	doms.Get("/:id", permRead, dh.Get)
	doms.Post("/:id/verify", permWrite, dh.Verify)
	doms.Delete("/:id", permDelete, dh.Delete)

	// Inbound emails - mail received by the MX listener.
	inb := v1.Group("/inbound-emails", permOn(perm.ResourceInbound))
	inb.Get("/", permRead, ih.List)
	inb.Get("/stats", permRead, ih.Stats)
	inb.Get("/:id", permRead, ih.Get)
	inb.Get("/:id/raw", permRead, ih.Raw)
	inb.Get("/:id/attachments/:idx", permRead, ih.Attachment)
	inb.Post("/:id/retry", permWrite, ih.Retry)
	inb.Delete("/:id", permDelete, ih.Delete)

	// Email sandbox - mail a developer submitted that was captured
	// instead of delivered.
	//
	// The sandbox is a resource like any other, so it needs no
	// middleware of its own. A project that wants somebody who can reach
	// the sandbox and nothing else writes that as a role.
	sbh := &sandbox.Handler{Runtime: rt}
	sb := v1.Group("/sandbox", permOn(perm.ResourceSandbox))
	sb.Get("/", permRead, sbh.List)
	sb.Get("/info", permRead, sbh.Info)
	// Registered before /:id so the literal paths are not read as ids.
	// A POST, and a delete: it empties the capture buffer. Declared
	// rather than derived, the same way /preview above is declared a
	// read.
	sb.Post("/clear", permDelete, sbh.Clear)
	// Sandbox credentials, mintable by anybody holding sandbox:write -
	// which includes a developer. Safe here and not on
	// /api/smtp-credentials because the handler forces sandbox on
	// every write: a credential minted here can only write into a
	// sandbox this caller already reads.
	sb.Get("/credentials", permRead, sbh.ListCredentials)
	sb.Post("/credentials", permWrite, sbh.CreateCredential)
	sb.Post("/credentials/:id/revoke", permWrite, sbh.RevokeCredential)
	sb.Get("/:id", permRead, sbh.Get)
	sb.Get("/:id/raw", permRead, sbh.Raw)
	sb.Get("/:id/attachments/:idx", permRead, sbh.Attachment)
	sb.Delete("/:id", permDelete, sbh.Delete)

	// Campaigns - bulk sends driven by the campaign runner.
	ch := &campaign.Handler{Runtime: rt}
	camps := v1.Group("/campaigns", permOn(perm.ResourceCampaigns))
	camps.Get("/", permRead, ch.List)
	camps.Post("/", permWrite, ch.Create)
	camps.Get("/:id", permRead, ch.Get)
	camps.Patch("/:id", permWrite, ch.Update)
	camps.Delete("/:id", permDelete, ch.Delete)
	// A campaign send is real mail to a whole list, and nothing on the
	// campaign path consults the credential's sandbox flag - see the
	// note on send-test.
	camps.Post("/:id/send", refuseSandboxCredential, permWrite, ch.Send)
	camps.Post("/:id/pause", permWrite, ch.Pause)
	camps.Post("/:id/resume", refuseSandboxCredential, permWrite, ch.Resume)
	camps.Post("/:id/cancel", permWrite, ch.Cancel)
	camps.Post("/:id/duplicate", permWrite, ch.Duplicate)
	camps.Get("/:id/messages", permRead, ch.Messages)
	camps.Get("/:id/analytics", permRead, ch.Analytics)

	// The embedded Hugo build, registered only when a site was actually
	// built into the binary - a plain `go build` without `task docs`
	// ships without one, and the route should 404 rather than serve an
	// empty directory.
	//
	// Gated on a session: the docs describe this install's own API
	// surface and settings. Assets sit under the same prefix, so the
	// same gate covers them and the session cookie (Path=/) is sent.
	if docsFS := docsite.FS(); docsFS != nil {
		mountDocs(app, docsFS, requirePageAuth(rt))
	}

	// An unknown API path answers in the same envelope as every other
	// API error. Fiber's own 404 is the plain string "Cannot GET
	// /api/v1/x", which a generated client reports as a JSON parse
	// failure rather than as a 404.
	//
	// Registered after every route and before the SPA, so it runs only
	// when nothing matched.
	app.Use("/api", apiNotFound("/api"))
	app.Use(env.ConsolePath+"/api", apiNotFound(env.ConsolePath+"/api"))

	// SPA (embedded Vue build from web/dist, served under
	// env.ConsolePath). Register LAST so /api/*, /tracking/* and
	// /healthz match before this catch-all. NotFoundFile = index.html is
	// the history-mode fallback: deep links like /app/campaigns resolve
	// client-side.
	sub := web.FS()
	if sub == nil {
		// Built without `npm run build`. Say so at the route rather
		// than 404ing: the alternative is someone concluding the
		// console is broken when it was simply never compiled in.
		slog.Warn("console not embedded in this binary, " + env.ConsolePath + " will not serve - run `task build`")
		app.Use(env.ConsolePath, func(c fiber.Ctx) error {
			return c.Status(fiber.StatusServiceUnavailable).
				SendString("console not built into this binary - run `task build`")
		})

		return
	}

	mountConsole(app, sub)
}

// mountDocs serves the embedded documentation site under /docs, behind
// the gate it is handed.
//
// The site is a directory per page - Hugo writes
// email-sending/single-email/index.html - while the pages link to each
// other without the trailing slash. Resolving a directory to its index
// is what makes that link scheme work, and nothing checks the prose, so
// TestADocumentationPageAnswersWithAndWithoutATrailingSlash pins it.
func mountDocs(app *fiber.App, site fs.FS, gate fiber.Handler) {
	docs := app.Group("/docs", gate)
	docs.Use("/", static.New("", static.Config{
		FS:         site,
		IndexNames: []string{"index.html"},

		// A bare /docs is handed straight past. v3's static resolves a
		// directory request to its index itself, where v2's filesystem
		// middleware did not match this path at all - so without this
		// the site root would answer at BOTH /docs and /docs/, and the
		// redirect below, which exists to keep one canonical URL for
		// it, would never run.
		Next: func(c fiber.Ctx) bool { return c.Path() == "/docs" },
	}))
	// static.Config.NotFoundHandler is not set on purpose - the 404 page
	// has to carry a 404 status, and a handler set there would be inside
	// the middleware that already wrote one meaning. static falls through
	// on a miss, so the page is served here, with the status it deserves.
	docs.Use("/", func(c fiber.Ctx) error {
		// A bare /docs, which is what a person types - static is handed
		// past that one path above, so it arrives here. It is answered
		// here rather than by a route of its own, because a route on
		// /docs would match /docs/ too (routing is not strict) and
		// redirect the index to itself forever.
		if c.Path() == "/docs" {
			return c.Redirect().Status(fiber.StatusFound).To("/docs/")
		}

		page, err := fs.ReadFile(site, "404.html")
		if err != nil {
			return c.SendStatus(fiber.StatusNotFound)
		}

		c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)

		return c.Status(fiber.StatusNotFound).Send(page)
	})
}

// apiNotFound answers the JSON envelope for a request UNDER prefix that
// matched no route, and passes anything else along.
//
// The segment check is the whole point. Fiber's Use matches a raw string
// prefix, so `/app/api` also matches `/app/api-keys` - which is a
// console PAGE. Clicking to it inside the SPA worked, because that is
// client-side routing and never asks the server, so the failure showed
// up only on a reload or a shared link: `{"error":"not found"}` where
// the API Keys page should have been. Found by fetching every console
// path in a browser and reading the status codes.
func apiNotFound(prefix string) fiber.Handler {
	return func(c fiber.Ctx) error {
		path := c.Path()
		if len(path) > len(prefix) && path[len(prefix)] != '/' {
			return c.Next()
		}

		return response.NotFound(c, "not found")
	}
}

// mountConsole serves the embedded build under env.ConsolePath.
//
// Split out of registerRoutes so a test can mount a filesystem it
// controls and ask what a request actually gets back - the rules below
// are about missing files, which is exactly what a real build never
// has.
func mountConsole(app *fiber.App, sub fs.FS) {
	// Cache policy: hashed asset filenames are immutable, but the
	// index.html shell must always revalidate - a browser holding a
	// stale shell after a binary upgrade would request chunk files
	// that no longer exist and every lazy-loaded page would break.
	app.Use(env.ConsolePath, func(c fiber.Ctx) error {
		if strings.HasPrefix(c.Path(), env.ConsolePath+"/assets/") {
			c.Set(fiber.HeaderCacheControl, "public, max-age=31536000, immutable")
		} else {
			c.Set(fiber.HeaderCacheControl, "no-cache")
		}

		return c.Next()
	})

	// A missing file under /assets is a 404 and not the index.html
	// fallback. That fallback exists for history-mode deep links, and
	// a hashed chunk is never one: answering a script request with the
	// HTML shell, at status 200, is what the browser reports as
	// "TypeError: Importing a module script failed" - a message naming
	// neither the file nor the reason. It is what a tab left open
	// across an upgrade asks for, and it took a packet capture to see
	// that the server had answered 200.
	app.Use(env.ConsolePath+"/assets", func(c fiber.Ctx) error {
		name := strings.TrimPrefix(c.Path(), env.ConsolePath+"/")
		f, err := sub.Open(name)
		if err != nil {
			// Includes an invalid name, so traversal is refused here
			// rather than checked for.
			return c.Status(fiber.StatusNotFound).SendString("not found")
		}

		_ = f.Close()

		return c.Next()
	})

	app.Use(env.ConsolePath, static.New("", static.Config{
		FS:         sub,
		IndexNames: []string{"index.html"},

		// The history fallback, which v2 spelled NotFoundFile. Status
		// 200 and not the 404 static already wrote: this answers a deep
		// link into the SPA, where the shell IS the right response.
		NotFoundHandler: func(c fiber.Ctx) error {
			page, err := fs.ReadFile(sub, "index.html")
			if err != nil {
				return c.SendStatus(fiber.StatusNotFound)
			}

			c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)

			return c.Status(fiber.StatusOK).Send(page)
		},
	}))
}

// noStoreCache forces Cache-Control: no-store on every /api/* response.
// JSON returned by the API is session-bound or otherwise dynamic -
// default browser heuristics could cache it on disk, leaking across
// users on shared machines or showing stale state after logout /
// privilege change. No-store is stricter than no-cache: it forbids
// storing entirely, in the browser AND in any intermediate proxy.
//
// Set before c.Next() so a specific handler can still override if it
// ever has reason to.
func noStoreCache(c fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")

	return c.Next()
}

// perMinute builds a fixed-window rate limiter from the operator's
// ratelimit config. It returns a pass-through handler when limiting
// is switched off or the individual max is zero, so the middleware
// chain is identical either way. keyFn defaults to clientip.From -
// NOT the limiter's own default, which is c.IP(), and with ProxyHeader
// deliberately unset that is the TCP peer. Behind the balancer every
// deployment is told to run, that put the whole installation in one
// login bucket: ten failed logins a minute from anyone locked
// everyone out of sign-in, reset and SSO.
//
// The window state is per process. On a multi-node deployment the
// effective ceiling is max times the node count - see the note on
// env.RateLimitConfig.
func perMinute(rt *env.Runtime, max int, keyFn func(fiber.Ctx) string) fiber.Handler {
	if !rt.Config.RateLimit.Enabled || max <= 0 {
		return func(c fiber.Ctx) error { return c.Next() }
	}

	if keyFn == nil {
		keyFn = clientip.From
	}

	cfg := limiter.Config{Max: max, Expiration: time.Minute, KeyGenerator: keyFn}

	return limiter.New(cfg)
}

// maintenanceMode refuses mutating requests while the platform is
// parked, so an operator can drain and migrate without racing
// incoming writes.
//
// Reads stay open: the console must remain usable to see what is
// going on, and blocking GETs would only make an incident harder to
// diagnose. Platform admins are exempt entirely - somebody has to be
// able to turn it back off.
func maintenanceMode(rt *env.Runtime) fiber.Handler {
	return func(c fiber.Ctx) error {
		switch c.Method() {
		case fiber.MethodGet, fiber.MethodHead, fiber.MethodOptions:
			return c.Next()
		}

		if !rt.Settings.Bool(smodel.KeyMaintenanceMode) {
			return c.Next()
		}

		if rc := domain.GetRequestContext(c); rc != nil && rc.IsPlatformAdmin() {
			return c.Next()
		}

		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "the platform is in maintenance mode, writes are temporarily disabled",
		})
	}
}

// auditWrites records every successful mutating request on the
// console surface.
//
// Middleware rather than a call in each handler: a hand-placed audit
// line is forgotten the moment somebody adds an endpoint, and the hole
// in the trail is invisible. This covers new routes the day they exist.
//
// Only successful writes. A rejected request changed nothing, and every
// validation error would bury the configuration changes an auditor is
// looking for. Failed sign-ins are the exception, recorded explicitly by
// the auth handler, because there the failure is the event.
func auditWrites(rt *env.Runtime) fiber.Handler {
	return func(c fiber.Ctx) error {
		err := c.Next()

		switch c.Method() {
		case fiber.MethodGet, fiber.MethodHead, fiber.MethodOptions:
			return err
		}

		status := c.Response().StatusCode()
		if status < 200 || status >= 400 {
			return err
		}

		rc := domain.GetRequestContext(c)
		ev := &amodel.Event{
			Type:   coreaudit.RouteType(c.Method(), c.Path()),
			Method: c.Method(),
			Path:   c.Path(),
			Status: status,
		}
		if rc != nil {
			if rc.User != nil {
				ev.ActorID = rc.User.ID
				ev.ActorEmail = rc.User.Email
			}

			if rc.Project != nil {
				ev.ProjectID = rc.Project.ID
			}

			if rc.APIKey != nil {
				// Machine callers have no user - name the key so the
				// trail still says who did it.
				ev.ActorEmail = "api key " + rc.APIKey.Name
				ev.Detail = "via api key " + rc.APIKey.ID
			}

			if rc.AdminAPIKey != nil {
				// The one credential that can rewrite the installation
				// leaving no trace would be the worst omission in this
				// whole trail.
				ev.ActorEmail = "admin api key " + rc.AdminAPIKey.Name
				ev.Detail = "via admin api key " + rc.AdminAPIKey.ID
			}
		}

		// A write with no project context is a platform-level
		// change (settings, users, plans). Record it against the
		// acting user's trail rather than inventing a project.
		if ev.ProjectID == "" {
			ev.Category = amodel.CategorySecurity
			rt.Audit.Security(c, ev)

			return err
		}

		rt.Audit.Project(c, ev)

		return err
	}
}
