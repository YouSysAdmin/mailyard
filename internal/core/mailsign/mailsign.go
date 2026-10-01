// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package mailsign signs outbound mail as its sender address, with
// S/MIME or OpenPGP.
//
// DKIM already proves the DOMAIN to the receiving server. This proves
// the ADDRESS to the person reading the mail: the client shows a seal
// beside a message whose signature checks against a key it knows, and
// warns on one that does not, which is the one layer of phishing
// defence the reader sees for themselves. S/MIME is what Outlook, Apple
// Mail and Google Workspace verify without plugins, OpenPGP is what
// Thunderbird and GPG users verify, so both are offered behind ONE
// interface and the message builder never learns which it is holding.
//
// Both kinds produce RFC 1847 multipart/signed: the MIME entity is
// signed as a detached blob and the signature travels as the second
// part. The builder owns the entity and the wrapping, this package owns
// the signature and the key material.
package mailsign

import (
	"errors"
	"fmt"
	"time"
)

// Kind names the signature scheme.
const (
	KindPGP   = "pgp"
	KindSMIME = "smime"
)

// Signer produces the detached signature for one MIME entity.
type Signer interface {
	// Protocol is the multipart/signed protocol parameter.
	Protocol() string

	// MicAlg is the multipart/signed micalg parameter.
	MicAlg() string

	// Sign returns the signature part for entity, which the caller has
	// already written with CRLF line endings and 7-bit safe encodings.
	Sign(entity []byte) (Part, error)
}

// Part is the signature as a MIME body part.
type Part struct {
	ContentType      string
	Filename         string
	TransferEncoding string
	Body             []byte
}

// Description is what the console shows about a stored key. Computed
// from the public half, never stored alongside the private one as a
// separate truth.
type Description struct {
	Kind        string
	Fingerprint string
	Algorithm   string
	Subject     string
	Issuer      string

	// NotAfter is nil when the key does not expire. An S/MIME
	// certificate always does, an OpenPGP key usually does not.
	NotAfter *time.Time
}

// Material is a key pair as it is stored: the private half sealed by
// the caller, the public half in the clear.
type Material struct {
	Kind string

	// Private is the armored OpenPGP private key or the PKCS#8 PEM
	// private key, unencrypted either way.
	Private string

	// Public is the armored OpenPGP public key or the PEM certificate
	// chain, leaf first.
	Public string
}

// ErrPassphrase says the private key did not open with what was
// given, which the caller reports on the passphrase field.
var ErrPassphrase = errors.New("the passphrase does not open this key")

// New builds a signer from stored material.
func New(m Material) (Signer, error) {
	switch m.Kind {
	case KindPGP:
		return newPGPSigner(m.Private)
	case KindSMIME:
		return newSMIMESigner(m.Private, m.Public)
	default:
		return nil, fmt.Errorf("mailsign: unknown kind %q", m.Kind)
	}
}

// Describe reads the public half.
func Describe(m Material) (Description, error) {
	switch m.Kind {
	case KindPGP:
		return describePGP(m.Public)
	case KindSMIME:
		return describeSMIME(m.Public)
	default:
		return Description{}, fmt.Errorf("mailsign: unknown kind %q", m.Kind)
	}
}
