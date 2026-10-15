// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"

	"github.com/yousysadmin/mailyard/internal/core/authenticator"
	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	akmodel "github.com/yousysadmin/mailyard/internal/models/apikey"
)

// The body ceiling is decided per request, from the path alone, and in
// three tiers. The variants matter: the router is case insensitive and
// forgives a trailing slash, so a ceiling keyed on the raw string would
// be sidestepped by /API/V1/Emails/Send/.
func TestTheBodyCeilingIsDecidedByPath(t *testing.T) {
	for _, tc := range []struct {
		path string
		want int
	}{
		{"/api/v1/emails/send", 0},
		{"/API/V1/Emails/Send/", 0},
		{"/api/v1/emails/batch", 0},
		{"/api/v1/templates/0195d1a2-7c3e-7f00-8000-0123456789ab/attachments", 0},
		{"/api/v1/templates/import", 0},
		{"/api/relay-nodes/inbound", 0},

		{"/api/v1/templates", apiBodyLimit},
		{"/api/v1/templates/0195d1a2-7c3e-7f00-8000-0123456789ab", apiBodyLimit},
		{"/api/v1/templates/0195d1a2-7c3e-7f00-8000-0123456789ab/send-test", apiBodyLimit},
		{"/api/v1/emails/verify", apiBodyLimit},
		{"/api/v1/subscribers/import/csv", apiBodyLimit},
		{env.ConsolePath + "/api/projects", apiBodyLimit},

		{env.ConsolePath + "/api/auth/login", baseBodyLimit},
		{env.ConsolePath + "/api/auth/register", baseBodyLimit},
		{"/webhooks/ses", baseBodyLimit},
		{"/healthz", baseBodyLimit},
		{"/tracking/open/x.gif", baseBodyLimit},
		{"/", baseBodyLimit},
	} {
		if got := bodyLimitForPath(tc.path); got != tc.want {
			t.Errorf("bodyLimitForPath(%q) = %d, want %d", tc.path, got, tc.want)
		}
	}

	// Off the attachment routes naming a credential keeps the server's
	// read timeout, but a body above baseBodyLimit still needs it to
	// verify. One declared within baseBodyLimit is not checked at all.
	limits := perRequestLimits(unverified)
	var h fasthttp.RequestHeader
	h.SetRequestURI("/api/v1/templates")
	h.Set(fiber.HeaderAuthorization, "Bearer x")
	h.SetContentLength(2 * baseBodyLimit)
	if got := limits(&h); got.MaxRequestBodySize != baseBodyLimit || got.ReadTimeout != 0 {
		t.Errorf("templates with an unverified credential = %d, %v, want baseBodyLimit and the server's timeout", got.MaxRequestBodySize, got.ReadTimeout)
	}

	h.SetContentLength(-1)
	if got := limits(&h).MaxRequestBodySize; got != baseBodyLimit {
		t.Errorf("a chunked body with an unverified credential = %d, want baseBodyLimit", got)
	}

	if got := perRequestLimits(verifiedAlways)(&h).MaxRequestBodySize; got != apiBodyLimit {
		t.Errorf("templates with a verified credential = %d, want apiBodyLimit", got)
	}

	h.Del(fiber.HeaderAuthorization)
	if got := limits(&h).ReadTimeout; got != uncredentialedReadTimeout {
		t.Errorf("read timeout with no credential = %v, want %v", got, uncredentialedReadTimeout)
	}
}

// unverified is a verifier no credential passes.
func unverified(*fasthttp.RequestHeader) bool { return false }

// verifiedAlways is a verifier every request passes.
func verifiedAlways(*fasthttp.RequestHeader) bool { return true }

// apiKeys answers one project key.
type apiKeys struct {
	store.APIKeyStore
	key *akmodel.Key
}

func (s apiKeys) GetByPrefix(_ context.Context, prefix string) (*akmodel.Key, error) {
	if s.key.KeyPrefix == prefix {
		return s.key, nil
	}

	return nil, nil
}

// credentials is a runtime holding one working key of each kind, one
// wrong secret for that key, and a working session token.
type credentials struct {
	rt                       *env.Runtime
	key, wrong, admin, token string
}

