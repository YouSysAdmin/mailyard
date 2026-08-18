// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package smtpcredential is the machine credential for the SMTP
// submission relay. It is deliberately separate from the API key:
// a relay credential unlocks submission and nothing else, so an
// operator can hand one to a legacy application without also handing
// over the HTTP machine API. Only the hash of the password is stored - the
// plaintext exists once, in the creation response.
package smtpcredential

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"time"
)

// UsernamePrefix marks generated usernames so a leaked pair is
// recognizable, and so the relay can tell a credential login from the
// legacy api-key login at AUTH time.
const UsernamePrefix = "smtp_"

// Credential is one relay login bound to exactly one project.
// Unlike an API key it carries no scopes and no expiry - it is
// either usable or revoked.
type Credential struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	CreatedBy string `json:"created_by,omitempty"`
	Name      string `json:"name"`
	Username  string `json:"username"`

	// PasswordHash is hex(sha256(password)). The password is 32
	// random bytes, so a plain digest is sound here - there is no
	// low-entropy guess space for a password stretch to protect.
	PasswordHash string   `json:"-"`
	AllowedIPs   []string `json:"allowed_ips"`

	// SMTPGroupID routes everything submitted with this credential to
	// a named server pool. Empty uses the project's default group.
	// The binding lives here because a relay client speaks SMTP and
	// has no way to pass a routing field with the message.
	SMTPGroupID string `json:"smtp_group_id,omitempty"`

	// Sandbox makes every message submitted with this credential get
	// captured into the project sandbox instead of delivered.
	//
	// On the credential rather than in the request, and that is the
	// whole design: a developer changes what their application
	// authenticates with, nothing else. An application that has to be
	// edited to point at a sandbox is an application that will be
	// deployed still pointing at one, or worse, shipped to production
	// with the flag flipped back and nobody able to tell.
	//
	// One-way, too. A sandbox credential cannot ask for real delivery,
	// so a header nobody meant to leave in cannot turn a test into a send to a customer.
	Sandbox    bool       `json:"sandbox"`
	Revoked    bool       `json:"revoked"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// IsValid reports whether the credential is usable.
func (c *Credential) IsValid() bool { return !c.Revoked }

// AllowsIP checks the caller's address against the allowlist (exact
// IPs or CIDRs). An empty list allows any address.
func (c *Credential) AllowsIP(ip string) bool {
	if len(c.AllowedIPs) == 0 {
		return true
	}

	addr := net.ParseIP(ip)
	if addr == nil {
		return false
	}

	for _, allowed := range c.AllowedIPs {
		if _, cidr, err := net.ParseCIDR(allowed); err == nil {
			if cidr.Contains(addr) {
				return true
			}

			continue
		}

		if a := net.ParseIP(allowed); a != nil && a.Equal(addr) {
			return true
		}
	}

	return false
}

// Generate mints a username plus a password (returned once) and the
// stored hash of that password.
func Generate() (username, password, hash string, err error) {
	uraw := make([]byte, 8)
	if _, err = rand.Read(uraw); err != nil {
		return "", "", "", err
	}

	praw := make([]byte, 32)
	if _, err = rand.Read(praw); err != nil {
		return "", "", "", err
	}

	username = UsernamePrefix + hex.EncodeToString(uraw)
	password = hex.EncodeToString(praw)

	return username, password, Hash(password), nil
}

// Hash returns the stored form of a password.
func Hash(password string) string {
	sum := sha256.Sum256([]byte(password))

	return hex.EncodeToString(sum[:])
}

// HashEquals compares a presented password against the stored hash in
// constant time.
func HashEquals(password, storedHash string) bool {
	return subtle.ConstantTimeCompare([]byte(Hash(password)), []byte(storedHash)) == 1
}
