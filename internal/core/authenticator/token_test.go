// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package authenticator

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "0123456789abcdef0123456789abcdef"

// A token round-trips with every claim the session needs.
func TestTokenRoundTrip(t *testing.T) {
	raw, err := CreateToken(testSecret, "u1", "a@example.com", "s1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	c, err := ParseToken(testSecret, raw)
	if err != nil {
		t.Fatal(err)
	}

	if c.UserID != "u1" || c.Email != "a@example.com" || c.SessionID != "s1" {
		t.Fatalf("claims: %+v", c)
	}

	if d := time.Until(c.Expiry); d < 59*time.Minute || d > time.Hour {
		t.Fatalf("expiry %v from now, want about an hour", d)
	}
}

// Everything the parser is told to insist on, refused one at a time:
// another HMAC variant under the same secret, alg none, a sibling
// issuer, a sibling audience, a token with no exp, an expired one, and
// a signature under another secret.
func TestParseTokenRefusesWhatItMust(t *testing.T) {
	now := time.Now()
	base := func() jwt.MapClaims {
		return jwt.MapClaims{
			"user_id": "u1", "email": "a@example.com",
			"iss": jwtIssuer, "aud": jwtAudience,
			"iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(time.Hour).Unix(),
		}
	}
	sign := func(method jwt.SigningMethod, claims jwt.MapClaims, key any) string {
		s, err := jwt.NewWithClaims(method, claims).SignedString(key)
		if err != nil {
			t.Fatal(err)
		}

		return s
	}

	cases := map[string]string{}
	cases["HS512 under the same secret"] = sign(jwt.SigningMethodHS512, base(), []byte(testSecret))
	cases["alg none"] = sign(jwt.SigningMethodNone, base(), jwt.UnsafeAllowNoneSignatureType)
	cases["another secret"] = sign(jwt.SigningMethodHS256, base(), []byte(strings.Repeat("x", 32)))

	c := base()
	c["iss"] = "other-service"
	cases["sibling issuer"] = sign(jwt.SigningMethodHS256, c, []byte(testSecret))

	c = base()
	c["aud"] = "other-audience"
	cases["sibling audience"] = sign(jwt.SigningMethodHS256, c, []byte(testSecret))

	c = base()
	delete(c, "exp")
	cases["no exp"] = sign(jwt.SigningMethodHS256, c, []byte(testSecret))

	c = base()
	c["exp"] = now.Add(-time.Minute).Unix()
	cases["expired"] = sign(jwt.SigningMethodHS256, c, []byte(testSecret))

	for name, raw := range cases {
		if got, err := ParseToken(testSecret, raw); err == nil {
			t.Errorf("%s: accepted as %+v", name, got)
		}
	}
}
