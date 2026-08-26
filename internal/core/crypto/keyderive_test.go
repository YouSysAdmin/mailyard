package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

// Only the HKDF key opens a row. A bare SHA-256 of the secret is a
// different key and is refused like any other wrong one.
func TestTheLegacySHA256KeyIsNotAccepted(t *testing.T) {
	const secret, plain = "an-operator-secret", "hunter2"

	legacy := sha256.Sum256([]byte(secret))
	block, _ := aes.NewCipher(legacy[:])
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}

	stored := base64.StdEncoding.EncodeToString(
		gcm.Seal(nonce, nonce, []byte(plain), nil))

	if got, err := New(secret).Decrypt(stored); err == nil {
		t.Fatalf("a ciphertext under the bare-SHA-256 key opened to %q", got)
	}
}

// New writes use the HKDF key.
func TestEncryptUsesDerivedKey(t *testing.T) {
	const secret, plain = "an-operator-secret", "hunter2"
	s := New(secret)
	sealed, err := s.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}

	legacy := sha256.Sum256([]byte(secret))
	raw, _ := base64.StdEncoding.DecodeString(sealed)
	if _, err := openWith(legacy[:], raw); err == nil {
		t.Fatal("new ciphertext opened under the legacy key, so HKDF is not being used")
	}

	got, err := s.Decrypt(sealed)
	if err != nil || got != plain {
		t.Fatalf("round trip: got %q err %v", got, err)
	}
}

// An empty secret derives NO key. HKDF over nothing is a perfectly
// good-looking 64-character string that anyone can compute from the
// label, and every consumer that asks "is a key configured" by testing
// for "" has to get "".
func TestDeriveKeyOfAnEmptySecretIsEmpty(t *testing.T) {
	if got := DeriveKey("", KeyTracking); got != "" {
		t.Fatalf("DeriveKey(\"\") = %q, want empty", got)
	}

	if got := DeriveKey("an-operator-secret", KeyTracking); len(got) != 64 {
		t.Fatalf("DeriveKey(secret) = %q, want 64 hex chars", got)
	}
}
