// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package sender models approved sender addresses. An address can
// only be registered when its domain is verified by the project,
// and the console offers these in every From selector.
package sender

import "time"

// Sender is one approved From address.
type Sender struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	CreatedBy string `json:"created_by,omitempty"`

	// Email is the full lowercase address (billing@example.com).
	Email string `json:"email"`

	// Name is the optional display name used as "Name <email>".
	Name      string    `json:"name,omitempty"`
	CreatedAt time.Time `json:"created_at"`

	// Signing is the key this address signs its mail with, nil when
	// it has none. Listed with the sender, without the key material.
	Signing *SigningKey `json:"signing,omitempty"`
}

// Signature kinds, the values of SigningKey.Kind.
const (
	SigningPGP   = "pgp"
	SigningSMIME = "smime"
)

// SigningKey is the S/MIME certificate or OpenPGP key of one sender.
type SigningKey struct {
	SenderID  string `json:"-"`
	ProjectID string `json:"-"`

	// SenderEmail is filled by the expiry sweep's read, which has no
	// sender row to hand, and is empty everywhere else.
	SenderEmail string `json:"-"`
	Kind        string `json:"kind"`

	// PrivateKey is the armored OpenPGP private key or the PKCS#8 PEM,
	// sealed at rest and never on the wire.
	PrivateKey string `json:"-"`

	// PublicKey is the armored public key or the PEM chain, leaf
	// first. Not in a listing, fetched on its own.
	PublicKey string `json:"-"`

	// Fingerprint, Algorithm, Subject and Issuer describe the public
	// half for a person, computed at import.
	Fingerprint string `json:"fingerprint"`
	Algorithm   string `json:"algorithm"`
	Subject     string `json:"subject"`
	Issuer      string `json:"issuer,omitempty"`

	// NotAfter is when the key stops being accepted, nil for a key
	// without an expiry.
	NotAfter *time.Time `json:"not_after,omitempty"`

	// Sign says mail from the address is signed. Off keeps the key
	// stored and stops using it.
	Sign bool `json:"sign"`

	// AttachKey says a PGP message carries the public key, as an
	// Autocrypt header and as a file. Meaningless for S/MIME, whose
	// certificate rides inside the signature itself.
	AttachKey bool      `json:"attach_key"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Signs reports whether mail from this sender is signed right now.
func (s *Sender) Signs() bool {
	return s.Signing != nil && s.Signing.Sign
}
