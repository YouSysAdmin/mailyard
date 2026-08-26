// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package project

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/quota"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/systemmail"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain"
	perm "github.com/yousysadmin/mailyard/internal/models/permission"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
)

// invitationTTL bounds how long an unaccepted invitation stays
// redeemable.
const invitationTTL = 7 * 24 * time.Hour

// Handler owns the /api/projects surface. Routes are mounted behind
// requireAuth only - permission checks happen per handler via
// loadWithMember because these routes address the project by path id.
type Handler struct {
	Runtime *env.Runtime
}

// List returns the caller's projects (every project for platform
// admins so the console can administer all tenants).
func (h *Handler) List(c fiber.Ctx) error {
	var (
		out []*projmodel.Project
		err error
	)
	rc := domain.GetRequestContext(c)
	switch {
	case h.Runtime.Config.Auth.Disabled:
		out, err = h.Runtime.Store.Project.List(c.Context())
	case rc != nil && rc.User != nil && rc.User.IsAdmin():
		out, err = h.Runtime.Store.Project.List(c.Context())
	case rc != nil && rc.User != nil:
		out, err = h.Runtime.Store.Project.ListForUser(c.Context(), rc.User.ID)
	default:
		return response.Unauthorized(c, "not authenticated")
	}

	if err != nil {
		return response.Internal(c, err)
	}

	// An empty list is an answer, not a state to repair. The console
	// shows the projects page with a create button, which is the same
	// number of clicks as minting one here and leaves nothing behind for
	// an account that never wanted one.
	if out == nil {
		out = []*projmodel.Project{}
	}

	acc, err := h.accessForList(c, out)
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, ListResponse{
		Projects:  out,
		Access:    acc,
		CanCreate: MayCreate(h.Runtime, rc),
	})
}

// accessForList answers what the caller may do in each listed project.
//
// The same two facts GET /projects/:id returns for one, so the console
// can gate a row by the row's own project rather than by whichever one
// happens to be active. One query for every membership, not one per
// project - see Store.MembershipsForUser.
//
// A platform admin and the auth-disabled mode are owner-equivalent
// everywhere, exactly as callerAccess treats them, and they are listed
// every project rather than their own - so deriving this from
// memberships alone would hand them an empty set for projects they can
// administer.
func (h *Handler) accessForList(c fiber.Ctx, projects []*projmodel.Project) (map[string]ProjectAccess, error) {
	out := make(map[string]ProjectAccess, len(projects))
	rc := domain.GetRequestContext(c)
	everything := ProjectAccess{Owner: true, Permissions: perm.NewSet(perm.All).List()}
	if h.Runtime.Config.Auth.Disabled || (rc != nil && rc.User != nil && rc.User.IsAdmin()) {
		for _, p := range projects {
			out[p.ID] = everything
		}

		return out, nil
	}

	if rc == nil || rc.User == nil {
		return out, nil
	}

	members, err := h.Runtime.Store.Project.MembershipsForUser(c.Context(), rc.User.ID)
	if err != nil {
		return nil, err
	}

	byProject := make(map[string]*projmodel.Member, len(members))
	for _, m := range members {
		byProject[m.ProjectID] = m
	}

	for _, p := range projects {
		m, ok := byProject[p.ID]
		if !ok {
			// Listed without a membership cannot happen on this branch,
			// and answering with nothing is the safe reading if it ever
			// does: the console then offers no action on that row.
			out[p.ID] = ProjectAccess{Permissions: []string{}}
			continue
		}

		out[p.ID] = ProjectAccess{
			Owner:       m.Owner,
			Permissions: perm.ForMember(m.Owner, m.RolePermissions, m.HasRole).List(),
		}
	}

	return out, nil
}

