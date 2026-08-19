// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package project

import (
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	perm "github.com/yousysadmin/mailyard/internal/models/permission"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
)

// Project roles: the permission lists a project writes for itself, and
// the only kind of role there is. Gated on the members resource -
// defining who may do what IS member management.
//
// Writing a role and assigning one are the same power, so both are
// bounded by what the caller already holds - see refuseDelegating.
// Treating either as harmless because the other exists is how
// members:write quietly becomes project-admin.

// catalogueSize is how many permissions the catalogue actually
// defines, used to reject an absurd input list before parsing it.
// Summed from the declared actions rather than assuming three per
// resource, which would be wrong for most of them.
func catalogueSize() int {
	n := 0
	for _, d := range perm.Registry {
		n += len(d.Actions)
	}

	return n
}

// normalizeRolePermissions is the STRICT write-side counterpart of the
// tolerant FromStrings. Reading skips what it cannot parse, because
// stored data outlives code. Writing refuses, because a typo accepted
// here - "email:read" for "emails:read" - would be skipped on every
// read forever, and the operator who made it would see a checkbox that
// saves fine and a member who mysteriously lacks the access it
// promised.
//
// Parse also refuses an action the resource does not have, so
// "contacts:write" is rejected here as firmly as a misspelling. It
// names a real resource and a real action and still grants nothing.
func normalizeRolePermissions(in []string) ([]string, string) {
	if len(in) > catalogueSize() {
		return nil, "too many permissions - the catalogue is smaller than this list"
	}

	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		p := strings.ToLower(strings.TrimSpace(raw))
		if p == "" {
			continue
		}

		if p == perm.All {
			// A role is an enumeration by definition. The way to say
			// "may do everything here" is to mark somebody an owner on
			// their membership row, where it is visible for what it is
			// and where it also carries the acts no permission can name.
			return nil, "the wildcard is not allowed in a role - make the member an owner instead"
		}

		if _, _, ok := perm.Parse(p); !ok {
			return nil, "unknown permission " + raw
		}

		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}

	return out, ""
}

// ListRoles returns the project's roles with their member counts.
func (h *Handler) ListRoles(c fiber.Ctx) error {
	w, access, err := h.loadWithMember(c)
	if err != nil || w == nil || !access.perms.Has(perm.ResourceMembers, perm.ActionRead) {
		return h.accessFailure(c, w, access, err)
	}

	roles, err := h.Runtime.Store.Project.ListRoles(c.Context(), w.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, RoleListResponse{Roles: roles})
}

// CreateRole adds a role.
func (h *Handler) CreateRole(c fiber.Ctx) error {
	w, access, err := h.loadWithMember(c)
	if err != nil || w == nil || !access.perms.Has(perm.ResourceMembers, perm.ActionWrite) {
		return h.accessFailure(c, w, access, err)
	}

	in, resp, ok := validation.Bind[roleInput](c)
	if !ok {
		return resp
	}

	perms, why := normalizeRolePermissions(in.Permissions)
	if why != "" {
		return response.BadRequest(c, why)
	}

	// You cannot GRANT what you do not hold - see refuseDelegating.
	if resp, refused := refuseGranting(c, access, perms); refused {
		return resp
	}

	for _, other := range mustListRoles(h, c, w.ID) {
		if strings.EqualFold(other.Name, in.Name) {
			return response.Conflict(c, "a role with this name already exists")
		}
	}

	role := &projmodel.Role{
		ProjectID:   w.ID,
		Name:        in.Name,
		Description: in.Description,
		Permissions: perms,
	}
	if err := h.Runtime.Store.Project.PutRole(c.Context(), role); err != nil {
		return response.Internal(c, err)
	}

	return response.Created(c, RoleResponse{Role: role})
}

// UpdateRole renames or repermissions a role. The id is the reference
// members carry, so a rename is cosmetic and an edited permission list
// applies to every holder on their next request - there is no cache to
// invalidate.
func (h *Handler) UpdateRole(c fiber.Ctx) error {
	w, access, err := h.loadWithMember(c)
	if err != nil || w == nil || !access.perms.Has(perm.ResourceMembers, perm.ActionWrite) {
		return h.accessFailure(c, w, access, err)
	}

	role, err := h.Runtime.Store.Project.GetRole(c.Context(), w.ID, c.Params("roleId"))
	if err != nil {
		return response.Internal(c, err)
	}

	if role == nil {
		return response.NotFound(c, "role not found")
	}

	in, resp, ok := validation.Bind[roleInput](c)
	if !ok {
		return resp
	}

	perms, why := normalizeRolePermissions(in.Permissions)
	if why != "" {
		return response.BadRequest(c, why)
	}

	// You cannot GRANT what you do not hold - see refuseDelegating.
	if resp, refused := refuseGranting(c, access, perms); refused {
		return resp
	}

	for _, other := range mustListRoles(h, c, w.ID) {
		if other.ID != role.ID && strings.EqualFold(other.Name, in.Name) {
			return response.Conflict(c, "a role with this name already exists")
		}
	}

	role.Name = in.Name
	role.Description = in.Description
	role.Permissions = perms
	if err := h.Runtime.Store.Project.PutRole(c.Context(), role); err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, RoleResponse{Role: role})
}

