// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package audit models the operational and security trails.
//
// Two categories share one table because the shape is identical and
// the retention sweep is one statement. They are read through
// different endpoints with different gates: project events need the
// project admin role, security events belong to the acting user.
package audit

import "time"

// Categories.
const (
	// CategoryProject is configuration activity inside one
	// project: credentials minted, templates changed, servers
	// added.
	CategoryProject = "project"

	// CategorySecurity is account activity: sign-ins, 2FA changes,
	// password resets. Carries no project.
	CategorySecurity = "security"
)

// Security event types. Project types are derived from the route
// (see core/audit) rather than enumerated, because they track the
// API surface one-for-one and a fixed list would silently miss new
// routes.
const (
	TypeLoginSucceeded = "auth.login.succeeded"
	TypeLoginFailed    = "auth.login.failed"
	TypeLogout         = "auth.logout"
	TypeOIDCLogin      = "auth.oidc.login"
	TypeOIDCDenied     = "auth.oidc.denied"
	TypeTOTPEnabled    = "auth.2fa.enabled"
	TypeTOTPDisabled   = "auth.2fa.disabled"

	// TypeTOTPReset is an ADMIN removing another user's second factor,
	// as opposed to TypeTOTPDisabled where the owner proves possession
	// with a code. Kept distinct because the trail must show which one
	// happened.
	TypeTOTPReset       = "auth.2fa.reset"
	TypePasswordResetOK = "auth.password_reset.completed"

	// TypePasswordChanged is the owner changing their own password
	// while signed in, proving the old one. Distinct from a reset,
	// which is reached without a session.
	TypePasswordChanged = "auth.password.changed"
	TypeSessionRevoked  = "auth.session.revoked"
	TypeRegistered      = "auth.registered"
	TypeEmailVerified   = "auth.email_verified"

	// Passkey enrolment and removal are recorded separately from the
	// sign-in they enable: adding a credential is the moment a second
	// way into the account appears, and that is what somebody reading
	// the trail after an incident is looking for.
	TypePasskeyAdded   = "auth.passkey.added"
	TypePasskeyRemoved = "auth.passkey.removed"
	TypePasskeyLogin   = "auth.passkey.login"

	// TypePasskeyReset is an ADMIN clearing every passkey on another
	// account, as opposed to TypePasskeyRemoved where the owner
	// removes one and proves their password. Distinct for the same
	// reason 2FA reset is: the trail must show which one happened.
	TypePasskeyReset = "auth.passkey.reset"

	// TypeWebhookDisabled is the dispatcher giving up on an endpoint
	// after every attempt at a delivery failed. Recorded explicitly,
	// like the security events, because no request produces it.
	TypeWebhookDisabled = "webhook.disabled"
)

// Event is one recorded action.
type Event struct {
	ID       string `json:"id"`
	Category string `json:"category"`
	Type     string `json:"type"`

	// ProjectID is empty for security events.
	ProjectID string `json:"project_id,omitempty"`

	// ActorID and ActorEmail identify who acted. Both are empty for
	// an unauthenticated action such as a failed sign-in with an
	// unknown address.
	ActorID    string `json:"actor_id,omitempty"`
	ActorEmail string `json:"actor_email,omitempty"`

	// ClientIP is the address the request REACHED US FROM, which is not
	// the same as the address the person is at and cannot be made so.
	//
	// A Safari user with iCloud Private Relay on arrives from a
	// Cloudflare, Akamai or Fastly egress address shared with strangers,
	// and no header carries their own: the egress proxy is never told the
	// client's IP, so nothing downstream can reveal what it never
	// received. WARP and any corporate egress behave the same way. Behind
	// a reverse proxy this is the X-Forwarded-For value only when the
	// proxy is named in `server.trusted_proxies` - otherwise it is the
	// proxy itself, which would make every row identical.
	//
	// So: two rows with the same address are not necessarily the same
	// person, and two with different addresses are not necessarily
	// different people.
	ClientIP string `json:"client_ip,omitempty"`

	// UserAgent is what the request announced itself as. Not identity
	// either, and trivially forged - but it is the one other thing a
	// request tells us for free, and it separates a phone from a laptop
	// when a shared egress address cannot.
	UserAgent string `json:"user_agent,omitempty"`

	// Method and Path record the request that produced a project
	// event, so an unrecognized type is still traceable.
	Method string `json:"method,omitempty"`
	Path   string `json:"path,omitempty"`

	// Status is the HTTP status the request returned.
	Status int `json:"status,omitzero"`

	// Detail is a short human-readable note (a failure reason, the
	// name of the affected object).
	Detail    string    `json:"detail,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
