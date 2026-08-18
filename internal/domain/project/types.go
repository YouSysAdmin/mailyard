// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package project

import (
	perm "github.com/yousysadmin/mailyard/internal/models/permission"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
)

// The wire types of this domain: what requests carry in and what
// responses carry out, in one file.
//
// They live here rather than beside the handlers that use them so a
// reader answering "what does this endpoint accept and return" has one
// place to look. The types in internal/models are the stored shapes -
// these are what crosses the wire, and the two are allowed to differ.

// ----------------------------------------------------------------------------
// Requests
// ----------------------------------------------------------------------------

// createInput is the POST /api/projects body. Slug is optional and
// derived from the name when omitted.
type createInput struct {
	Name            string `json:"name"             validate:"required,min=1,max=100"  normalize:"trim"`
	Slug            string `json:"slug"             validate:"omitempty,min=1,max=100" normalize:"normalize"`
	Description     string `json:"description"      validate:"omitempty,max=500"       normalize:"trim"`
	DefaultLanguage string `json:"default_language" validate:"omitempty,min=2,max=10"  normalize:"normalize"`
}

// updateInput is the PATCH /api/projects/:id body. Empty strings
// mean "leave unchanged". Slug is immutable - it may end up in
// operator bookmarks and scripts.
type updateInput struct {
	Name            string  `json:"name"             validate:"omitempty,min=1,max=100" normalize:"trim"`
	Description     *string `json:"description"     validate:"omitzero,max=500"`
	DefaultLanguage string  `json:"default_language" validate:"omitempty,min=2,max=10"  normalize:"normalize"`

	// StrictSenders toggles registered-sender enforcement for every
	// send from this project.
	StrictSenders *bool `json:"strict_senders"`

	// TrackOpens and TrackClicks are the defaults for mail sent
	// outside a campaign. Campaigns track regardless.
	TrackOpens  *bool `json:"track_opens"`
	TrackClicks *bool `json:"track_clicks"`

	// BounceAddress is the envelope sender for mail leaving through a
	// server this project owns, so delivery status notifications come
	// back to Mailyard instead of to the From domain's mailbox.
	//
	// Pointer so an explicit "" clears it back to the default, which
	// is the From address as envelope sender.
	BounceAddress *string `json:"bounce_address" validate:"omitzero,email,max=320"`

	// AlertEmail is where this project's alerts go BESIDE its owners: a
	// ticket queue or a shared ops mailbox rather than one person's inbox.
	//
	// No domain check, unlike BounceAddress. That one becomes an envelope
	// sender whose SPF a receiver evaluates against our IP, so it has to
	// be a domain the project proved it owns. This one only receives, and
	// a ticket system almost never lives on the sending domain.
	//
	// Pointer so an explicit "" clears it back to owners only.
	AlertEmail *string `json:"alert_email" validate:"omitzero,email,max=320"`

	// SandboxRetentionDays is how long this project keeps a capture.
	// Clamped to the plan's ceiling on write, so a project can ask for
	// less than its plan allows and never more. Zero means the platform
	// default.
	SandboxRetentionDays *int `json:"sandbox_retention_days" validate:"omitzero,min=0,max=365"`
}

// memberInput adds a member by email.
//
// role_id is OPTIONAL and naming none is a real choice, not a
// forgotten field: the member then carries the project's default role,
// which is how most people are meant to be added. There is no list of
// role names to validate against here - roles are rows this project
// wrote, so the store's tenancy check is the validation.
type memberInput struct {
	Email  string `json:"email"   validate:"required,email,max=320" normalize:"normalize"`
	RoleID string `json:"role_id" validate:"omitempty,max=64"`
}

// memberRoleInput patches a membership: its role, its ownership, or
// both.
//
// role_id is a pointer so an explicit "" clears it back to the project
// default, while omitting the field leaves it alone. Same for owner,
// which only an owner may set - granting it hands over the ability to
// delete the project, which is not something members:write should
// reach.
type memberRoleInput struct {
	RoleID *string `json:"role_id" validate:"omitzero,max=64"`
	Owner  *bool   `json:"owner"`
}

// inviteInput offers membership to an email address, optionally at a
// named role. Empty offers the project default - see memberInput.
type inviteInput struct {
	Email  string `json:"email"   validate:"required,email,max=320" normalize:"normalize"`
	RoleID string `json:"role_id" validate:"omitempty,max=64"`
}

// defaultRoleInput names the role members carry when they have none of
// their own. An explicit "" clears it, which leaves those members
// reaching nothing at all.
type defaultRoleInput struct {
	RoleID string `json:"role_id" validate:"omitempty,max=64"`
}