// Create adds a project with the caller as owner.
func (h *Handler) Create(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	if rc == nil || rc.User == nil {
		return response.Forbidden(c, "project creation requires an authenticated user")
	}

	in, resp, ok := validation.Bind[createInput](c)
	if !ok {
		return resp
	}

	slug := in.Slug
	if slug == "" {
		slug = slugify(in.Name)
	}
	if slug == "" {
		return response.BadRequest(c, "slug could not be derived from the name, set one explicitly")
	}

	existing, err := h.Runtime.Store.Project.GetBySlug(c.Context(), slug)
	if err != nil {
		return response.Internal(c, err)
	}

	if existing != nil {
		return response.Conflict(c, "a project with this slug already exists")
	}

	w := &projmodel.Project{
		ID:              ids.New(),
		Name:            in.Name,
		Slug:            slug,
		Description:     in.Description,
		OwnerID:         rc.User.ID,
		DefaultLanguage: in.DefaultLanguage,
	}
	if w.DefaultLanguage == "" {
		w.DefaultLanguage = "en"
	}

	if err := h.Runtime.Store.Project.CreateWithOwner(c.Context(), w, rc.User.ID); err != nil {
		return response.Internal(c, err)
	}

	return response.Created(c, ProjectResponse{Project: w})
}

// Get returns one project the caller can see. Membership required,
// and membership is the whole test: the console reads the project
// name and the caller own role from here before it can render
// anything at all, including for a role that may see only the
// sandbox.
func (h *Handler) Get(c fiber.Ctx) error {
	w, acc, err := h.loadWithMember(c)
	if err != nil || w == nil || !acc.member {
		return h.accessFailure(c, w, acc, err)
	}

	// The permission set ships with the project, and the console
	// renders its menu from it.
	//
	// Sent rather than derived in the browser, because a second copy
	// of the presets in TypeScript is a second thing to keep true, and
	// the one that drifts is always the one nobody enforces. What the
	// console hides is then exactly what the server refuses - a
	// hidden item is hidden because the permission is absent, not
	// because a separate visibility flag said so.
	//
	// It is not a secret and it is not the enforcement. Every gate
	// re-derives the set server-side on every request, so a caller who
	// edits this response has changed a menu and nothing else.
	return response.Success(c, ProjectAccessResponse{
		Project:     w,
		Owner:       acc.owner,
		Permissions: acc.perms.List(),
	})
}

// Update patches name, description, or default language. Project
// admin tier.
func (h *Handler) Update(c fiber.Ctx) error {
	w, acc, err := h.loadWithMember(c)
	if err != nil || w == nil || !acc.perms.Has(perm.ResourceSettings, perm.ActionWrite) {
		return h.accessFailure(c, w, acc, err)
	}

	in, resp, ok := validation.Bind[updateInput](c)
	if !ok {
		return resp
	}

	if in.Name != "" {
		w.Name = in.Name
	}

	if in.Description != nil {
		w.Description = *in.Description
	}

	if in.DefaultLanguage != "" {
		w.DefaultLanguage = in.DefaultLanguage
	}

	if in.StrictSenders != nil {
		w.StrictSenders = *in.StrictSenders
	}

	if in.TrackOpens != nil {
		w.TrackOpens = *in.TrackOpens
	}

	if in.TrackClicks != nil {
		w.TrackClicks = *in.TrackClicks
	}

	if in.BounceAddress != nil {
		addr := strings.ToLower(strings.TrimSpace(*in.BounceAddress))
		// Must be a domain this project has proven it owns, and a
		// subdomain of one counts - bounce.user.com under a verified
		// user.com is the shape this is for. The check is not
		// bureaucracy: a receiver evaluates SPF for the return path
		// against the connecting IP, so an address on somebody else's
		// domain both fails and puts the failure on them.
		if addr != "" {
			if ok, err := h.ownsVerifiedDomain(c, w.ID, addr); err != nil {
				return response.Internal(c, err)
			} else if !ok {
				return response.BadRequest(c, "bounce address must be on a domain verified to this project")
			}
		}

		w.BounceAddress = addr
	}

	if in.AlertEmail != nil {
		w.AlertEmail = strings.ToLower(strings.TrimSpace(*in.AlertEmail))
	}

	if in.SandboxRetentionDays != nil {
		days := *in.SandboxRetentionDays
		// Clamped, not refused. The plan's ceiling is the operator's
		// business and a project asking past it is asking for something
		// nobody offered them - answering with the ceiling is more useful
		// than a 400 that makes them guess what it is. The response
		// carries what was actually stored, so the console shows the
		// clamp rather than pretending.
		if _, ceiling, qerr := quota.Sandbox(c.Context(), h.Runtime.Store, w.ID); qerr == nil {
			if ceiling > 0 && (days <= 0 || days > ceiling) {
				days = ceiling
			}
		}

		w.SandboxRetentionDays = days
	}

	w.UpdatedAt = new(time.Now().UTC())
	if err := h.Runtime.Store.Project.Put(c.Context(), w); err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, ProjectResponse{Project: w})
}

