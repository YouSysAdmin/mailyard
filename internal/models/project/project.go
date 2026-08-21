// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package project is the tenancy model. Every mailyard resource
// (SMTP servers, templates, emails, API keys, ...) belongs to exactly
// one project, and a user reaches a project through a membership row.
//
// There are no built-in roles here, and the absence is the design.
// The five presets nested one inside the next - 153, 153, 118, 59 and
// 10 of the router's 153 gated routes, owner and admin identical - so
// the ladder the permission catalogue was meant to escape had grown
// back one layer up.
//
// What replaces them is a role a project writes for itself, plus one
// bit on the membership row for ownership. Ownership stays out of the
// catalogue because what it governs - deleting the project, rewriting
// the SSO policy - has no honest `resource:action`.
package project

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// Project is one tenant. A personal project is minted for a user who
// belongs to no other project, and is not deletable through the
// ordinary project surface. OwnerID is informational - membership
// rows are the access authority.
type Project struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Slug            string `json:"slug"`
	Description     string `json:"description,omitempty"`
	OwnerID         string `json:"owner_id,omitempty"`
	DefaultLanguage string `json:"default_language"`

	// PlanID assigns a usage plan. Empty means the default plan (or
	// unlimited when no plan is marked default).
	PlanID string `json:"plan_id,omitempty"`

	// DefaultRoleID is the role a member carries when their own
	// membership row names none - which is every member an invitation,
	// an OIDC auto-provision or a plain add creates without saying
	// otherwise.
	//
	// Empty means those members reach nothing - a deliberate floor,
	// visible in the console and one click to fix, where a baseline
	// nobody chose is neither.
	//
	// Resolved in SQL by the membership join, not here.
	DefaultRoleID string `json:"default_role_id,omitempty"`

	// StrictSenders requires every send to use a registered sender
	// address (senders table).
	StrictSenders bool `json:"strict_senders"`

	// TrackOpens and TrackClicks are the per-project defaults for mail
	// sent outside a campaign - API, relay, template sends. Campaigns
	// track regardless, which is what a campaign is for. Off by
	// default: a pixel in a password reset is a bad look, and one
	// costs deliverability with some providers.
	TrackOpens  bool `json:"track_opens"`
	TrackClicks bool `json:"track_clicks"`

	// BounceAddress is the SMTP envelope sender for mail this project
	// sends through a server it OWNS, and nothing else. Empty leaves
	// MAIL FROM as the From address.
	//
	// It says where reports go, never which message they are about -
	// that is smtpclient.HeaderEmailID, and conflating the two is what
	// made the earlier version of this field unworkable.
	//
	// It has to be a domain the project controls: SPF for the return
	// path is checked against the connecting IP, and only the project
	// can authorize its own relay. A send through the shared pool uses
	// sending.bounce_address instead, where the IP is ours.
	//
	// SES owns the envelope and discards it.
	BounceAddress string `json:"bounce_address,omitempty"`

	// AlertEmail is where this project's alerts go BESIDE its owners -
	// a ticket queue or a shared ops mailbox. Additive on purpose:
	// redirecting alerts away from the people accountable for the project
	// is not something a project should be able to do quietly. Empty
	// means owners only.
	AlertEmail string `json:"alert_email,omitempty"`

	// SandboxRetentionDays is how long this project keeps a captured
	// message. Zero means the platform default, and whatever is set here
	// is clamped to the plan's ceiling
	// (plans.max_sandbox_retention_days) - a project may ask for less
	// than its plan allows, never more.
	//
	// Per project because a sandbox is a project's: one team wants a day
	// of captures and another wants a fortnight, and neither is the
	// installation's business.
	SandboxRetentionDays int        `json:"sandbox_retention_days"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            *time.Time `json:"updated_at,omitempty"`
}

// Member links a user to a project. Email, RoleName and the resolved
// permissions are filled by the store's join (from users and
// project_roles) and are never stored on the row itself.
type Member struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	UserID    string `json:"user_id"`
	Email     string `json:"email,omitempty"`

	// Owner is the one tier the catalogue cannot express, and it is a
	// BIT rather than the top of a ladder. It grants the wildcard and
	// it grants the acts that are not permissions at all: deleting the
	// project and rewriting its SSO policy.
	//
	// A project may have several. Losing the last one is refused at
	// the write path, because a project nobody owns cannot be given
	// away or shut down.
	Owner bool `json:"owner"`

	// RoleID names the project role whose permissions this member
	// carries. Empty falls back to the project's DefaultRoleID, which
	// the store's join resolves - so a member with neither reaches
	// nothing.
	//
	// Written by exactly one store method, SetMemberRole. PutMember
	// never touches it, so re-adding a member, accepting an invitation
	// or an OIDC auto-provision cannot silently clear it.
	RoleID   string `json:"role_id,omitempty"`
	RoleName string `json:"role_name,omitempty"`

	// InheritedRole reports that RoleID came from the project default
	// rather than from this row. The console says so next to the name,
	// because "everyone here is a Viewer" and "this person was made a
	// Viewer" are different facts and only one of them survives a
	// change to the project default.
	InheritedRole bool `json:"inherited_role,omitzero"`

	// RolePermissions is the role's stored permission strings,
	// resolved by the membership join. Not serialized: the console
	// gets its set from GET /projects/:id, which serves the RESOLVED
	// set through the same code the gates use.
	RolePermissions []string `json:"-"`

	// HasRole reports whether the join actually found a role - the
	// member's own or the project default. Distinct from RoleID != "":
	// a stale reference (deleted role, tiny race window) has an id and
	// no row, and a role granting [] is a deliberate lockdown that
	// must stay distinguishable from having none.
	HasRole bool `json:"-"`

	CreatedAt time.Time `json:"created_at"`
}

// Role is one project-defined permission list, and it is the only
// kind of role there is.
//
// It began as a "custom group", an override sitting beside five
// built-in presets. The presets are gone (see the package doc), so
// this took both their name and their job: a project writes the roles
// it needs and assigns them, and nothing in the binary decides in
// advance what a project's roles should be.
//
// Permissions holds "resource:action" strings from the permission
// catalogue. Validation lives at the write path (the endpoint refuses
// what permission.Parse refuses, and the wildcard) - the model does
// not import the permission package.
type Role struct {
	ID          string   `json:"id"`
	ProjectID   string   `json:"project_id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Permissions []string `json:"permissions"`

	// Members is how many members currently carry the role. Filled by
	// list queries for the console and the referenced-delete refusal,
	// never stored. Members holding it by way of the project default
	// are counted too - deleting the role would change what they can
	// do just as much.
	Members int `json:"members"`

	// Default reports that the project names this role as the one its
	// roleless members carry.
	Default   bool       `json:"default"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// Invitation statuses. Accepting creates the membership and flips the
// status so the row doubles as an audit record.
const (
	InvitationPending  = "pending"
	InvitationAccepted = "accepted"
	InvitationDeclined = "declined"
)

// Invitation is an offer of membership addressed to an email. The
// token is the capability - it is returned once on create and never
// listed again.
type Invitation struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Email     string `json:"email"`

	// RoleID is the role the invitation offers. Empty offers the
	// project default, which is also what an invitation written before
	// the role it named was deleted ends up offering.
	RoleID   string `json:"role_id,omitempty"`
	RoleName string `json:"role_name,omitempty"`

	// Token is the capability. In memory it is the plaintext only at
	// mint time - the create response and the invitation mail carry it
	// once - and the STORED form is HashInvitationToken(plaintext), so
	// a row read back holds the hash. Same shape as password-reset and
	// signup-verification tokens.
	Token string `json:"-"`

	Status    string    `json:"status"`
	InvitedBy string    `json:"invited_by"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// HashInvitationToken returns the stored form of an invitation token,
// hex(sha256(plaintext)). Hashed at rest so a read-only copy of the
// table - a dump, a backup, a replica - does not hand out live
// invitation links, which is the property the password-reset and
// signup-verification tables already had.
func HashInvitationToken(token string) string {
	sum := sha256.Sum256([]byte(token))

	return hex.EncodeToString(sum[:])
}
