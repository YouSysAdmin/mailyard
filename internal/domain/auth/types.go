// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package auth

import (
	pkmodel "github.com/yousysadmin/mailyard/internal/models/passkey"
	sessmodel "github.com/yousysadmin/mailyard/internal/models/session"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
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

// loginInput carries credentials for /api/auth/login. The
// normalize:"normalize" tag (trim+lower) on Email runs before
// validation so a "  Foo@Example.com " becomes "foo@example.com"
// and matches the lower-cased rows in the users table. Password is
// trimmed only - never lower-cased - and kept short of an
// unreasonable upper bound so a runaway POST can't waste bcrypt's
// CPU budget on a 1 MB attempt.
type loginInput struct {
	Email    string `json:"email"    validate:"required,email,max=320" normalize:"normalize"`
	Password string `json:"password" validate:"required,min=1,max=256" normalize:"trim"`

	// TOTPCode is required only for accounts with 2FA enabled.
	TOTPCode string `json:"totp_code" validate:"omitempty,len=6,numeric" normalize:"trim"`
}

// registerInput is the public signup form. Same normalization rules
// as login, but the password minimum is the real account policy (8)
// rather than login's min=1, which only exists to reject empty input
// without leaking policy.
type registerInput struct {
	Email    string `json:"email"    validate:"required,email,max=320" normalize:"normalize"`
	Password string `json:"password" validate:"required,min=8,max=256,bcryptlen" normalize:"trim"`
}

// passkeyReauthInput is the password confirmation guarding the
// credential list. Enrolling and removing are both changes to how the
// account can be entered, so a hijacked session must not be enough.
type passkeyReauthInput struct {
	Password string `json:"password" validate:"required,min=1,max=256" normalize:"trim"`
	Name     string `json:"name"     validate:"omitempty,max=60"        normalize:"trim"`
}

type passkeyRenameInput struct {
	Name string `json:"name" validate:"required,max=60" normalize:"trim"`
}

type resetRequestInput struct {
	Email string `json:"email" validate:"required,email,max=320" normalize:"normalize"`
}

type resetConfirmInput struct {
	Token    string `json:"token"    validate:"required,min=32,max=128" normalize:"trim"`
	Password string `json:"password" validate:"required,min=12,max=256,bcryptlen" normalize:"trim"`
}

// changePasswordInput is the signed-in change, so it proves the
// current password rather than a mailed token.
type changePasswordInput struct {
	CurrentPassword string `json:"current_password" validate:"required,min=1,max=256"`
	Password        string `json:"password"         validate:"required,min=12,max=256,bcryptlen" normalize:"trim"`
}

type verifyConfirmInput struct {
	Token string `json:"token" validate:"required,min=32,max=128" normalize:"trim"`
}

type verifyResendInput struct {
	Email string `json:"email" validate:"required,email,max=320" normalize:"normalize"`
}

// systemMailTestInput optionally names an address to deliver a real
// test message to. Without it the check stops at the connection.
type systemMailTestInput struct {
	To string `json:"to" validate:"omitempty,email,max=320" normalize:"normalize"`
}

type totpCodeInput struct {
	Code string `json:"code" validate:"required,len=6,numeric"`
}

// ----------------------------------------------------------------------------
// Responses
// ----------------------------------------------------------------------------

// The response types of the auth surface.
//
// Declared rather than built as maps for the same reason the machine
// API are: a body with no type is a body nothing can check. These
// are the most security-adjacent responses in the tree, and the one
// property that matters - that no secret rides along - is easier to
// hold when the shape is written down in one place.

// UserResponse carries the signed-in account. The model omits the
// password hash and TOTP secret through json:"-", which is what keeps
// them out of here.
type UserResponse struct {
	User *usermodel.User `json:"user"`
}

// AuthDisabledResponse is the answer on an install running with
// authentication switched off entirely.
//
// It carries the edition too, because /auth/info answers with this
// shape when auth.disabled is set and the console reads the edition
// from that call either way. Without it a console on such an install
// learns nothing about which build it is talking to.
type AuthDisabledResponse struct {
	AuthDisabled bool   `json:"auth_disabled"`
	Edition      string `json:"edition"`
}

// RegisterPendingResponse is returned when self-registration created
// the account but a confirmation link has to be redeemed before it can
// sign in. Deliberately carries no user: there is no session yet.
type RegisterPendingResponse struct {
	VerificationRequired bool   `json:"verification_required"`
	Message              string `json:"message"`
}

// MessageResponse is a bare human-readable outcome, used where the
// answer has to be identical whatever happened - password reset and
// verification resend both answer the same way for a known address, an
// unknown one and a throttled one, so that neither becomes an
// enumeration oracle.
type MessageResponse struct {
	Message string `json:"message"`
}

// AuthInfoResponse tells the unauthenticated sign-in page which
// controls to render.
//
// Public by necessity, so it carries only what a login form needs. The
// provider entries name no client ids, issuers or allowlists, and the
// passkey flag says whether the install offers passkeys at all - never
// whether a particular account has one, which would be an oracle.
type AuthInfoResponse struct {
	// Edition is which build this is - "community" or "enterprise".
	//
	// It sits on this endpoint because this is already the "what does
	// this installation offer" answer, it is the first call the console
	// makes, and it is reachable before anyone has signed in. The
	// alternative was a capability endpoint of its own, which is a
	// second thing to keep in step with the first.
	//
	// The console bundle is identical in both editions - it is public
	// source either way - so every difference a reader sees is a
	// runtime branch on this one string. Nothing is GATED on it: the
	// gate is the absent code, and a page reading this only knows
	// whether to explain itself.
	Edition      string `json:"edition"`
	LocalEnabled bool   `json:"local_enabled"`

	// OIDCEnabled is a convenience: true when Providers is non-empty,
	// so a template does not have to reason about an empty array.
	OIDCEnabled bool            `json:"oidc_enabled"`
	Providers   []LoginProvider `json:"providers"`

	// PasswordResetEnabled is false without system mail: a reset link
	// needs somewhere to be sent, and offering one that always fails is
	// worse than hiding it.
	PasswordResetEnabled bool `json:"password_reset_enabled"`
	RegistrationEnabled  bool `json:"registration_enabled"`
	PasskeysEnabled      bool `json:"passkeys_enabled"`
}

// LoginProvider is one identity provider as the sign-in page sees it.
type LoginProvider struct {
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	Type     string `json:"type"`
	StartURL string `json:"start_url"`
}

// PasskeyListResponse is the caller's enrolled passkeys. No credential
// ids and no key material: neither is any use to a browser here, and a
// credential id is a correlatable identifier.
type PasskeyListResponse struct {
	Passkeys []*pkmodel.Passkey `json:"passkeys"`
}

// PasskeyResponse is one newly enrolled passkey.
type PasskeyResponse struct {
	Passkey *pkmodel.Passkey `json:"passkey"`
}

// RenamedResponse confirms a rename.
type RenamedResponse struct {
	Renamed bool `json:"renamed"`
}

// RemovedResponse confirms a removal.
type RemovedResponse struct {
	Removed bool `json:"removed"`
}

// SessionListResponse is the caller's own sign-ins.
type SessionListResponse struct {
	Sessions []*sessmodel.Session `json:"sessions"`
}

// RevokedResponse reports how many sessions were ended.
type RevokedResponse struct {
	Revoked int64 `json:"revoked"`
}

// TOTPSetupResponse carries the enrolment secret.
//
// This is the one response in the tree that hands out a secret on
// purpose: the authenticator has to be given it, and it is useless
// until a valid code proves the user stored it. It is returned only to
// the account enrolling, behind a live session.
type TOTPSetupResponse struct {
	Secret     string `json:"secret"`
	OTPAuthURL string `json:"otpauth_url"`
}

// TOTPStateResponse reports whether the second factor is now on.
type TOTPStateResponse struct {
	TOTPEnabled bool `json:"totp_enabled"`
}

// PasskeyChallengeResponse is what the two WebAuthn *begin* legs answer:
// the ceremony options, to be handed to navigator.credentials unchanged.
//
// A declared shape for something deliberately opaque. The handler returns
// go-webauthn's own CredentialCreation / CredentialAssertion, which
// marshal to `{"publicKey": {...}}` - and both routes were documented as
// returning nothing at all, so a reader could not tell there was a body,
// let alone that it goes straight to a browser API.
//
// publicKey is a free-form object on purpose. Its contents are the
// WebAuthn spec's, not ours: reflecting the library's struct would paste
// a hundred lines of someone else's schema into our document and pin us
// to their field names. The console types it the same way -
// Record<string, unknown> - because neither side inspects it.
type PasskeyChallengeResponse struct {
	PublicKey map[string]any `json:"publicKey"`
}

// SystemMailStatusResponse describes the platform's own outbound mail.
// Platform admin only, and it never echoes the password - HasAuth says
// whether one is set and nothing more.
type SystemMailStatusResponse struct {
	SystemMail SystemMailStatus `json:"system_mail"`
}

// SystemMailStatus is the configuration as an admin may see it.
type SystemMailStatus struct {
	// Enabled is whether platform mail is configured: a from address
	// is set. Whether the pool can deliver right now is Server below.
	Enabled  bool   `json:"enabled"`
	From     string `json:"from"`
	FromName string `json:"from_name"`

	// Server names the shared pool row platform mail would leave
	// through, empty when the pool holds none it can use. Reserved
	// says that row is marked platform_only, so no tenant shares it.
	Server   string `json:"server"`
	Reserved bool   `json:"reserved"`

	// Problem explains an empty Server in words, since "no server" has
	// two causes an admin fixes differently - an empty pool, or a pool
	// whose rows are all disabled.
	Problem string `json:"problem,omitempty"`
}

// LoginChallenge is a 401 that is not a refusal but a request for one
// more thing.
//
// It carries the error alongside a flag rather than a bare message
// because the console has to tell "wrong password" from "now show the
// code field" and from "go confirm your address", and parsing English
// to decide would break the day the wording changes. Exactly one flag
// is ever set.
type LoginChallenge struct {
	Error string `json:"error"`

	// Requires2FA asks for the authenticator code. Reached only after
	// the password checked out, so it reveals nothing to a guesser.
	Requires2FA bool `json:"requires_2fa,omitempty"`

	// RequiresVerification means the signup link has not been redeemed.
	// Also checked after the password, for the same reason.
	RequiresVerification bool `json:"requires_verification,omitempty"`
}