// Delete removes a project and everything in it. Owner only.
//
// There is no undeletable project. Refusing on the premise that every
// account must keep somewhere to stand stopped making sense once
// belonging to no project became an ordinary state.
func (h *Handler) Delete(c fiber.Ctx) error {
	w, acc, err := h.loadWithMember(c)
	if err != nil || w == nil || !acc.owner {
		return h.accessFailure(c, w, acc, err)
	}

	// Attachment objects first, then the rows that name them. Every
	// tenant table cascades off projects - the email log too, since
	// 00070 - but a blob in the object store is named only by the row
	// cascading away, so the cascade would strand every object the
	// project ever offloaded with nothing left to find it from.
	//
	// Three tables own blobs, not one. This collected email keys only,
	// and the comment claimed emails was the only table that mattered -
	// inbound mail and template attachments were stranded silently, and
	// retention never looks at template_attachments at all, so nothing
	// could ever reclaim those.
	//
	// A failed drop refuses the deletion instead of continuing. The
	// project is still there, so the keys are still findable and a
	// retry works - which is not true the other way round.
	ctx := c.Context()
	var keys []string
	for _, collect := range []func(context.Context, string) ([]string, error){
		h.Runtime.Store.Email.StorageKeysForProject,
		h.Runtime.Store.Inbound.StorageKeysForProject,
		h.Runtime.Store.Template.StorageKeysForProject,
	} {
		found, err := collect(ctx, w.ID)
		if err != nil {
			return response.Internal(c, err)
		}

		keys = append(keys, found...)
	}

	if h.Runtime.Blob != nil {
		for _, k := range keys {
			if err := h.Runtime.Blob.Delete(ctx, k); err != nil {
				return response.Internal(c, fmt.Errorf("delete attachment %s: %w", k, err))
			}
		}
	}

	if err := h.Runtime.Store.Project.Delete(ctx, w.ID); err != nil {
		return response.Internal(c, err)
	}

	return response.NoContent(c)
}

