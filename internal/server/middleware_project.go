// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/project"
	"github.com/yousysadmin/mailyard/internal/models/permission"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
)

// ProjectHeader selects the project a tenant-scoped request
// operates in. Falls back to the project_id query param (for
// browser-direct links that cannot set headers), then to
// project.ResolveDefault - the caller's sole project if they have
// exactly one, else their personal one.
const ProjectHeader = "X-Mailyard-Project-Id"

// requireProject resolves the request's project and the caller's
// access into the RequestContext. Stack it after requireAuth on every
// tenant-scoped group - handlers behind it read rc.Project.ID and
// never trust a project id from the request.
//
// Resolution order: header, then query param, then ResolveDefault.
// Platform admins are owner-equivalent in any project, and with auth
// disabled the shared default project is used.
//
// It admits any MEMBER and decides nothing else. What they may do is
// the permission set resolved alongside them - their own role, else
// the project default, else nothing - which permOn and permRead /
// permWrite / permDelete consult.
//
// The old floor of viewer was also a ceiling: admitting viewer to
// every group is why a viewer could read the domain list, the SMTP
// configuration and the whole data export. A group that declares no
// resource is now refused outright.
func requireProject(rt *env.Runtime) fiber.Handler {
	return func(c fiber.Ctx) error {
		if ok, resp := stampProject(c, rt); !ok {
			return resp
		}

		return c.Next()
	}
}

// stampProject resolves the project and the caller's access into the
// RequestContext, without advancing the chain.
//
// Split out of requireProject so machineAuth can run it as one branch
// of a decision, rather than the machine surface growing its own
// tenancy resolution.
func stampProject(c fiber.Ctx, rt *env.Runtime) (bool, error) {
	rc := domain.GetRequestContext(c)
	if rc == nil {
		return false, response.Internal(c, errors.New("request context middleware not installed"))
	}

	projID := c.Get(ProjectHeader)
	if projID == "" {
		projID = c.Query("project_id")
	}

	if rt.Config.Auth.Disabled {
		proj, err := resolveOpenProject(c, rt, projID)
		if err != nil {
			return false, response.Internal(c, err)
		}

		if proj == nil {
			return false, response.BadRequest(c, "unknown project")
		}

		rc.Project = proj
		rc.ProjectOwner = true
		rc.Permissions = permission.NewSet(permission.All)

		return true, nil
	}

	// A platform credential is owner-equivalent in any project it
	// names, the same way a platform-admin session is - see
	// projectAccessOf. It has no membership row to read, so this
	// branch is where that is decided rather than there.
	if rc.User == nil && rc.AdminAPIKey != nil {
		proj, err := rt.Store.Project.Get(c.Context(), projID)
		if err != nil {
			return false, response.Internal(c, err)
		}

		if proj == nil {
			// Same rule as the session branch below: unreachable is
			// nil, and permOn answers on the routes that need one.
			return true, nil
		}

		rc.Project = proj
		rc.ProjectOwner = true
		rc.Permissions = permission.NewSet(permission.All)

		return true, nil
	}

	if rc.User == nil {
		return false, response.Unauthorized(c, "authentication required")
	}

	if projID == "" {
		proj, err := project.ResolveDefault(c.Context(), rt, rc.User.ID)
		if err != nil {
			return false, response.Internal(c, err)
		}

		// Either the caller belongs to several projects and none of
		// them is theirs, or they belong to none and personal
		// projects are off.
		//
		// Not a refusal. rc.Project stays nil and the request
		// continues, because the routes that need a project all
		// declare permOn and it answers this - while the ones that
		// do not (listing your projects, reading the permission
		// catalogue, accepting an invitation) are exactly what a
		// caller in this state needs to reach. Refusing here would
		// leave a brand-new account unable to find its way to a
		// project at all.
		if proj == nil {
			return true, nil
		}

		// Access comes from the membership rather than being assumed
		// owner. This fallback can return a project somebody else owns,
		// where a member has to stay a member.
		access, err := projectAccessOf(c, rt, proj.ID)
		if err != nil {
			return false, response.Internal(c, err)
		}

		if !access.member {
			return false, refuseProjectRole(c)
		}

		rc.Project = proj
		rc.ProjectOwner = access.owner
		rc.Permissions = access.perms

		return true, nil
	}

	// A header naming a project that cannot be reached leaves
	// rc.Project nil and the request CONTINUES, exactly as an absent
	// header does above. It is not a refusal here.
	//
	// The console sends its stored project id on every request, so
	// refusing here once that project is deleted would refuse every
	// route including GET /projects. The console could then never learn
	// the id was stale, and the account is locked out short of clearing
	// site data.
	//
	// Collapsing "no such project" into "not a member" is also the
	// tenancy rule - a distinct 403 tells a stranger the project exists.
	proj, err := rt.Store.Project.Get(c.Context(), projID)
	if err != nil {
		return false, response.Internal(c, err)
	}

	if proj == nil {
		return true, nil
	}

	access, err := projectAccessOf(c, rt, proj.ID)
	if err != nil {
		return false, response.Internal(c, err)
	}

	if !access.member {
		return true, nil
	}

	rc.Project = proj
	rc.ProjectOwner = access.owner
	rc.Permissions = access.perms

	return true, nil
}

