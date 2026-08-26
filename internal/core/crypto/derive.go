// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package crypto

import (
	"crypto/sha256"
	"encoding/hex"
	"io"

	"golang.org/x/crypto/hkdf"
)

// Purpose labels for DeriveKey. One label per HMAC consumer of
// auth.jwt_secret so a token minted for one surface can never
// validate on another, even in a hypothetical parser-confusion bug.
const (
	KeySession   = "mailyard/session-jwt/v1"
	KeyOIDCState = "mailyard/oidc-state/v1"
	KeyTracking  = "mailyard/tracking/v1"

	// KeyPasskey seals the WebAuthn ceremony cookie, which holds the
	// challenge and the user-verification requirement between begin
	// and finish. A client able to forge that cookie could replay an
	// old assertion or downgrade the ceremony it is checked against,
	// so it gets a key of its own rather than sharing the at-rest one.
	KeyPasskey = "mailyard/passkey-ceremony/v1"
)

// DeriveKey expands the operator's single jwt_secret into a
// per-purpose subkey via HKDF-SHA256 and returns it hex-encoded (64
// chars = 32 bytes) so existing string-secret APIs stay unchanged.
// Deterministic: same secret + label always yields the same key.
//
// EMPTY IN, EMPTY OUT, the convention crypto.New follows. HKDF over an
// empty secret is a perfectly good-looking 64-character key that anyone
// can compute from the label alone, and every consumer decides "is a
// key configured" by testing for "". Returning "" is what makes a
// consumer handed no secret report itself disabled rather than sign
// with a public key.
func DeriveKey(secret, label string) string {
	if secret == "" {
		return ""
	}

	r := hkdf.New(sha256.New, []byte(secret), nil, []byte(label))
	out := make([]byte, 32)
	if _, err := io.ReadFull(r, out); err != nil {
		// HKDF over SHA-256 cannot fail for 32 bytes. Were it to, no
		// key is the right answer - a fallback hash would be a second,
		// unrelated key, opening nothing sealed under the first.
		return ""
	}

	return hex.EncodeToString(out)
}
