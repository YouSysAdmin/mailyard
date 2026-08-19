// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/models/permission"
)

// The tenant authorization vocabulary: a group names its resource, a
// route names its action.
//
//	tpl := apiAuth.Group("/templates", requireProject(rt), permOn(perm.ResourceTemplates))
//	tpl.Get("/", permRead, th.List)
//	tpl.Post("/", permWrite, th.Create)
//
// Two tokens, replacing the `editor` and `projAdmin` gates, which
// could only say how far up one ladder a caller had to be and never
// which part of the product they were in.
//
// The action is declared and not derived from the HTTP method: Fiber
// runs group middleware before any route handler, so a group-level
// check fires before a route could correct it - and derived,
// `POST /templates/preview` would be a write.

// permLocal is where permOn leaves the resource for the action check.
// A private type rather than a string key so nothing else in the
// process can write it by accident.
type permLocalKey struct{}

// permOn declares which resource a route group governs, and refuses a
// caller who may do nothing with it.
//
// The early refusal is the point. A viewer reaching /api/smtp-servers
// is turned away here rather than at each of the eight routes below
// it, and - more usefully - a group added tomorrow with no permOn is
// refused outright rather than inheriting whatever the last floor
// happened to be. TestEveryProjectRouteDeclaresAPermission makes that
// a build failure instead of a runtime surprise.
//
// Stack it after requireProject, which is what resolves the caller's
// permission set.
func permOn(r permission.Resource) fiber.Handler {
	if _, ok := permission.Lookup(r); !ok {
		// A typo in a resource constant would otherwise be a group
		// that silently refuses everybody. Panic at construction, in
		// registerRoutes, which runs at boot.
		panic("permOn: unknown resource " + string(r))
	}

	return permOnResolved(func(*domain.RequestContext) permission.Resource { return r })
}

// permOnSending is permOn for the two routes that ACCEPT MAIL, where
// which resource is being touched depends on the credential.
//
// A sandbox credential's send is captured, never delivered, so what it
// touches is the SANDBOX - and it is judged on sandbox:write rather
// than emails:write. That is not a nicety. Requiring emails:write
// handed every sandbox key the whole emails write surface, and only
// two routes on it honour the flag: batch refuses explicitly, and
// retry was missed, so a credential handed out precisely so it could
// not send real mail could re-queue a real failed message to a real
// customer. Verified, not theorised.
//
// Narrowing the resource removes that by construction rather than by
// adding a third refusal to a list somebody has to keep complete. A
// developer's key now holds sandbox:read and sandbox:write and nothing
// on emails at all - it cannot read the delivery log, retry anything
// or batch, because it has no permission there to spend.
func permOnSending() fiber.Handler {
	return permOnResolved(func(rc *domain.RequestContext) permission.Resource {
		if rc.IsSandboxCredential() {
			return permission.ResourceSandbox
		}

		return permission.ResourceEmails
	})
}

// permOnResolved is the body both of the above share: it records the
// resource for the route-level action check and refuses a caller who
// may do nothing with it.
func permOnResolved(resolve func(*domain.RequestContext) permission.Resource) fiber.Handler {
	return func(c fiber.Ctx) error {
		rc := domain.GetRequestContext(c)
		if rc == nil {
			return response.Internal(c, errors.New("request context middleware not installed"))
		}

		// No project named, and this route governs one.
		//
		// The refusal lives here rather than in the resolver because
		// the resolver runs on routes that do not need a project -
		// listing the projects you belong to, reading the permission
		// catalogue, accepting an invitation. Failing there would make
		// "you belong to no project yet" unanswerable, which is the
		// state a new account starts in.
		//
		// permOn is exactly the declaration "this route is about a
		// project", so it is the honest place to say which one is
		// missing. A bare 403 here would send the caller looking for a
		// permission problem they do not have.
		if rc.Project == nil {
			return response.BadRequest(c,
				"no project selected - send the "+ProjectHeader+" header")
		}

		r := resolve(rc)
		if !rc.Permissions.Touches(r) {
			return refusePermission(c, r, "")
		}

		c.Locals(permLocalKey{}, r)

		return c.Next()
	}
}

// refuseSandboxCredential turns a sandbox credential away from a route
// it must never reach.
//
// It guards the rest of the emails surface: the delivery log, message
// detail, attachments, stats, batch and retry all describe or touch
// real mail, and a credential whose entire purpose is that its mail is
// not real has no business there. Saying so beats a bare 403 on a
// permission the holder was never meant to be given.
func refuseSandboxCredential(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	if rc != nil && rc.IsSandboxCredential() {
		return response.Forbidden(c,
			"this is a sandbox credential, so it reaches only /emails/send and "+
				"/emails/send-template - the delivery log and retries are real mail")
	}

	return c.Next()
}

// permRead, permWrite and permDelete gate one route against the
// group's resource.
//
// Package-level handlers rather than constructors, so a route reads as
// one token.
//
// Three actions rather than two, so a project can say "may edit but not
// remove" in a role of its own. There is no built-in admin tier to
// borrow for that any more.
//
// The mapping is mechanical: a DELETE method takes permDelete. A POST
// that erases has to say so itself, the same way /templates/preview has
// to say permRead.
func permRead(c fiber.Ctx) error   { return checkPermission(c, permission.ActionRead) }
func permWrite(c fiber.Ctx) error  { return checkPermission(c, permission.ActionWrite) }
func permDelete(c fiber.Ctx) error { return checkPermission(c, permission.ActionDelete) }

func checkPermission(c fiber.Ctx, a permission.Action) error {
	r, ok := c.Locals(permLocalKey{}).(permission.Resource)
	if !ok {
		// The route declared an action but its group declared no
		// resource. Refusing rather than allowing: an unanswerable
		// authorization question is not a yes.
		return response.Internal(c, errors.New(
			"permission action declared on a route whose group has no permOn"))
	}

	rc := domain.GetRequestContext(c)
	if rc == nil {
		return response.Internal(c, errors.New("request context middleware not installed"))
	}

	if !rc.Permissions.Has(r, a) {
		return refusePermission(c, r, a)
	}

	return c.Next()
}

// refusePermission answers a caller the catalogue turned away.
//
// It names the permission, which is safe and useful: the caller is
// already a confirmed member of this project, so telling them what
// they are missing leaks nothing they could not learn by asking a
// colleague, and a bare "forbidden" on a console page is the kind of
// thing that becomes a support ticket. A non-member never reaches
// here - requireProject refuses them first, with a message that does
// not distinguish an existing project from one they cannot see.
func refusePermission(c fiber.Ctx, r permission.Resource, a permission.Action) error {
	if a == "" {
		return response.Forbidden(c, "no access to "+string(r)+" in this project")
	}

	return response.Forbidden(c, "permission "+string(permission.Of(r, a))+" required")
}
