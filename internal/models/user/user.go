// Package user is the dashboard user record.
package user

import "time"

// AccountType is how an account signs in, stored at creation.
//
// It answers one question, asked on every profile: may this person
// manage their own password, second factor and passkeys? Only
// AccountOIDC may not - an identity provider owns those, and a local
// password or a passkey would be a way in the IdP knows nothing
// about, surviving the account being disabled there.
//
// Everything else is an ordinary account, however it was created. An
// admin adding somebody through the console mints AccountLocal, and if
// that were treated as "managed elsewhere" those people would be left
// staring at "ask an administrator" with no form to use.
//
// This is not where administration is recorded. Signing in and
// administering are separate questions - an OIDC account can perfectly
// well run the installation - and folding them into one value would make
// that combination unsayable. See Admin.
type AccountType int

// The two account types. Numbered from one, but zero is NOT rejected:
// Store.Put writes an unset type as AccountLocal, and
// ManagesOwnCredentials tests against AccountOIDC rather than for
// AccountLocal - so anything that is not explicitly OIDC behaves as a
// local account.
const (
	AccountLocal AccountType = 1
	AccountOIDC  AccountType = 2
)

// User is the operator-console account.
//
// Admin gates the user-management surface through the requireAdmin
// middleware - every other protected route is "in or out" - any
// authenticated, non-disabled user may call it. Gate new privileged
// route groups with requireAdmin at registration time
// (server/routes.go) rather than hand-rolling IsAdmin() checks inside
// individual handlers.
//
// The first user, whether it comes from the local bootstrap or the
// first OIDC sign-in, is provisioned Admin so the install has an
// owner.
type User struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	PasswordHash string `json:"-"` // empty for OIDC-only users

	// AccountType is stored, not derived. See the type.
	AccountType AccountType `json:"account_type"`

	// Admin is the whole of platform administration. It replaced a
	// role string plus a super_user boolean, which no code anywhere
	// told apart: IsAdmin ORed them and the console ORed them again,
	// so an install could have a user who was admin by one and not the
	// other, and nothing could act on the difference.
	Admin    bool `json:"admin"`
	Disabled bool `json:"disabled"`

	// EmailVerified is FALSE only for a self-registered account that
	// has not confirmed its address yet. Every trusted creation path
	// (admin, invitation, OIDC, bootstrap) sets it true at creation -
	// the zero value is deliberately the locked state, so a creation
	// site that forgets the field fails closed.
	EmailVerified bool   `json:"email_verified"`
	TOTPSecret    string `json:"-"`
	TOTPEnabled   bool   `json:"totp_enabled"`

	// PasskeyCount is resolved when the row is read and never stored,
	// like a contact's suppressed flag. The admin user list needs it
	// for one thing: a Reset passkeys button that is disabled when
	// there is nothing to reset, the way Reset 2FA already is.
	PasskeyCount int        `json:"passkey_count"`
	CreatedAt    time.Time  `json:"created_at"`
	LastLoginAt  *time.Time `json:"last_login_at,omitempty"`
}

// IsAdmin reports whether the account administers the installation.
// A method rather than the bare field, because this is the question
// every caller actually asks.
func (u *User) IsAdmin() bool { return u.Admin }

// ManagesOwnCredentials reports whether this account may change its
// own password, enrol a passkey and turn 2FA on or off. False only
// for an account an identity provider owns.
func (u *User) ManagesOwnCredentials() bool { return u.AccountType != AccountOIDC }
