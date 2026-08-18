// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package crypto encrypts secrets at rest - SMTP passwords, TOTP
// secrets, DKIM private keys, OAuth client secrets - with AES-256-GCM.
//
// A stored value is base64(nonce||ciphertext), and nothing else.
//
// No prefix or marker on the stored value. The key is required, so a
// column either holds ciphertext or holds something wrong, and there is
// no fallback encoding to tell it apart from. A marker naming the
// algorithm would earn its place the day the algorithm changes.
//
// Empty in, empty out, in both directions. An SMTP server with no
// password is ordinary, and sealing an empty string would just mean
// every read path unwrapping a nothing.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

// KeyAtRest is the HKDF purpose label for the at-rest encryption key,
// keeping it separate from the HMAC subkeys in derive.go even when an
// operator reuses one secret across both settings.
const KeyAtRest = "mailyard/at-rest/v1"

// ErrNoKey is returned by both directions when the service holds no
// key. Config.Validate makes that unreachable for the at-rest service,
// so it means a caller built one from an empty secret.
var ErrNoKey = errors.New("crypto: no encryption key configured")

// Service holds the derived AES key.
type Service struct {
	key []byte

	// legacyKey is the pre-HKDF derivation, a bare SHA-256 of the
	// secret. Decrypt only: rows written before that change are still
	// sealed under it, and refusing them would lock an operator out of
	// every stored SMTP password on upgrade.
	legacyKey []byte
}

// New derives a 32-byte AES-256 key from secret.
//
// HKDF-SHA256 with a purpose label rather than a bare hash, so this
// key is unrelated to the session, OIDC-state, tracking and passkey
// subkeys even when crypto.encryption_key and auth.jwt_secret happen
// to hold the same string - which, for a single-value deployment,
// they often do. See derive.go.
//
// An empty secret yields a service that refuses to work rather than
// one that quietly stores base64. Storing a reversible encoding in a
// column documented as encrypted at rest is worse than not starting.
func New(secret string) *Service {
	s := NewFor(secret, KeyAtRest)
	if secret != "" {
		legacy := sha256.Sum256([]byte(secret))
		s.legacyKey = legacy[:]
		if s.key == nil {
			// DeriveKey returns hex it produced itself, so this cannot
			// happen. Fall back to the legacy key rather than ending up
			// with no key at all.
			s.key = legacy[:]
		}
	}

	return s
}

// NewFor is New with an explicit HKDF purpose label, for a consumer
// sealing something other than a column at rest - see
// crypto.KeyPasskey. It carries no legacy key: that migration path
// belongs to the at-rest rows and nothing else.
func NewFor(secret, label string) *Service {
	if secret == "" {
		return &Service{}
	}

	key, err := hex.DecodeString(DeriveKey(secret, label))
	if err != nil {
		return &Service{}
	}

	return &Service{key: key}
}

// Enabled reports whether a key is configured.
func (s *Service) Enabled() bool { return s.key != nil }

// Encrypt seals plaintext and returns base64(nonce||ciphertext).
func (s *Service) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	if s.key == nil {
		return "", ErrNoKey
	}

	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", fmt.Errorf("crypto: new cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("crypto: new gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("crypto: nonce: %w", err)
	}

	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plaintext), nil)), nil
}

// Decrypt reverses Encrypt.
//
// Every failure is an error, deliberately. There is no branch that
// hands back the stored bytes when they do not decrypt: that would
// turn a changed key into an SMTP AUTH attempt with a base64 blob for
// a password, which fails somewhere far away with a message about
// authentication rather than about configuration.
func (s *Service) Decrypt(stored string) (string, error) {
	if stored == "" {
		return "", nil
	}

	if s.key == nil {
		return "", ErrNoKey
	}

	raw, err := base64.StdEncoding.DecodeString(stored)
	if err != nil {
		return "", fmt.Errorf("crypto: decode ciphertext: %w", err)
	}

	pt, err := openWith(s.key, raw)
	if err == nil {
		return string(pt), nil
	}

	// GCM authentication failing is what a row sealed under the old
	// bare-SHA-256 key looks like, so try that before calling it a
	// failure. Nothing re-encrypts on read: the row is upgraded the
	// next time something writes it.
	if s.legacyKey != nil {
		if pt, lerr := openWith(s.legacyKey, raw); lerr == nil {
			return string(pt), nil
		}
	}

	return "", err
}

// openWith unseals nonce||ciphertext under one key.
func openWith(key, raw []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: new cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: new gcm: %w", err)
	}

	if len(raw) < gcm.NonceSize() {
		return nil, fmt.Errorf("crypto: ciphertext too short")
	}

	pt, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return nil, fmt.Errorf("crypto: decrypt: %w", err)
	}

	return pt, nil
}
