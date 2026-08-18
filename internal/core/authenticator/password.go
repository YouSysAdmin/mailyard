// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package authenticator owns the credential primitives: bcrypt
// password hashing + verification, HS256 JWT minting + parsing for
// the session cookie. OIDC handlers under internal/domain/auth use
// the JWT primitives here once the IdP exchange has succeeded.
package authenticator

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

const (
	passwordCost       = 12
	generatedPasswordN = 16 // base64-url chars; ~96 bits of entropy
)

// Username brute force protection.
// dummyHash is a real cost-12 bcrypt hash over an unknown random
// secret, computed once at first use.
// The login handler runs VerifyDummyPassword on the "user not found" / "user disabled"
// branches so response timing does not leak which leg failed.
var dummyHash = sync.OnceValue(func() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		// Should never happen - bcrypt.GenerateFromPassword on empty
		// is a degraded fallback that still parses.
		raw = []byte("dummy-fallback-secret-never-matches-real-input")
	}

	h, err := bcrypt.GenerateFromPassword(raw, passwordCost)
	if err != nil {
		return ""
	}

	return string(h)
})

// VerifyDummyPassword runs a bcrypt verify against an unguessable
// fixed hash so the login path's negative branches consume the same
// time as the positive branch.
// Always returns false - the caller uses it purely for timing.
func VerifyDummyPassword(plaintext string) bool {
	h := dummyHash()
	if h == "" {
		return false
	}

	return bcrypt.CompareHashAndPassword([]byte(h), []byte(plaintext)) == nil
}

// HashPassword returns a bcrypt hash at the cost this package fixes.
// Never store the plaintext alongside it.
func HashPassword(plaintext string) (string, error) {
	if plaintext == "" {
		return "", errors.New("password required")
	}

	h, err := bcrypt.GenerateFromPassword([]byte(plaintext), passwordCost)
	if err != nil {
		return "", err
	}

	return string(h), nil
}

// VerifyPassword reports whether plaintext matches hash. A malformed
// hash is false, never an error, so a corrupt row cannot admit
// anybody.
func VerifyPassword(hash, plaintext string) bool {
	if hash == "" || plaintext == "" {
		return false
	}

	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext)) == nil
}

// GeneratePassword returns a URL-safe random password.
// Used by the bootstrap flow to mint the operator's initial credential when the
// users table is empty - the plaintext is logged once at startup
// then never recoverable.
func GeneratePassword() (string, error) {
	raw := make([]byte, (generatedPasswordN*3+3)/4)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(raw)[:generatedPasswordN], nil
}
