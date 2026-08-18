package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

// A row written before the HKDF change must still decrypt, otherwise
// upgrading loses every stored SMTP password.
func TestDecryptReadsLegacySHA256Ciphertext(t *testing.T) {
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

	got, err := New(secret).Decrypt(stored)
	if err != nil {
		t.Fatalf("legacy ciphertext failed to decrypt: %v", err)
	}

	if got != plain {
		t.Fatalf("got %q, want %q", got, plain)
	}
}

// New writes must use the HKDF key, not the legacy one.
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
