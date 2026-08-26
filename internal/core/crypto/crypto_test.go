// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package crypto

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	s := New("test-secret-key")
	for _, plain := range []string{"hunter2", "p@ss with spaces", strings.Repeat("x", 4096)} {
		enc, err := s.Encrypt(plain)
		if err != nil {
			t.Fatalf("encrypt %q: %v", plain, err)
		}

		if enc == plain {
			t.Fatalf("the stored value is the plaintext: %q", enc)
		}

		// A stored value is base64 and nothing else. No prefix, no
		// envelope, no marker.
		if _, err := base64.StdEncoding.DecodeString(enc); err != nil {
			t.Fatalf("stored value is not bare base64: %q", enc)
		}

		got, err := s.Decrypt(enc)
		if err != nil {
			t.Fatalf("decrypt: %v", err)
		}

		if got != plain {
			t.Fatalf("round trip mismatch: got %q want %q", got, plain)
		}
	}
}

// An SMTP server with no password and a provider with no client
// secret are ordinary. Neither should cost a ciphertext, and neither
// should error on the way back.
func TestEmptyStaysEmpty(t *testing.T) {
	s := New("k")
	enc, err := s.Encrypt("")
	if err != nil || enc != "" {
		t.Fatalf("encrypt empty: got %q err %v", enc, err)
	}

	got, err := s.Decrypt("")
	if err != nil || got != "" {
		t.Fatalf("decrypt empty: got %q err %v", got, err)
	}
}

func TestEncryptIsNonDeterministic(t *testing.T) {
	s := New("k")
	a, _ := s.Encrypt("same")
	b, _ := s.Encrypt("same")
	if a == b {
		t.Fatal("two encryptions of the same value must differ (random nonce)")
	}
}

// Without a key the service refuses both directions rather than
// falling back to base64. Storing a reversible encoding in a column
// documented as encrypted at rest is the failure this prevents.
func TestNoKeyRefusesBothDirections(t *testing.T) {
	s := New("")
	if s.Enabled() {
		t.Fatal("an empty secret must leave the service disabled")
	}

	if _, err := s.Encrypt("secret"); !errors.Is(err, ErrNoKey) {
		t.Fatalf("keyless encrypt returned %v, want ErrNoKey", err)
	}

	if _, err := s.Decrypt("anything"); !errors.Is(err, ErrNoKey) {
		t.Fatalf("keyless decrypt returned %v, want ErrNoKey", err)
	}
}

func TestDecryptWithWrongKeyFails(t *testing.T) {
	enc, _ := New("right").Encrypt("secret")
	if _, err := New("wrong").Decrypt(enc); err == nil {
		t.Fatal("decrypting with the wrong key must error")
	}
}

// A value that is not a valid sealed blob is an error, never a
// pass-through. Handing the stored bytes back would send a base64 blob
// to an SMTP server as a password and fail there, with a message about
// authentication rather than about the key.
func TestGarbageIsAnErrorNotAPassThrough(t *testing.T) {
	s := New("k")
	for _, bad := range []string{"not base64 at all!!!", "c2hvcnQ=", "enc:AAAA"} {
		got, err := s.Decrypt(bad)
		if err == nil {
			t.Errorf("Decrypt(%q) returned %q with no error", bad, got)
		}
	}
}
