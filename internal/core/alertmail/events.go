// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package alertmail

import (
	amodel "github.com/yousysadmin/mailyard/internal/models/audit"
)

// Tier is who hears about an event.
type Tier int

const (
	// TierAccount mails the person the event happened TO. Their own
	// password, their own second factor.
	TierAccount Tier = iota

	// TierProject mails the project's owners, plus its alert address if
	// one is set. Ownership is the right audience by construction: there
	// can be several, they are responsible for the project, and no
	// separate list has to be kept in step with the membership.
	TierProject

	// TierPlatform mails every enabled platform admin. The same audience
	// the certificate expiry sweep already uses.
	TierPlatform
)

// Alert describes one mailable event.
type Alert struct {
	Tier Tier

	// Heading is the mail's subject and its first line. Written from the
	// RECIPIENT's point of view, not the system's - "Your password was
	// changed", not "auth.password.changed".
	Heading string

	// Note is the sentence under it, saying what to do about it. Every
	// account-tier note has to answer "what if this was not me", which
	// is the only reason that mail is worth sending.
	Note string
}

// alerts is the EXPLICIT list of what gets mailed, and it is the whole
// policy of this package.
//
// A list and not a rule over the stream: every successful mutating
// request produces an event, so "mail the interesting ones" has to
// name them. Anything absent stays in the trail, which is the right
// default.
//
// Deliberately absent: auth.login.failed, because an attacker who
// knows an address could flood that inbox and the real signal is a
// rate. auth.login.succeeded, because without a notion of a new device
// it is a mail per sign-in, which people filter within a week.
// auth.session.revoked, because it fires on a password change and
// would double it. Ordinary configuration edits, because the trail
// already answers who changed what.
var alerts = map[string]Alert{
	// ----------------------------------------------------------------------------
	// The account's own credentials
	// ----------------------------------------------------------------------------
	amodel.TypePasswordChanged: {TierAccount,
		"Your Mailyard password was changed",
		"If this was not you, reset your password immediately and check your active sessions - every other session was signed out when it changed."},
	amodel.TypePasswordResetOK: {TierAccount,
		"Your Mailyard password was reset",
		"This used a link mailed to this address. If you did not ask for it, reset the password again and review your sessions."},
	amodel.TypeTOTPEnabled: {TierAccount,
		"Two-factor authentication was enabled",
		"Sign-in now asks for a code from your authenticator app as well as your password."},
	amodel.TypeTOTPDisabled: {TierAccount,
		"Two-factor authentication was disabled",
		"Your account is now protected by its password alone. If this was not you, enable it again and change the password."},
	amodel.TypeTOTPReset: {TierAccount,
		"An administrator reset your second factor",
		"Two-factor authentication is off until you enrol again, so your account is protected by its password alone until you do."},
	amodel.TypePasskeyAdded: {TierAccount,
		"A passkey was added to your account",
		"That passkey can now sign in without your password. If you did not add it, remove it and change your password."},
	amodel.TypePasskeyRemoved: {TierAccount,
		"A passkey was removed from your account",
		"If that was your only passkey, sign-in falls back to your password."},
	amodel.TypePasskeyReset: {TierAccount,
		"An administrator removed your passkeys",
		"Passwordless sign-in is unavailable until you enrol a new one."},

	// ----------------------------------------------------------------------------
	// The project's access and its data
	// ----------------------------------------------------------------------------
	//
	// String keys, not constants: these are DERIVED from the router by
	// audit.RouteType, so the honest form is the string it produces.
	// TestEveryAlertNamesARealEvent is what keeps a typo here from
	// becoming an alert nobody ever gets.
	"apikey.created": {TierProject,
		"An API key was created in your project",
		"An API key can send mail and read data as its permissions allow. Revoke it from Developers -> API Keys if you did not expect it."},
	"apikey.deleted": {TierProject,
		"An API key was deleted in your project",
		"Anything using that key stops working immediately."},
	"apikey.revoke": {TierProject,
		"An API key was revoked in your project",
		"Anything using that key stops working immediately."},
	"smtpcredential.created": {TierProject,
		"An SMTP credential was created in your project",
		"That credential can submit mail for sending. Revoke it from Developers -> SMTP Submission if you did not expect it."},
	"project.member.created": {TierProject,
		"Somebody was added to your project",
		"They can see and do whatever their role allows. Review it under Project -> Members."},
	"project.member.deleted": {TierProject,
		"Somebody was removed from your project",
		"Their access ended immediately."},
	// Ownership comes through here too: it is a field on the membership,
	// not a route of its own, and an owner holds everything a role can
	// grant plus deleting the project. There is no project.owner event -
	// the first cut of this list invented one, and it would have mailed
	// nobody, ever.
	"project.member.updated": {TierProject,
		"A member's role or ownership changed in your project",
		"Roles decide what a member may reach, and an owner holds everything a role can grant plus deleting the project. Review it under Project -> Members."},
	amodel.TypeWebhookDisabled: {TierProject,
		"A webhook in your project was disabled",
		"Every delivery attempt to it failed, so it was taken out of rotation and no more events will be sent to it. Fix the endpoint, then re-enable it under Developers -> Webhooks."},
	"domain.deleted": {TierProject,
		"A sending domain was removed from your project",
		"Mail from addresses on that domain is refused until it is verified again."},
	"data.deletecontacts": {TierProject,
		"Contacts were erased from your project",
		"This is not reversible. The audit trail records who asked for it."},
	"data.deleteemaillogs": {TierProject,
		"Email logs were erased from your project",
		"This is not reversible. Queued and scheduled mail was left alone."},

	// ----------------------------------------------------------------------------
	// The installation
	// ----------------------------------------------------------------------------
	"admin.apikeys": {TierPlatform,
		"A platform admin key was created",
		"This credential administers the whole installation - it can create users, change platform settings and read every project. It carries no permission list and cannot be narrowed beyond an IP allowlist and an expiry."},
	"admin.users": {TierPlatform,
		"A user account was created on this installation",
		"Check whether it was meant to have administrator rights."},
	"admin.settings": {TierPlatform,
		"Platform settings were changed",
		"These apply to the whole installation. The audit trail records which keys."},
	// RouteType collapses a DELETE or PATCH on any /admin/<thing>/<id>
	// to these two, so the type alone does not say what was touched -
	// the mail carries the method and path for that reason.
	"admin.deleted": {TierPlatform,
		"Something was deleted on this installation",
		"Platform administration, not a project. The path below says what."},
	"admin.updated": {TierPlatform,
		"Something was changed on this installation",
		"Platform administration, not a project. The path below says what."},
}

// Lookup reports whether an event is mailed, and how.
func Lookup(eventType string) (Alert, bool) {
	a, ok := alerts[eventType]

	return a, ok
}

// Types lists every mailable event type, for the guard tests.
func Types() map[string]Tier {
	out := make(map[string]Tier, len(alerts))
	for k, a := range alerts {
		out[k] = a.Tier
	}

	return out
}