// ListMembers returns the membership roster. Any member may see it.
func (h *Handler) ListMembers(c fiber.Ctx) error {
	w, acc, err := h.loadWithMember(c)
	if err != nil || w == nil || !acc.perms.Has(perm.ResourceMembers, perm.ActionRead) {
		return h.accessFailure(c, w, acc, err)
	}

	members, err := h.Runtime.Store.Project.ListMembers(c.Context(), w.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if members == nil {
		members = []*projmodel.Member{}
	}

	return response.Success(c, MemberListResponse{Members: members})
}

// AddMember attaches an existing user by email, at a named role or at
// the project default.
//
// Two writes rather than one: PutMember creates the row and never
// touches role_id (see the store - that contract is what stops an OIDC
// sign-in clearing somebody's role), then SetMemberRole assigns. So
// adding a member who is already there and naming a role reassigns
// them, which is the only reading of that request that makes sense.
func (h *Handler) AddMember(c fiber.Ctx) error {
	w, acc, err := h.loadWithMember(c)
	if err != nil || w == nil || !acc.perms.Has(perm.ResourceMembers, perm.ActionWrite) {
		return h.accessFailure(c, w, acc, err)
	}

	in, resp, ok := validation.Bind[memberInput](c)
	if !ok {
		return resp
	}

	u, err := h.Runtime.Store.User.Get(c.Context(), in.Email)
	if err != nil {
		return response.Internal(c, err)
	}

	if u == nil {
		return response.NotFound(c, "no user with this email, send an invitation instead")
	}

	// The role is checked before the membership is written, because the
	// membership is what survives if the check fails afterwards.
	//
	// Leaving it to SetMemberRole below is not enough. That is a guarded
	// UPDATE, so on the INSERT path a role_id belonging to another
	// project gets written first - project_members.role_id deliberately
	// has no foreign key - the UPDATE then matches no row, and the
	// handler answers "404 role not found" having already made the person
	// a member. A member carrying an id that resolves to nothing is worse
	// than one with no role: memberSelect scopes the join to the project,
	// so COALESCE(m.role_id, p.default_role_id) keeps the dead id over
	// the default and the permission set comes back empty.
	if in.RoleID != "" {
		role, err := h.Runtime.Store.Project.GetRole(c.Context(), w.ID, in.RoleID)
		if err != nil {
			return response.Internal(c, err)
		}

		if role == nil {
			return response.NotFound(c, "role not found")
		}

		if resp, refused := refuseDelegating(c, acc, role); refused {
			return resp
		}
	}

	if err := h.Runtime.Store.Project.PutMember(c.Context(), &projmodel.Member{
		ProjectID: w.ID,
		UserID:    u.ID,
		RoleID:    in.RoleID,
	}); err != nil {
		return response.Internal(c, err)
	}

	if in.RoleID != "" {
		assigned, err := h.Runtime.Store.Project.SetMemberRole(c.Context(), w.ID, u.ID, in.RoleID)
		if err != nil {
			return response.Internal(c, err)
		}

		if !assigned {
			return response.NotFound(c, "role not found")
		}
	}

	// Re-read so the response carries the resolved role name and
	// whether it came from the project default.
	m, err := h.Runtime.Store.Project.GetMember(c.Context(), w.ID, u.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Created(c, MemberResponse{Member: m})
}

// UpdateMember changes a member's role, their ownership, or both.
//
// Ownership is gated harder than the rest: members:write hands out
// roles, and roles are bounded by the catalogue, but granting
// ownership hands over deleting the project. Only an owner may do
// that. The last owner cannot be demoted - the store refuses inside
// the statement, so two simultaneous resignations cannot both win.
func (h *Handler) UpdateMember(c fiber.Ctx) error {
	w, acc, err := h.loadWithMember(c)
	if err != nil || w == nil || !acc.perms.Has(perm.ResourceMembers, perm.ActionWrite) {
		return h.accessFailure(c, w, acc, err)
	}

	userID := c.Params("userId")
	in, resp, ok := validation.Bind[memberRoleInput](c)
	if !ok {
		return resp
	}

	if in.RoleID == nil && in.Owner == nil {
		return response.BadRequest(c, "nothing to change - set role_id, owner, or both")
	}

	if in.Owner != nil && !acc.owner {
		return response.Forbidden(c, "only a project owner may grant or revoke ownership")
	}

	m, err := h.Runtime.Store.Project.GetMember(c.Context(), w.ID, userID)
	if err != nil {
		return response.Internal(c, err)
	}

	if m == nil {
		return response.NotFound(c, "member not found")
	}

	if in.RoleID != nil && *in.RoleID != "" {
		// Read the role before assigning it, so what it grants can be
		// checked against what the caller holds. See refuseDelegating.
		role, rerr := h.Runtime.Store.Project.GetRole(c.Context(), w.ID, *in.RoleID)
		if rerr != nil {
			return response.Internal(c, rerr)
		}

		if role == nil {
			// One answer for a role that does not exist and one that
			// belongs to another project - the tenancy rule.
			return response.NotFound(c, "role not found")
		}

		if resp, refused := refuseDelegating(c, acc, role); refused {
			return resp
		}
	}

	if in.RoleID != nil {
		assigned, err := h.Runtime.Store.Project.SetMemberRole(c.Context(), w.ID, userID, *in.RoleID)
		if err != nil {
			return response.Internal(c, err)
		}

		if !assigned {
			return response.NotFound(c, "role not found")
		}
	}

	if in.Owner != nil && *in.Owner != m.Owner {
		changed, err := h.Runtime.Store.Project.SetMemberOwner(c.Context(), w.ID, userID, *in.Owner)
		if err != nil {
			return response.Internal(c, err)
		}

		if !changed {
			return response.Conflict(c,
				"a project must keep at least one owner - promote somebody else first")
		}
	}

	// Re-read so the response carries the resolved role name and
	// ownership together, whatever combination just changed.
	m, err = h.Runtime.Store.Project.GetMember(c.Context(), w.ID, userID)
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, MemberResponse{Member: m})
}

// RemoveMember detaches a member. Any member may remove themself
// (leave), so the test for that case is membership. Removing somebody
// else needs members:delete.
//
// The last owner cannot be removed, by themself or by anybody - the
// store refuses inside the DELETE. A project with no owner could never
// be deleted or handed on, and nothing in the product can put an owner
// back.
func (h *Handler) RemoveMember(c fiber.Ctx) error {
	w, acc, err := h.loadWithMember(c)
	if err != nil || w == nil || !acc.member {
		return h.accessFailure(c, w, acc, err)
	}

	userID := c.Params("userId")
	rc := domain.GetRequestContext(c)
	self := rc != nil && rc.User != nil && rc.User.ID == userID
	if !self && !acc.perms.Has(perm.ResourceMembers, perm.ActionDelete) {
		return response.Forbidden(c, "the members:delete permission is required")
	}

	m, err := h.Runtime.Store.Project.GetMember(c.Context(), w.ID, userID)
	if err != nil {
		return response.Internal(c, err)
	}

	if m == nil {
		return response.NotFound(c, "member not found")
	}

	// Evicting an owner is an act on ownership, so it takes what
	// UpdateMember takes: being one. Without this the two routes
	// disagreed about the same power - `members:delete` could not
	// DEMOTE an owner (403 there, reserved for owners) and could
	// REMOVE them outright, which is strictly more. So a project
	// administrator who was never made an owner could clear out the
	// owners one at a time down to the last, and the ownership rule
	// held only by accident of the last-owner guard.
	//
	// Self is exempt: leaving a project you own is your own decision,
	// and the last-owner guard still refuses the final one.
	if m.Owner && !self && !acc.owner {
		return response.Forbidden(c, "only a project owner may remove another owner")
	}

	removed, err := h.Runtime.Store.Project.RemoveMember(c.Context(), w.ID, userID)
	if err != nil {
		return response.Internal(c, err)
	}

	if !removed {
		return response.Conflict(c,
			"a project must keep at least one owner - promote somebody else first")
	}

	return response.NoContent(c)
}

// ListInvitations returns pending and past invitations. Admin tier --
// they carry outside email addresses.
func (h *Handler) ListInvitations(c fiber.Ctx) error {
	w, acc, err := h.loadWithMember(c)
	if err != nil || w == nil || !acc.perms.Has(perm.ResourceMembers, perm.ActionWrite) {
		return h.accessFailure(c, w, acc, err)
	}

	invs, err := h.Runtime.Store.Project.ListInvitations(c.Context(), w.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if invs == nil {
		invs = []*projmodel.Invitation{}
	}

	return response.Success(c, InvitationListResponse{Invitations: invs})
}

// CreateInvitation mints an invitation token for an email address.
// The token is returned exactly once. When system mail is configured
// the invitee is also emailed the accept link, but the token stays in
// the response either way: an install without system mail has to be
// able to pass the link along out of band, and a mail server that is
// briefly down must not fail the invite.
func (h *Handler) CreateInvitation(c fiber.Ctx) error {
	w, acc, err := h.loadWithMember(c)
	if err != nil || w == nil || !acc.perms.Has(perm.ResourceMembers, perm.ActionWrite) {
		return h.accessFailure(c, w, acc, err)
	}

	in, resp, ok := validation.Bind[inviteInput](c)
	if !ok {
		return resp
	}

	// Same check as AddMember, and missing here for the same reason: the
	// role was only ever validated by the guarded UPDATE that assigning
	// one uses, and accepting an invitation does not go through it.
	// AcceptInvitation writes inv.RoleID straight into the membership, so
	// an id from another project offered at invite time became a member
	// with an unresolvable role and no permissions at all.
	if in.RoleID != "" {
		role, err := h.Runtime.Store.Project.GetRole(c.Context(), w.ID, in.RoleID)
		if err != nil {
			return response.Internal(c, err)
		}

		if role == nil {
			return response.NotFound(c, "role not found")
		}
	}

	token, err := randomToken()
	if err != nil {
		return response.Internal(c, err)
	}

	rc := domain.GetRequestContext(c)
	invitedBy := ""
	if rc != nil && rc.User != nil {
		invitedBy = rc.User.ID
	}

	inv := &projmodel.Invitation{
		ID:        ids.New(),
		ProjectID: w.ID,
		Email:     in.Email,
		RoleID:    in.RoleID,
		Token:     token,
		Status:    projmodel.InvitationPending,
		InvitedBy: invitedBy,
		ExpiresAt: time.Now().UTC().Add(invitationTTL),
	}
	if err := h.Runtime.Store.Project.PutInvitation(c.Context(), inv); err != nil {
		return response.Internal(c, err)
	}

	emailed := false
	if h.Runtime.SystemMail.Enabled() && h.Runtime.Config.Server.PublicURL != "" {
		inviter := ""
		if rc != nil && rc.User != nil {
			inviter = rc.User.Email
		}

		link := strings.TrimRight(h.Runtime.Config.Server.PublicURL, "/") +
			env.ConsolePath + "/invitations?token=" + token
		subject, htmlBody, textBody := systemmail.Invitation(
			w.Name, inviter, link, int(invitationTTL.Hours()))
		h.Runtime.SystemMail.SendAsync([]string{inv.Email}, subject, htmlBody, textBody)
		emailed = true
	}

	return response.Created(c, InvitationCreatedResponse{
		Invitation: inv,
		Token:      token,
		// Emailed tells the console whether to present the link as the
		// primary hand-off or as a backup to a mail already sent.
		Emailed: emailed,
	})
}

// DeleteInvitation revokes an invitation.
func (h *Handler) DeleteInvitation(c fiber.Ctx) error {
	w, acc, err := h.loadWithMember(c)
	if err != nil || w == nil || !acc.perms.Has(perm.ResourceMembers, perm.ActionDelete) {
		return h.accessFailure(c, w, acc, err)
	}

	if err := h.Runtime.Store.Project.DeleteInvitation(c.Context(), w.ID, c.Params("invId")); err != nil {
		return response.Internal(c, err)
	}

	return response.NoContent(c)
}

// AcceptInvitation redeems a token for a membership. The caller's
// account email must match the invited address so a leaked token
// alone is not enough to join.
func (h *Handler) AcceptInvitation(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	if rc == nil || rc.User == nil {
		return response.Unauthorized(c, "not authenticated")
	}

	inv, err := h.Runtime.Store.Project.GetInvitationByToken(c.Context(), c.Params("token"))
	if err != nil {
		return response.Internal(c, err)
	}

	if inv == nil || inv.Status != projmodel.InvitationPending {
		return response.NotFound(c, "invitation not found or already used")
	}

	if time.Now().UTC().After(inv.ExpiresAt) {
		return response.Gone(c, "invitation expired")
	}

	if !strings.EqualFold(inv.Email, rc.User.Email) {
		return response.Forbidden(c, "this invitation was issued to a different email address")
	}

	// A role deleted since the invitation was written falls back to the
	// project default, which is what the model and the invitation query
	// both say happens - and it did not.
	//
	// DeleteRole refuses while a MEMBER holds the role and while it is
	// the default, but a pending invitation naming it does not block the
	// delete. The id was then written into the membership verbatim, and
	// because COALESCE(m.role_id, p.default_role_id) prefers the column,
	// the dead id beat the default and the join resolved nothing: the
	// new member joined with zero permissions and only a log line said
	// so. Clearing it here is what makes the fallback real.
	roleID := inv.RoleID
	if roleID != "" {
		role, rerr := h.Runtime.Store.Project.GetRole(c.Context(), inv.ProjectID, roleID)
		if rerr != nil {
			return response.Internal(c, rerr)
		}

		if role == nil {
			slog.Warn("project: invitation named a role that no longer exists, using the project default",
				"project_id", inv.ProjectID, "role_id", roleID, "user_id", rc.User.ID)
			roleID = ""
		}
	}

	m := &projmodel.Member{
		ProjectID: inv.ProjectID,
		UserID:    rc.User.ID,
		RoleID:    roleID,
	}
	if err := h.Runtime.Store.Project.PutMember(c.Context(), m); err != nil {
		return response.Internal(c, err)
	}

	// PutMember does not write role_id on conflict, so somebody who is
	// already a member keeps the role they have. That is the right
	// answer: an invitation is an offer to JOIN, and redeeming one is
	// not the moment to overwrite a role an owner set deliberately.
	inv.Status = projmodel.InvitationAccepted
	if err := h.Runtime.Store.Project.PutInvitation(c.Context(), inv); err != nil {
		return response.Internal(c, err)
	}

	w, err := h.Runtime.Store.Project.Get(c.Context(), inv.ProjectID)
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, JoinedResponse{Project: w, Member: m})
}

// DeclineInvitation burns a token without creating a membership, so
// the inviter sees "declined" instead of an invitation that sits
// pending until it expires. Same guards as accept: only the invited
// address may answer, in either direction.
func (h *Handler) DeclineInvitation(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	if rc == nil || rc.User == nil {
		return response.Unauthorized(c, "not authenticated")
	}

	inv, err := h.Runtime.Store.Project.GetInvitationByToken(c.Context(), c.Params("token"))
	if err != nil {
		return response.Internal(c, err)
	}

	if inv == nil || inv.Status != projmodel.InvitationPending {
		return response.NotFound(c, "invitation not found or already used")
	}

	if time.Now().UTC().After(inv.ExpiresAt) {
		return response.Gone(c, "invitation expired")
	}

	if !strings.EqualFold(inv.Email, rc.User.Email) {
		return response.Forbidden(c, "this invitation was issued to a different email address")
	}

	inv.Status = projmodel.InvitationDeclined
	if err := h.Runtime.Store.Project.PutInvitation(c.Context(), inv); err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, DeclinedResponse{Declined: true})
}

