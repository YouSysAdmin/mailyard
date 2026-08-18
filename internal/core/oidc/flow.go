// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package oidc

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// StatePayload carries the state-cookie fields between Authorize +
// Exchange. Random per-request: state guards against CSRF on the
// callback, nonce binds the ID token to this exact request,
// codeVerifier is the PKCE secret the IdP hashes against the
// challenge it received earlier.
type StatePayload struct {
	State        string
	Nonce        string
	CodeVerifier string

	// Invite is the project invitation token the sign-in started from,
	// empty for an ordinary one. It rides here rather than in a
	// parameter on the redirect URL because this payload is already
	// signed and already expires - so the callback can trust it without
	// a second mechanism, and an IdP that drops unknown parameters
	// cannot lose it.
	//
	// It MUST NOT contain a dot: the serialization below is
	// dot-separated. The only writer validates it as hex first, which is
	// the shape the invitation tokens have.
	Invite string
}

// Authorize mints fresh state/nonce/verifier and returns the IdP
// redirect URL plus a signed cookie value the caller stores so the
// callback can recover the same payload.
func (p *Provider) Authorize(jwtSecret []byte, invite string) (redirectURL, cookieValue string) {
	pay := StatePayload{
		State:        randHex(16),
		Nonce:        randHex(16),
		CodeVerifier: randURLB64(32),
		Invite:       invite,
	}
	cookieValue = signState(pay, jwtSecret, time.Now().Add(StateCookieTTL))
	codeChallenge := pkceChallenge(pay.CodeVerifier)
	redirectURL = p.oauth2.AuthCodeURL(pay.State,
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		gooidc.Nonce(pay.Nonce),
	)

	return redirectURL, cookieValue
}

// Claims is the subset of ID-token + UserInfo claims the callback
// needs to make an admit/deny decision.
type Claims struct {
	Subject       string         `json:"sub"`
	Email         string         `json:"email"`
	EmailVerified bool           `json:"email_verified"`
	Name          string         `json:"name"`
	Raw           map[string]any `json:"-"` // for groups_claim lookup
}

// Exchange verifies the callback URL parameters against the
// previously-set state cookie, exchanges the code, validates the ID
// token (signature + audience + nonce + expiry), and returns the
// resolved claims.
func (p *Provider) Exchange(ctx context.Context, jwtSecret []byte, returnedState, code, cookieValue string) (*Claims, string, error) {
	pay, err := verifyState(cookieValue, jwtSecret)
	if err != nil {
		return nil, "", fmt.Errorf("state cookie: %w", err)
	}

	if pay.State == "" || pay.State != returnedState {
		return nil, "", errors.New("state mismatch (CSRF guard)")
	}

	tok, err := p.oauth2.Exchange(ctx, code,
		oauth2.SetAuthURLParam("code_verifier", pay.CodeVerifier),
	)
	if err != nil {
		return nil, "", fmt.Errorf("token exchange: %w", err)
	}

	// No discovery document means no published signing keys, so an ID
	// token cannot be verified locally. Read identity from the
	// UserInfo endpoint instead: that call is authenticated with the
	// access token we just received over TLS directly from the token
	// endpoint, so the response is trusted for the same reason the
	// token itself is.
	if p.verifier == nil {
		c, uerr := p.userInfoClaims(ctx, tok)

		return c, pay.Invite, uerr
	}

	rawID, ok := tok.Extra("id_token").(string)
	if !ok || rawID == "" {
		return nil, "", errors.New("id_token missing from token response")
	}

	idTok, err := p.verifier.Verify(ctx, rawID)
	if err != nil {
		return nil, "", fmt.Errorf("id_token verify: %w", err)
	}

	if idTok.Nonce != pay.Nonce {
		return nil, "", fmt.Errorf("nonce mismatch")
	}

	var c Claims
	if err := idTok.Claims(&c); err != nil {
		return nil, "", fmt.Errorf("decode id_token claims: %w", err)
	}

	if err := idTok.Claims(&c.Raw); err != nil {
		return nil, "", fmt.Errorf("decode raw claims: %w", err)
	}

	if c.Subject == "" {
		return nil, "", fmt.Errorf("id_token has no sub claim")
	}

	return &c, pay.Invite, nil
}

// userInfoClaims reads identity from the UserInfo endpoint, for
// providers configured with manual endpoints. Name is assembled from
// whichever of the common claims the IdP actually sends.
func (p *Provider) userInfoClaims(ctx context.Context, tok *oauth2.Token) (*Claims, error) {
	if p.userInfoURL == "" {
		return nil, fmt.Errorf("no id_token verifier and no userinfo_url configured")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.userInfoURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("userinfo request: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		// Bounded read: an IdP erroring with a huge body should not
		// become our memory problem.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))

		return nil, fmt.Errorf("userinfo returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var raw map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode userinfo: %w", err)
	}

	c := &Claims{Raw: raw}
	c.Subject, _ = raw["sub"].(string)
	c.Email, _ = raw["email"].(string)
	c.Name, _ = raw["name"].(string)
	// email_verified arrives as a bool from most IdPs and as the
	// string "true" from a few.
	switch v := raw["email_verified"].(type) {
	case bool:
		c.EmailVerified = v
	case string:
		c.EmailVerified = v == "true"
	}

	if c.Name == "" {
		given, _ := raw["given_name"].(string)
		family, _ := raw["family_name"].(string)
		c.Name = strings.TrimSpace(given + " " + family)
	}
	if c.Name == "" {
		c.Name, _ = raw["preferred_username"].(string)
	}

	if c.Subject == "" {
		return nil, fmt.Errorf("userinfo response has no sub claim")
	}

	return c, nil
}

// signState returns "<state>.<nonce>.<code_verifier>.<invite>.<exp>.<sig>",
// HMAC-SHA256 over the first five fields with the supplied secret.
func signState(p StatePayload, secret []byte, exp time.Time) string {
	body := strings.Join([]string{
		p.State, p.Nonce, p.CodeVerifier, p.Invite, strconv.FormatInt(exp.Unix(), 10),
	}, ".")
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(body))

	return body + "." + hex.EncodeToString(mac.Sum(nil))
}

func verifyState(cookie string, secret []byte) (StatePayload, error) {
	parts := strings.Split(cookie, ".")
	if len(parts) != 6 {
		return StatePayload{}, fmt.Errorf("malformed cookie")
	}

	body := strings.Join(parts[:5], ".")
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(body))
	want := mac.Sum(nil)
	got, err := hex.DecodeString(parts[5])
	if err != nil {
		slog.Debug("oidc: state cookie hex decode failed", "err", err)

		return StatePayload{}, fmt.Errorf("bad signature")
	}

	if !hmac.Equal(want, got) {
		slog.Debug("oidc: state cookie hmac mismatch")

		return StatePayload{}, fmt.Errorf("bad signature")
	}

	exp, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil {
		return StatePayload{}, fmt.Errorf("bad expiry")
	}

	if time.Now().Unix() > exp {
		return StatePayload{}, fmt.Errorf("expired (sit too long on IdP page?)")
	}

	return StatePayload{
		State:        parts[0],
		Nonce:        parts[1],
		CodeVerifier: parts[2],
		Invite:       parts[3],
	}, nil
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))

	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("oidc: crypto/rand failed: " + err.Error())
	}

	return hex.EncodeToString(b)
}

func randURLB64(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("oidc: crypto/rand failed: " + err.Error())
	}

	return base64.RawURLEncoding.EncodeToString(b)
}