func newCredentials(t *testing.T) credentials {
	t.Helper()

	key, keyPrefix, keyHash, err := akmodel.Generate()
	if err != nil {
		t.Fatal(err)
	}

	admin, adminPrefix, adminHash, err := akmodel.GenerateAdmin()
	if err != nil {
		t.Fatal(err)
	}

	wrong := key[:len(key)-1] + "x"
	if wrong == key {
		wrong = key[:len(key)-1] + "y"
	}

	const secret = "0123456789abcdef0123456789abcdef"
	token, err := authenticator.CreateToken(crypto.DeriveKey(secret, crypto.KeySession),
		"0195d1a2-7c3e-7f00-8000-0123456789ab", "a@example.test", "", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	rt := &env.Runtime{
		Config: &env.Config{Auth: env.AuthConfig{JWTSecret: secret}},
		Store: &store.Store{
			APIKey:      apiKeys{key: &akmodel.Key{ID: "k1", KeyPrefix: keyPrefix, KeyHash: keyHash}},
			AdminAPIKey: adminKeys{key: &akmodel.Admin{ID: "a1", KeyPrefix: adminPrefix, KeyHash: adminHash}},
		},
	}

	return credentials{rt: rt, key: key, wrong: wrong, admin: admin, token: token}
}

// fasthttp allocates the body buffer from the declared Content-Length
// before reading a byte, so any ceiling above baseBodyLimit and the long
// read timeout go only to a credential that VERIFIES. Naming one is not
// enough.
func TestOnlyAVerifiedCredentialLiftsTheBodyCeiling(t *testing.T) {
	cr := newCredentials(t)
	limits := perRequestLimits(credentialVerifier(cr.rt))

	revoked := &akmodel.Key{ID: "k2", Revoked: true}
	revokedToken, revokedPrefix, revokedHash, err := akmodel.Generate()
	if err != nil {
		t.Fatal(err)
	}

	revoked.KeyPrefix, revoked.KeyHash = revokedPrefix, revokedHash

	forged, err := authenticator.CreateToken(crypto.DeriveKey("another-secret-of-at-least-32-chars", crypto.KeySession),
		"0195d1a2-7c3e-7f00-8000-0123456789ab", "a@example.test", "", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, header, cookie string
		verified             bool
	}{
		{"a project key", "Bearer " + cr.key, "", true},
		{"a platform key", "Bearer " + cr.admin, "", true},
		{"a session bearer", "Bearer " + cr.token, "", true},
		{"a session cookie", "", cr.token, true},

		{"no credential", "", "", false},
		{"a made-up project key", "Bearer myk_madeup", "", false},
		{"a project key with a wrong secret", "Bearer " + cr.wrong, "", false},
		{"a made-up platform key", "Bearer mya_madeup", "", false},
		{"a made-up bearer", "Bearer x", "", false},
		{"a forged session bearer", "Bearer " + forged, "", false},
		{"a made-up session cookie", "", "x", false},
		{"a made-up bearer beside a working cookie", "Bearer x", cr.token, false},
	} {
		for path, verifiedLimit := range map[string]int{"/api/v1/emails/send?sandbox=true": 0, "/api/v1/templates": apiBodyLimit} {
			var h fasthttp.RequestHeader
			h.SetRequestURI(path)
			h.SetContentLength(2 * baseBodyLimit)
			if tc.header != "" {
				h.Set(fiber.HeaderAuthorization, tc.header)
			}

			if tc.cookie != "" {
				h.SetCookie("mailyard_session", tc.cookie)
			}

			got := limits(&h)
			wantLimit := baseBodyLimit
			if tc.verified {
				wantLimit = verifiedLimit
			}

			if got.MaxRequestBodySize != wantLimit {
				t.Errorf("%s on %s: limit %d, want %d", tc.name, path, got.MaxRequestBodySize, wantLimit)
			}

			if path != "/api/v1/templates" {
				wantTimeout := uncredentialedReadTimeout
				if tc.verified {
					wantTimeout = 0
				}

				if got.ReadTimeout != wantTimeout {
					t.Errorf("%s on %s: timeout %v, want %v", tc.name, path, got.ReadTimeout, wantTimeout)
				}
			}
		}
	}

	// A relay node proves itself in the body, so the inbound route keeps
	// apiBodyLimit without a header credential.
	var relay fasthttp.RequestHeader
	relay.SetRequestURI("/api/relay-nodes/inbound")
	relay.SetContentLength(2 * baseBodyLimit)
	if got := limits(&relay).MaxRequestBodySize; got != apiBodyLimit {
		t.Errorf("relay inbound with no credential: limit %d, want apiBodyLimit", got)
	}

	relay.SetRequestURI("/api/relay-nodes/report")
	if got := limits(&relay).MaxRequestBodySize; got != baseBodyLimit {
		t.Errorf("relay report with no credential: limit %d, want baseBodyLimit", got)
	}

	// A revoked key is a key that does not verify.
	cr.rt.Store.APIKey = apiKeys{key: revoked}
	var h fasthttp.RequestHeader
	h.SetRequestURI("/api/v1/emails/send")
	h.Set(fiber.HeaderAuthorization, "Bearer "+revokedToken)
	if got := limits(&h).MaxRequestBodySize; got != baseBodyLimit {
		t.Errorf("a revoked key: limit %d, want baseBodyLimit", got)
	}
}

// fasthttp buffers a body BEFORE any handler runs, so the only thing
// that can refuse a large one on the login route is the per-request
// config handed back from HeaderReceived. This posts the same
// over-the-tier body at the three tiers and checks that only the
// attachment route takes it - over a real listener, because a unit
// test of bodyLimitForPath says nothing about whether fasthttp honors
// the field, and app.Test surfaces the refusal as a transport error
// rather than the 413 a client sees.
func TestALargeBodyIsRefusedOffTheAttachmentRoutes(t *testing.T) {
	const serverLimit = 4 * apiBodyLimit
	app := fiber.New(fiber.Config{BodyLimit: serverLimit, ErrorHandler: errorHandler})
	cr := newCredentials(t)
	app.Server().HeaderReceived = perRequestLimits(credentialVerifier(cr.rt))
	ok := func(c fiber.Ctx) error { return c.SendString("ok") }
	app.Post("/api/v1/emails/send", ok)
	app.Post("/api/v1/templates", ok)
	app.Post(env.ConsolePath+"/api/auth/login", ok)
	app.Post(relayInboundPath, ok)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	go func() { _ = app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true}) }()
	t.Cleanup(func() { _ = app.Shutdown() })

	for _, tc := range []struct {
		path string
		size int
		want int
	}{
		{"/api/v1/emails/send", 2 * apiBodyLimit, fiber.StatusOK},
		{"/api/v1/templates", 2 * apiBodyLimit, fiber.StatusRequestEntityTooLarge},
		{"/api/v1/templates", apiBodyLimit / 2, fiber.StatusOK},
		{env.ConsolePath + "/api/auth/login", 2 * baseBodyLimit, fiber.StatusRequestEntityTooLarge},
		{env.ConsolePath + "/api/auth/login", baseBodyLimit / 2, fiber.StatusOK},
	} {
		if got := post(t, ln.Addr(), tc.path, tc.size, "Authorization: Bearer "+cr.key); got != tc.want {
			t.Errorf("%s with %d bytes answered %d, want %d", tc.path, tc.size, got, tc.want)
		}
	}

	// Anything above baseBodyLimit is for a request whose credential
	// verifies. One naming none, or one that does not verify, gets
	// baseBodyLimit on every API path.
	for _, header := range [][]string{nil, {"Authorization: Bearer myk_madeup"}, {"Cookie: mailyard_session=x"}} {
		for _, path := range []string{"/api/v1/emails/send", "/api/v1/templates"} {
			if got := post(t, ln.Addr(), path, 2*baseBodyLimit, header...); got != fiber.StatusRequestEntityTooLarge {
				t.Errorf("%s with %q and %d bytes answered %d, want 413", path, header, 2*baseBodyLimit, got)
			}

			if got := post(t, ln.Addr(), path, baseBodyLimit/2, header...); got != fiber.StatusOK {
				t.Errorf("%s with %q and %d bytes answered %d, want 200", path, header, baseBodyLimit/2, got)
			}
		}
	}

	// A relay node carries its token in the body, so the inbound route
	// keeps apiBodyLimit with no header credential at all.
	if got := post(t, ln.Addr(), relayInboundPath, 2*baseBodyLimit); got != fiber.StatusOK {
		t.Errorf("relay inbound with %d bytes answered %d, want 200", 2*baseBodyLimit, got)
	}

	if got := post(t, ln.Addr(), relayInboundPath, 2*apiBodyLimit); got != fiber.StatusRequestEntityTooLarge {
		t.Errorf("relay inbound with %d bytes answered %d, want 413", 2*apiBodyLimit, got)
	}

	if got := post(t, ln.Addr(), "/api/v1/emails/send", 2*apiBodyLimit, "Cookie: mailyard_session="+cr.token); got != fiber.StatusOK {
		t.Errorf("send with a working session cookie and %d bytes answered %d, want 200", 2*apiBodyLimit, got)
	}
}

// post sends a body of the given size and reports the status code.
//
// Raw TCP rather than net/http, and the body written on a goroutine
// that ignores its own error: a refused body is answered 413 off the
// Content-Length alone and the connection closed, so a client still
// writing megabytes into it sees a broken pipe - which http.Client
// reports as the error instead of the response it was given.
func post(t *testing.T, addr net.Addr, path string, size int, headers ...string) int {
	t.Helper()

	conn, err := net.Dial("tcp", addr.String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if err := conn.SetDeadline(time.Now().Add(15 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}

	extra := ""
	for _, h := range headers {
		extra += h + "\r\n"
	}

	if _, err := fmt.Fprintf(conn, "POST %s HTTP/1.1\r\nHost: x\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n%s\r\n", path, size, extra); err != nil {
		t.Fatalf("write request: %v", err)
	}

	go func() { _, _ = io.Copy(conn, bytes.NewReader(make([]byte, size))) }()

	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("read status: %v", err)
	}

	var status int
	if _, err := fmt.Sscanf(line, "HTTP/1.1 %d", &status); err != nil {
		t.Fatalf("status line %q: %v", line, err)
	}

	return status
}
