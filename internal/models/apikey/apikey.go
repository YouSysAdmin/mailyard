// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package apikey is the machine credential for the /api/v1 surface.
// Only the SHA-256 hash of a key is stored - the plaintext exists
// once, in the creation response. The KeyPrefix column (first characters
// of the token) is the lookup index.
package apikey

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"net"
	"time"
)

// Prefix marks every token so leaked keys are recognizable by secret
// scanners. AdminPrefix does the same for a platform credential, and
// the two differ so the auth path knows which TABLE to read from the
// token alone - one lookup, not two.
const (
	Prefix      = "myk_"
	AdminPrefix = "mya_"

	// prefixLen is how many characters of the token are stored as the lookup index.
	prefixLen = 12
)

// Key is one API key bound to exactly one project.
type Key struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	CreatedBy string `json:"created_by,omitempty"`
	Name      string `json:"name"`
	KeyHash   string `json:"-"`
	KeyPrefix string `json:"key_prefix"`

	// Permissions are "resource:action" strings from the catalogue in
	// internal/models/permission - the same vocabulary a project
	// member is judged by, resolved through permission.ForKey.
	//
	// One vocabulary, shared with members, rather than a separate list
	// of scopes that nothing translates into permissions. A key is
	// narrowed by what it holds and by nothing else - there is no
	// underlying tier for it to fall back to.
	//
	// Empty grants nothing.
	Permissions []string `json:"permissions"`
	AllowedIPs  []string `json:"allowed_ips"`

	// Sandbox captures everything sent with this key into the project
	// sandbox instead of delivering it. See the same field on
	// smtpcredential.Credential for why it lives on the credential and
	// not in the request.
	//
	// Not a permission. Permissions say what a key is allowed to do,
	// and this says where what it does ends up - a sandbox key still
	// needs emails:write, and folding it into the catalogue would make
	// "may send" and "may send for real" the same word.
	Sandbox    bool       `json:"sandbox"`
	Revoked    bool       `json:"revoked"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// Admin is a platform credential: users, plans, identity providers,
// the shared SMTP pool, installation settings.
//
// Its own table and its own type rather than a flag on Key, following
// shared_smtp_servers: api_keys carries `project_id NOT NULL` and every
// query scopes on it first, so a platform-wide row would break the
// constraint and the scoping both, and one missing clause would list
// another tenant's credentials.
//
// It carries no permissions. The catalogue governs what a member may
// do inside a project, and none of its resources describe creating a
// user or editing a plan. An admin key is admin or it does not exist -
// which is why minting one is a decision, not a convenience.
type Admin struct {
	ID         string     `json:"id"`
	CreatedBy  string     `json:"created_by,omitempty"`
	Name       string     `json:"name"`
	KeyHash    string     `json:"-"`
	KeyPrefix  string     `json:"key_prefix"`
	AllowedIPs []string   `json:"allowed_ips"`
	Revoked    bool       `json:"revoked"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// IsValid reports whether the key is usable at now (not revoked, not
// expired).
func (k *Key) IsValid(now time.Time) bool { return usable(k.Revoked, k.ExpiresAt, now) }

// AllowsIP checks the caller's address against the allowlist (exact
// IPs or CIDRs). An empty list allows any address.
func (k *Key) AllowsIP(ip string) bool { return ipAllowed(k.AllowedIPs, ip) }

// IsValid and AllowsIP, for a platform credential. Same rules, and
// shared implementations so the two cannot drift into disagreeing
// about what "expired" or "allowed" means.
func (k *Admin) IsValid(now time.Time) bool { return usable(k.Revoked, k.ExpiresAt, now) }

// AllowsIP reports whether ip is within the key's allowlist. An empty
// allowlist admits every address.
func (k *Admin) AllowsIP(ip string) bool { return ipAllowed(k.AllowedIPs, ip) }

func usable(revoked bool, expires *time.Time, now time.Time) bool {
	if revoked {
		return false
	}

	return expires == nil || !now.After(*expires)
}

func ipAllowed(list []string, ip string) bool {
	if len(list) == 0 {
		return true
	}

	addr := net.ParseIP(ip)
	if addr == nil {
		return false
	}

	for _, allowed := range list {
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

// Generate mints a token: plaintext (returned once), the stored
// lookup prefix, and the stored hash.
func Generate() (plaintext, prefix, hash string, err error) { return generate(Prefix) }

// GenerateAdmin mints a platform credential.
func GenerateAdmin() (plaintext, prefix, hash string, err error) { return generate(AdminPrefix) }

func generate(marker string) (plaintext, prefix, hash string, err error) {
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", "", "", err
	}

	plaintext = marker + base64.RawURLEncoding.EncodeToString(raw)

	return plaintext, TokenPrefix(plaintext), Hash(plaintext), nil
}

// TokenPrefix returns the lookup prefix of a presented token.
func TokenPrefix(token string) string {
	if len(token) < prefixLen {
		return token
	}

	return token[:prefixLen]
}

// Hash returns the stored form of a token.
func Hash(token string) string {
	sum := sha256.Sum256([]byte(token))

	return hex.EncodeToString(sum[:])
}

// HashEquals compares a presented token against the stored hash in
// constant time.
func HashEquals(token, storedHash string) bool {
	return subtle.ConstantTimeCompare([]byte(Hash(token)), []byte(storedHash)) == 1
}
