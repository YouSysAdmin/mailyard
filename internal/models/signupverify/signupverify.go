// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package signupverify models the single-use token behind signup
// email verification. Same storage discipline as passwordreset: only
// the hash is kept, the plaintext exists in the verification mail and
// nowhere else.
package signupverify

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"time"
)

// TTL is how long a verification link stays usable. Longer than a
// password reset on purpose: redeeming it only proves the mailbox and
// signs in the account that was created seconds before, it cannot
// take over anything that existed already.
const TTL = 24 * time.Hour

// Token is one outstanding verification request.
type Token struct {
	ID     string
	UserID string

	// TokenHash is hex(sha256(token)).
	TokenHash string
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time

	// RequestIP records who asked, for the audit trail.
	RequestIP string
}

// Usable reports whether the token can still be redeemed at now.
func (t *Token) Usable(now time.Time) bool {
	return t.UsedAt == nil && now.Before(t.ExpiresAt)
}

// Generate mints a token: the plaintext (emailed once) and its hash.
func Generate() (plaintext, hash string, err error) {
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", "", err
	}

	plaintext = hex.EncodeToString(raw)

	return plaintext, Hash(plaintext), nil
}

// Hash returns the stored form of a token.
func Hash(token string) string {
	sum := sha256.Sum256([]byte(token))

	return hex.EncodeToString(sum[:])
}

// HashEquals compares a presented token against a stored hash in
// constant time.
func HashEquals(token, storedHash string) bool {
	return subtle.ConstantTimeCompare([]byte(Hash(token)), []byte(storedHash)) == 1
}
