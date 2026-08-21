// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package passkey wraps go-webauthn for passwordless sign-in
// (WebAuthn / FIDO2).
//
// The relying party is built per request from the browser's Origin
// rather than from server.public_url. The Origin is exactly what the
// browser signs into the WebAuthn client data, so deriving the RP id
// and origin from it keeps them equal to what the authenticator bound
// the credential to - in dev on localhost and behind a proxy alike,
// without depending on the proxy to preserve the Host header or on an
// operator keeping public_url in step with reality.
package passkey

import (
	"bytes"
	"encoding/json/v2"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// displayName is what the authenticator shows the user when it asks
// them to confirm. It names the product, not the install.
const displayName = "Mailyard"

// Re-exported so callers and the store never import go-webauthn
// directly. The credential shape is the library's, and keeping that
// dependency in one package is what lets it be replaced without
// touching the handlers.
type (
	Credential          = webauthn.Credential
	SessionData         = webauthn.SessionData
	CredentialCreation  = protocol.CredentialCreation
	CredentialAssertion = protocol.CredentialAssertion
)

// EncodeCredential and DecodeCredential move a credential in and out
// of the opaque JSON column it is stored in.
func EncodeCredential(c *Credential) (string, error) {
	b, err := json.Marshal(c)

	return string(b), err
}

// DecodeCredential parses a stored credential back into its library
// form. Returns an error rather than a zero value, since a credential
// that will not decode cannot authenticate anybody.
func DecodeCredential(raw string) (*Credential, error) {
	var c Credential
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return nil, err
	}

	return &c, nil
}

// Service is a relying party bound to one (rpID, origin).
type Service struct{ wa *webauthn.WebAuthn }

// New builds the RP for rpID (the host, no scheme or port) and origin
// (the exact scheme://host[:port] the browser used).
func New(rpID, origin string) (*Service, error) {
	wa, err := webauthn.New(&webauthn.Config{
		RPID:          rpID,
		RPDisplayName: displayName,
		RPOrigins:     []string{origin},
	})
	if err != nil {
		return nil, err
	}

	return &Service{wa: wa}, nil
}

// User adapts a console account to webauthn.User.
type User struct {
	Handle  []byte
	Name    string // email
	Display string // falls back to Name
	Creds   []Credential
}

// WebAuthnID implements webauthn.User.
func (u *User) WebAuthnID() []byte { return u.Handle }

// WebAuthnName implements webauthn.User.
func (u *User) WebAuthnName() string { return u.Name }

// WebAuthnDisplayName implements webauthn.User.
func (u *User) WebAuthnDisplayName() string {
	if u.Display != "" {
		return u.Display
	}

	return u.Name
}

// WebAuthnCredentials implements webauthn.User, listing what this
// account may sign with.
func (u *User) WebAuthnCredentials() []Credential { return u.Creds }

// BeginRegistration starts enrollment of a new discoverable (resident)
// passkey, excluding the credentials already on the account so the
// same authenticator is not registered twice.
//
// User verification is required. A passkey signs in on its own, so it
// has to be backed by a biometric or a PIN - possession plus
// verification in one step - rather than mere presence. The library
// only enforces the UV flag when the ceremony asked for it, so asking
// here is what makes the later check meaningful.
func (s *Service) BeginRegistration(u *User) (*CredentialCreation, *SessionData, error) {
	exclude := make([]protocol.CredentialDescriptor, 0, len(u.Creds))
	for i := range u.Creds {
		exclude = append(exclude, u.Creds[i].Descriptor())
	}

	return s.wa.BeginRegistration(u,
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementRequired,
			UserVerification: protocol.VerificationRequired,
		}),
		webauthn.WithExclusions(exclude),
	)
}

// FinishRegistration verifies the attestation in body and returns the
// new credential.
func (s *Service) FinishRegistration(u *User, sess SessionData, body []byte) (*Credential, error) {
	parsed, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	return s.wa.CreateCredential(u, sess, parsed)
}

// BeginLogin starts a usernameless (discoverable) assertion, so the
// browser shows its passkey picker and no email is typed.
//
// User verification is required to match registration, for the same
// reason: without it a sign-in would prove somebody tapped a key, not
// that they unlocked it, which is too weak to be the only factor.
func (s *Service) BeginLogin() (*CredentialAssertion, *SessionData, error) {
	return s.wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
}

// FinishLogin verifies the assertion in body. resolve maps the
// authenticator's (rawID, userHandle) to the owning user, and the
// matched User comes back alongside the validated credential so the
// caller can mint a session and persist the new sign count.
//
// resolve is a plain func rather than a webauthn.User factory so that
// callers still avoid importing go-webauthn.
func (s *Service) FinishLogin(resolve func(rawID, userHandle []byte) (*User, error), sess SessionData, body []byte) (*Credential, *User, error) {
	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}

	var matched *User
	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		u, err := resolve(rawID, userHandle)
		if err != nil {
			return nil, err
		}

		matched = u

		return u, nil
	}
	cred, err := s.wa.ValidateDiscoverableLogin(handler, sess, parsed)

	return cred, matched, err
}
