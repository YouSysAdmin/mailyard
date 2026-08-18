// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package passkey is one enrolled WebAuthn credential on a console
// account.
package passkey

import "time"

// Passkey is a row of user_passkeys.
//
// Credential is the go-webauthn credential as opaque JSON, and it
// carries the PUBLIC key plus the sign counter - there is no secret
// here, the private half never leaves the authenticator. It is still
// json:"-" because it is noise to every caller and because a
// credential id is a correlatable identifier that the console has no
// reason to hand out.
type Passkey struct {
	ID     string `json:"id"`
	UserID string `json:"-"`

	// CredentialID is the authenticator's raw credential id,
	// base64url encoded. Unique across the install: the same
	// authenticator must not be enrolled twice, and the assertion
	// arrives naming this and nothing else.
	CredentialID string `json:"-"`

	// Name is what the account holder called it, so a list of three
	// passkeys can be told apart when one needs removing.
	Name       string     `json:"name"`
	Credential string     `json:"-"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}