// ownsVerifiedDomain reports whether addr's domain is verified to
// project projID. Covering, so a subdomain of a verified domain
// counts - which is the point, since a bounce domain wants its own MX
// and SPF and so is never the apex.
func (h *Handler) ownsVerifiedDomain(c fiber.Ctx, projID, addr string) (bool, error) {
	_, host, ok := strings.CutLast(addr, "@")
	if !ok {
		return false, nil
	}

	d, err := h.Runtime.Store.Domain.GetVerifiedCovering(c.Context(), host)
	if err != nil {
		return false, err
	}

	return d != nil && d.ProjectID == projID, nil
}

// access is what one caller may do in one project: whether they
// belong there, whether they own it, and the permission set the rest
// is decided from.
//
// The same three facts the middleware carries on the RequestContext,
// resolved here because these routes address a project by path id and
// so cannot use the header-resolved one.
type access struct {
	member bool
	owner  bool
	perms  perm.Set
}

// loadWithMember fetches the project addressed by the :id path param
// and resolves the caller's access to it. The permission set, not
// ownership, is what almost every handler here gates on, and it is the
// same ForMember resolution the middleware applies - so a role, and
// the project default behind it, are honored identically on both
// surfaces. Splitting them would recreate the drift where a role is
// obeyed on one surface and ignored on the other.
//
// Returns (nil, zero, nil) when the project does not exist.
//
// It also stamps rc.Project. These routes cannot run requireProject
// (they address the project by path, not header), so without this the
// auditWrites middleware saw no project and filed every member,
// invitation, SSO and role change into the SECURITY trail with an
// empty project id - invisible to the project audit log, which is
// exactly where a permission-policy change belongs.
func (h *Handler) loadWithMember(c fiber.Ctx) (*projmodel.Project, access, error) {
	w, err := h.Runtime.Store.Project.Get(c.Context(), c.Params("id"))
	if err != nil || w == nil {
		return nil, access{}, err
	}

	acc, err := h.callerAccess(c, w.ID)
	if err != nil {
		return w, access{}, err
	}

	if rc := domain.GetRequestContext(c); rc != nil && acc.member {
		rc.Project = w
		rc.ProjectOwner = acc.owner
	}

	return w, acc, nil
}

