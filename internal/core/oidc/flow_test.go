// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package oidc

import (
	"strings"
	"testing"
	"time"
)

// TestStateCookieRoundTrips pins verifyState to signState's actual
// layout. The cookie has six dot-separated parts with the signature
// LAST, and the check once read the expiry field as the signature -
// every genuine cookie failed and SSO could not complete at all, with
// nothing in this package saying so.
func TestStateCookieRoundTrips(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	p := StatePayload{State: "st", Nonce: "no", CodeVerifier: "cv", Invite: "inv"}

	cookie := signState(p, secret, time.Now().Add(time.Minute))
	got, err := verifyState(cookie, secret)
	if err != nil {
		t.Fatalf("genuine cookie refused: %v", err)
	}

	if got != p {
		t.Fatalf("payload did not survive the round trip: %+v != %+v", got, p)
	}
}

// TestStateCookieRefusals covers the reject legs: a tampered body, a
// foreign secret, an expired stamp, and a malformed shape.
func TestStateCookieRefusals(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	p := StatePayload{State: "st", Nonce: "no", CodeVerifier: "cv"}
	cookie := signState(p, secret, time.Now().Add(time.Minute))

	if _, err := verifyState(strings.Replace(cookie, "st.", "xx.", 1), secret); err == nil {
		t.Fatal("tampered state accepted")
	}

	if _, err := verifyState(cookie, []byte("another-secret-another-secret-xx")); err == nil {
		t.Fatal("foreign secret accepted")
	}

	if _, err := verifyState(signState(p, secret, time.Now().Add(-time.Minute)), secret); err == nil {
		t.Fatal("expired cookie accepted")
	}

	if _, err := verifyState("a.b.c", secret); err == nil {
		t.Fatal("malformed cookie accepted")
	}
}