// DeleteRole removes a role nobody carries and the project does not
// name as its default.
//
// Both are REFUSALS rather than cascades, and they fail differently.
// Dropping a role its members carry would move all of them to the
// project default at once - a bulk change of access nobody asked for.
// Dropping the DEFAULT role leaves the project pointing at a row that
// is not there, which reads as "no role", so everybody who never had
// one of their own silently loses everything.
func (h *Handler) DeleteRole(c fiber.Ctx) error {
	w, access, err := h.loadWithMember(c)
	if err != nil || w == nil || !access.perms.Has(perm.ResourceMembers, perm.ActionDelete) {
		return h.accessFailure(c, w, access, err)
	}

	deleted, holding, isDefault, err := h.Runtime.Store.Project.DeleteRole(
		c.Context(), w.ID, c.Params("roleId"))
	if err != nil {
		return response.Internal(c, err)
	}

	if deleted {
		return response.NoContent(c)
	}

	if isDefault {
		return response.Conflict(c,
			"this is the project's default role - name a different default first")
	}

	if holding > 0 {
		return response.Conflict(c,
			"this role is assigned to members - move them to another role first")
	}

	return response.NotFound(c, "role not found")
}

// SetDefaultRole names the role members carry when they have none of
// their own, or clears it.
//
// Gated on members:write rather than settings:write even though the
// console shows it on the project settings screen. It decides what
// every unassigned member may do, which is member policy - the same
// question CreateRole and UpdateMember answer.
func (h *Handler) SetDefaultRole(c fiber.Ctx) error {
	w, access, err := h.loadWithMember(c)
	if err != nil || w == nil || !access.perms.Has(perm.ResourceMembers, perm.ActionWrite) {
		return h.accessFailure(c, w, access, err)
	}

	in, resp, ok := validation.Bind[defaultRoleInput](c)
	if !ok {
		return resp
	}

	// The third way a role reaches somebody, and it reaches every member
	// who has none of their own at once - so it takes the same rule as
	// the other two. See refuseDelegating.
	if in.RoleID != "" {
		role, rerr := h.Runtime.Store.Project.GetRole(c.Context(), w.ID, in.RoleID)
		if rerr != nil {
			return response.Internal(c, rerr)
		}

		if role == nil {
			return response.NotFound(c, "role not found")
		}

		if refusal, refused := refuseDelegating(c, access, role); refused {
			return refusal
		}
	}

	ok, err = h.Runtime.Store.Project.SetDefaultRole(c.Context(), w.ID, in.RoleID)
	if err != nil {
		return response.Internal(c, err)
	}

	if !ok {
		// One answer for a role that does not exist and one that
		// belongs to another project - the tenancy rule.
		return response.NotFound(c, "role not found")
	}

	w.DefaultRoleID = in.RoleID

	return response.Success(c, ProjectResponse{Project: w})
}

// refuseDelegating stops a caller handing out a permission they do not
// hold themselves.
//
// members:write is the only gate on both writing roles and handing them
// out, so without this a member trusted just to manage people could own
// the project in two calls: mint a role carrying the whole catalogue,
// then assign it to themselves. Assigning an existing powerful role
// skips even the first call. That is why we check all three ways a role
// reaches somebody - AddMember, UpdateMember and SetDefaultRole - and
// why CreateRole and UpdateRole check the same thing against whatever
// permissions are being written.
//
// A wildcard in a role was already refused, but enumerating the
// catalogue by hand is the same grant spelled out longhand, which is
// what made this easy to miss.
//
// An owner holds the wildcard, so Missing comes back empty and they may
// delegate anything. That is what ownership is, and it sits on the
// membership row where you can see it.
//
// Two returns, and the bool matters: response.* writes the status and
// returns nil - see TestARefusalHelperIsReturnedAndNotTested.
func refuseDelegating(c fiber.Ctx, caller access, role *projmodel.Role) (error, bool) {
	short := caller.perms.Missing(role.Permissions)
	if len(short) == 0 {
		return nil, false
	}

	return response.Forbidden(c,
		"the role "+role.Name+" grants permissions you do not hold yourself: "+
			strings.Join(short, ", ")+" - ask a project owner"), true
}

// refuseGranting is refuseDelegating for the permission list being
// WRITTEN into a role, rather than for a role being handed to somebody.
// Two doors, one rule, one sentence explaining it.
func refuseGranting(c fiber.Ctx, caller access, perms []string) (error, bool) {
	short := caller.perms.Missing(perms)
	if len(short) == 0 {
		return nil, false
	}

	return response.Forbidden(c,
		"a role cannot grant a permission you do not hold yourself: "+
			strings.Join(short, ", ")+" - ask a project owner"), true
}

// mustListRoles is the name-collision read. An error here surfaces as
// an empty list and the UNIQUE constraint still backstops the race,
// answering 500 instead of a duplicate.
func mustListRoles(h *Handler, c fiber.Ctx, projID string) []*projmodel.Role {
	roles, err := h.Runtime.Store.Project.ListRoles(c.Context(), projID)
	if err != nil {
		return nil
	}

	return roles
}

// Catalog serves the permission Registry: resource keys, labels,
// descriptions, the actions each one actually has, and the
// infrastructure flag.
//
// This is what the console grid renders, and it comes from the server
// so the grid and the enforcement cannot disagree about what exists.
// requireAuth only - the catalogue is static, identical for every
// project, and holds nothing secret (it ships in the docs).
func (h *Handler) Catalog(c fiber.Ctx) error {
	return response.Success(c, CatalogResponse{
		Resources: perm.Registry,
		Actions: []string{
			string(perm.ActionRead),
			string(perm.ActionWrite),
			string(perm.ActionDelete),
		},
	})
}
