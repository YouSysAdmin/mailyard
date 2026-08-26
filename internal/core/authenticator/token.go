// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package authenticator

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims is the minimal session payload.
// Single-operator deploy --no org / scopes / refresh-version dance.
// UserID + Email are enough to look the user up on every request.
type Claims struct {
	UserID string    `json:"user_id"`
	Email  string    `json:"email"`
	Expiry time.Time `json:"expiry"`

	// SessionID is the jti claim, naming the row in the sessions
	// table. Empty on tokens minted before session tracking existed -
	// callers must treat that as "not revocable" rather than
	// "invalid", so an in-flight cookie survives the upgrade.
	SessionID string `json:"session_id"`
}

// Issuer + Audience pin session JWTs to this app. If the same
// auth.jwt_secret is ever reused for a sibling service (or by an
// operator who confused two installs), a token minted by the other
// side won't validate here - the iss/aud strings won't match.
// Constants instead of config knobs because the cookie never leaves
// this process - nothing else should produce tokens with these
// markers.
const (
	jwtIssuer   = "mailyard"
	jwtAudience = "mailyard-session"
)

// CreateToken signs a session JWT for the given user. sessionID
// becomes the jti claim and names the row in the sessions table that
// makes the token revocable - pass an empty string for a token that
// is not tracked.
// TTL is the lifetime - a sensible value is 12h-24h for an operator console.
// The secret comes from auth.jwt_secret in YAML (HS256).
func CreateToken(secret, userID, email, sessionID string, ttl time.Duration) (string, error) {
	if secret == "" {
		return "", errors.New("jwt secret not configured")
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"user_id": userID,
		"email":   email,
		"iss":     jwtIssuer,
		"aud":     jwtAudience,
		"iat":     now.Unix(),
		"nbf":     now.Unix(),
		"exp":     now.Add(ttl).Unix(),
	}
	if sessionID != "" {
		claims["jti"] = sessionID
	}

	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	return t.SignedString([]byte(secret))
}

// ParseToken verifies the HMAC signature and returns the embedded claims.
// Expired or malformed tokens fail with a clear error so the caller can
// write a 401.
//
// The parser is told everything it must insist on: HS256 and only HS256
// (so a token signed HS384/HS512 against the same secret, or with no
// algorithm at all, is refused before the key is even asked for), our
// issuer, our audience, and that exp is present - a token that never
// expires is not a session.
func ParseToken(secret, raw string) (*Claims, error) {
	if raw == "" {
		return nil, errors.New("empty token")
	}

	if secret == "" {
		return nil, errors.New("jwt secret not configured")
	}

	mc := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(raw, mc, func(*jwt.Token) (any, error) {
		return []byte(secret), nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(jwtIssuer),
		jwt.WithAudience(jwtAudience),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, err
	}

	if !parsed.Valid {
		return nil, errors.New("invalid token")
	}

	uid, _ := mc["user_id"].(string)
	email, _ := mc["email"].(string)
	if uid == "" {
		return nil, errors.New("invalid token user_id")
	}

	exp, err := mc.GetExpirationTime()
	if err != nil || exp == nil {
		return nil, fmt.Errorf("invalid token exp: %w", err)
	}

	jti, _ := mc["jti"].(string)

	return &Claims{
		UserID:    uid,
		Email:     email,
		Expiry:    exp.Time,
		SessionID: jti,
	}, nil
}
