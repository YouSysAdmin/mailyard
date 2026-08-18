// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package certificate is the stored form of a certificate and its
// private key.
//
// Certificates live in the database rather than on a node's disk for
// two reasons that are really the same reason. A relay CA key in a
// file belongs to whichever node minted it: no other node can sign
// with it, and it does not survive that machine. An ACME certificate
// in a local autocert DirCache is likewise per node, which makes
// several nodes serving one hostname order several certificates and
// walk into Let's Encrypt's duplicate limit. Both are cases of a
// cluster-wide secret being kept somewhere only one member can see.
package certificate

import "time"

// Scopes. A scope says what kind of thing an entry is and who goes
// looking for it.
const (
	// ScopeACME holds autocert cache entries verbatim. Name is
	// autocert own cache key, which is why nothing here parses it.
	ScopeACME = "acme"

	// ScopeSelfSigned holds a generated self-signed pair, so a
	// restart or a second node does not mint a different one and
	// break every pinned peer.
	ScopeSelfSigned = "selfsigned"

	// ScopeRelayCA is the certificate authority that signs relay node
	// certificates. Exactly one entry, name empty.
	ScopeRelayCA = "relay-ca"

	// ScopeRelayClient is the client certificate workers present when
	// connecting to a relay node. Exactly one entry, name empty.
	ScopeRelayClient = "relay-client"

	// ScopeRelayNode holds an issued node certificate, keyed by node
	// id. The node has the private half - what is kept here is the
	// public leaf, so the console can show what was issued and to
	// whom without being able to impersonate it.
	ScopeRelayNode = "relay-node"
)

// Certificate is one stored entry.
type Certificate struct {
	Scope string `json:"scope"`
	Name  string `json:"name"`

	// Data is sealed at rest and never serialized. For ScopeRelayNode
	// it is empty: we issued that certificate and deliberately did not
	// keep the key.
	Data string `json:"-"`

	// CertPEM is the public certificate, readable without the
	// encryption key.
	CertPEM   string     `json:"cert_pem,omitempty"`
	NotAfter  *time.Time `json:"not_after,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// ExpiresWithin reports whether the entry runs out inside d. An entry
// with no parsed expiry answers false - unknown is not urgent, and
// treating it as urgent would make every ACME blob look like it was
// about to die.
func (c *Certificate) ExpiresWithin(d time.Duration, now time.Time) bool {
	if c.NotAfter == nil {
		return false
	}

	return c.NotAfter.Sub(now) <= d
}