// refuseProjectRole answers somebody who is not a member.
//
// One message. Naming a required role would only make sense if a single
// rank were the answer, and a caller is turned away by permOn or
// permRead, which name the permission.
func refuseProjectRole(c fiber.Ctx) error {
	return response.Forbidden(c, "not a project member")
}

// There is no requireProjectOwner middleware, deliberately. The four
// destructive deletes that wanted to sit above write are
// permission.ActionDelete now, and the one act the catalogue cannot
// name - deleting the project - lives on /api/v1/projects/:id, which
// addresses a project by path id and so checks access.owner in its
// handler. Nothing is left for a middleware to gate.

// resolveOpenProject picks the project for the auth-disabled
// mode: the explicitly addressed one, else the shared default
// (created on demand).
func resolveOpenProject(c fiber.Ctx, rt *env.Runtime, projID string) (*projmodel.Project, error) {
	if projID != "" {
		return rt.Store.Project.Get(c.Context(), projID)
	}

	return rt.Store.Project.EnsureDefault(c.Context())
}

// projectAccess is what one caller may do in one project: whether
// they belong there at all, whether they own it, and the permission
// set everything else is decided from.
//
// A struct rather than three returns because the three are read
// together at three call sites, and a bare (bool, bool, Set) is the
// kind of signature where two arguments get swapped and nothing
// complains until somebody is an owner who should not be.
type projectAccess struct {
	member bool
	owner  bool
	perms  permission.Set
}

// projectAccessOf resolves the authenticated caller's access to
// projID, from the one membership read that already happens per
// request - the effective role rides the same join.
//
// Platform admins are owner-equivalent. Non-members get the zero
// value, which every gate refuses.
func projectAccessOf(c fiber.Ctx, rt *env.Runtime, projID string) (projectAccess, error) {
	rc := domain.GetRequestContext(c)
	if rc.IsPlatformAdmin() {
		return projectAccess{member: true, owner: true, perms: permission.NewSet(permission.All)}, nil
	}

	m, err := rt.Store.Project.GetMember(c.Context(), projID, rc.User.ID)
	if err != nil {
		return projectAccess{}, err
	}

	if m == nil {
		return projectAccess{}, nil
	}

	if m.RoleID != "" && !m.HasRole {
		// A stale reference: the role was deleted in the moment between
		// the referenced-delete check and this read. The member falls
		// back to nothing, which is safe but silent, so the log is how
		// the window is noticed if it ever widens.
		slog.Warn("project member carries a role that no longer resolves",
			"project_id", projID, "user_id", m.UserID, "role_id", m.RoleID)
	}

	return projectAccess{
		member: true,
		owner:  m.Owner,
		perms:  permission.ForMember(m.Owner, m.RolePermissions, m.HasRole),
	}, nil
}
