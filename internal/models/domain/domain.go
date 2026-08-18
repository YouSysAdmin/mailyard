// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package domain models recipient domains claimed by a project for
// inbound mail routing. A domain is claimed with a DNS TXT challenge
// and only verified domains accept mail - names are globally unique
// because the MX listener has no project context to disambiguate.
package domain

import "time"

// VerificationPrefix is the TXT record value prefix an operator adds
// at the domain apex: mailyard-verification=<token>.
const VerificationPrefix = "mailyard-verification="

// Domain is one name a project has claimed, verified or not.
type Domain struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	CreatedBy string `json:"created_by,omitempty"`

	// Domain is the bare lowercase name (example.com).
	Domain string `json:"domain"`

	// VerificationToken is shown to the operator so they can create
	// the TXT record. Knowing it is useless without DNS control, so
	// it is not treated as a secret.
	VerificationToken string     `json:"verification_token"`
	Verified          bool       `json:"verified"`
	VerifiedAt        *time.Time `json:"verified_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`

	// DKIMSelector names the DNS record carrying the public key. Held
	// per domain rather than as a global constant because rotation
	// means publishing a new selector next to the old one and cutting
	// over once the new record has propagated.
	DKIMSelector string `json:"dkim_selector,omitempty"`

	// DKIMPrivateKey is the signing key, encrypted by the store on the
	// way in and decrypted on the way out. json:"-" so it can never
	// leave through an API response.
	DKIMPrivateKey string `json:"-"`

	// DKIMPublicKey is the bare base64 DER the operator publishes.
	// Public by definition.
	DKIMPublicKey string `json:"dkim_public_key,omitempty"`

	// The three record checks, each refreshed by POST
	// /api/domains/:id/verify. Separate from Verified, which is
	// ownership alone: a domain can be provably yours and still have
	// no SPF record, and sending must not wait on the others.
	SPFVerified   bool `json:"spf_verified"`
	DKIMVerified  bool `json:"dkim_verified"`
	DMARCVerified bool `json:"dmarc_verified"`

	// CheckedAt is when the three above were last refreshed.
	CheckedAt *time.Time `json:"checked_at,omitempty"`
}

// TXTRecordValue is the exact value the operator must publish.
func (d *Domain) TXTRecordValue() string {
	return VerificationPrefix + d.VerificationToken
}

// CanSign reports whether outbound mail from this domain can be DKIM
// signed: a key exists and the domain is provably ours.
//
// Deliberately not gated on DKIMVerified. The published DNS record is
// what RECEIVERS need in order to check a signature, and until it
// propagates a signature is merely ignored, never held against the
// message. Waiting for our own check to pass would leave mail unsigned
// during exactly the window where the operator has done everything
// right and DNS has not caught up.
func (d *Domain) CanSign() bool {
	return d.Verified && d.DKIMPrivateKey != "" && d.DKIMSelector != ""
}