// callerAccess resolves the caller's access to projID. Platform admins
// and the auth-disabled mode are owner-equivalent. Non-members get the
// zero value, which every gate refuses.
func (h *Handler) callerAccess(c fiber.Ctx, projID string) (access, error) {
	everything := access{member: true, owner: true, perms: perm.NewSet(perm.All)}
	if h.Runtime.Config.Auth.Disabled {
		return everything, nil
	}

	rc := domain.GetRequestContext(c)
	if rc == nil || rc.User == nil {
		return access{}, nil
	}

	if rc.User.IsAdmin() {
		return everything, nil
	}

	m, err := h.Runtime.Store.Project.GetMember(c.Context(), projID, rc.User.ID)
	if err != nil {
		return access{}, err
	}

	if m == nil {
		return access{}, nil
	}

	if m.RoleID != "" && !m.HasRole {
		slog.Warn("project member carries a role that no longer resolves",
			"project_id", projID, "user_id", m.UserID, "role_id", m.RoleID)
	}

	return access{
		member: true,
		owner:  m.Owner,
		perms:  perm.ForMember(m.Owner, m.RolePermissions, m.HasRole),
	}, nil
}

// accessFailure translates the loadWithMember outcome into the right
// error response: 500 on store error, 404 on a missing project or a
// non-member, 403 on not holding the permission.
func (h *Handler) accessFailure(c fiber.Ctx, w *projmodel.Project, acc access, err error) error {
	if err != nil {
		return response.Internal(c, err)
	}

	if w == nil {
		return response.NotFound(c, "project not found")
	}

	// A NON-MEMBER gets the same answer as a missing project. 403 here
	// told a stranger that a project with this id exists, which the
	// header-addressed routes already refuse to do - and ids travel in
	// invitations, exports and logs.
	if !acc.member {
		return response.NotFound(c, "project not found")
	}

	return response.Forbidden(c, "insufficient permissions in this project")
}

// randomToken returns 32 bytes of hex, the invitation capability.
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	return hex.EncodeToString(b), nil
}

// slugify lowers the name and collapses every non-alphanumeric run
// into a single dash.
func slugify(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}

	return strings.Trim(b.String(), "-")
}