// roleInput defines a project role. Permissions is required and may
// be empty - an empty role is a deliberate lockdown, and leaving the
// field out entirely is more likely a bug than an intention.
type roleInput struct {
	Name        string   `json:"name"        validate:"required,min=1,max=100" normalize:"trim"`
	Description string   `json:"description" validate:"omitempty,max=500"      normalize:"trim"`
	Permissions []string `json:"permissions" validate:"required"`
}

// ----------------------------------------------------------------------------
// Responses
// ----------------------------------------------------------------------------

// ListResponse is every project the caller belongs to.
type ListResponse struct {
	Projects []*projmodel.Project `json:"projects"`

	// Access is what the caller may do in each of them, keyed by project
	// id - the same two fields ProjectAccessResponse carries for one.
	//
	// It is here because the console needs it PER ROW. Gating the buttons
	// on a list with the active project's permissions is judging one
	// project by another's role: a member without members:read was
	// offered a Members button that answered 403, and the router refused
	// pages in projects where the same person held everything.
	Access map[string]ProjectAccess `json:"access"`

	// CanCreate is whether this caller may make ANOTHER one, which is a
	// platform setting rather than anything about the projects listed -
	// so it belongs to the caller, not to a row in Access.
	//
	// It rides on the list because the list is what the console already
	// fetches at boot, and because every control it gates hangs off that
	// store: the button on the projects page, the two switcher entries,
	// and the card an account with no projects at all is shown. The
	// answer is the server's, never recomputed in the browser.
	CanCreate bool `json:"can_create"`
}

// ProjectAccess is one caller's standing in one project. Not the
// enforcement - every gate re-derives it server-side per request.
type ProjectAccess struct {
	Owner       bool     `json:"owner"`
	Permissions []string `json:"permissions"`
}

// ProjectResponse is one project.
type ProjectResponse struct {
	Project *projmodel.Project `json:"project"`
}

// ProjectAccessResponse is one project together with what the caller
// may do in it.
//
// Permissions is what the console builds its menu from. It is not the
// enforcement and not a secret: every gate re-derives the set
// server-side on every request, so a caller who edits this response
// has changed a menu and nothing else.
type ProjectAccessResponse struct {
	Project *projmodel.Project `json:"project"`

	// Owner is the one thing that is not a permission: the caller may
	// delete this project and rewrite its SSO policy.
	Owner       bool     `json:"owner"`
	Permissions []string `json:"permissions"`
}

// MemberListResponse is the project roster.
type MemberListResponse struct {
	Members []*projmodel.Member `json:"members"`
}

// MemberResponse is one membership.
type MemberResponse struct {
	Member *projmodel.Member `json:"member"`
}

// JoinedResponse is what accepting an invitation returns: the project
// joined and the membership created in it.
type JoinedResponse struct {
	Project *projmodel.Project `json:"project"`
	Member  *projmodel.Member  `json:"member"`
}

// DeclinedResponse confirms an invitation was turned down.
type DeclinedResponse struct {
	Declined bool `json:"declined"`
}

// InvitationListResponse is the project's pending invitations.
type InvitationListResponse struct {
	Invitations []*projmodel.Invitation `json:"invitations"`
}

// InvitationCreatedResponse carries the new invitation and its token.
//
// Token is returned so the console can offer a copyable link, which is
// the whole hand-off on an install with no system mail. Emailed says
// which of the two happened, so the screen can present the link as the
// primary route or as a backup to a message already sent.
type InvitationCreatedResponse struct {
	Invitation *projmodel.Invitation `json:"invitation"`
	Token      string                `json:"token"`
	Emailed    bool                  `json:"emailed"`
}

// RoleListResponse is the project's roles.
type RoleListResponse struct {
	Roles []*projmodel.Role `json:"roles"`
}

// RoleResponse is one role.
type RoleResponse struct {
	Role *projmodel.Role `json:"role"`
}

// CatalogResponse is the permission catalogue the console grid renders
// from. Served by the binary that enforces it, so the checkboxes
// cannot offer a permission that does not exist.
// Actions is the full vocabulary. Which of them apply to a given
// resource is on the resource itself (Definition.Actions), because
// most have fewer than three and a grid that renders every cell
// teaches people to stop reading it.
type CatalogResponse struct {
	Resources []perm.Definition `json:"resources"`
	Actions   []string          `json:"actions"`
}

// CleanupResponse reports what to delete actually did. Emptiness is
// re-evaluated at delete time, so a project that has since been used
