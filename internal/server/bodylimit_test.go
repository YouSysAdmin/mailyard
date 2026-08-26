// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"

	"github.com/yousysadmin/mailyard/internal/core/env"
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

	// The hook sees the raw request target, query string included.
	var h fasthttp.RequestHeader
	h.SetRequestURI("/api/v1/emails/send?sandbox=true")
	if got := perRequestLimits(&h).MaxRequestBodySize; got != 0 {
		t.Errorf("perRequestLimits with a query string = %d, want the server's limit", got)
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
	app.Server().HeaderReceived = perRequestLimits
	ok := func(c fiber.Ctx) error { return c.SendString("ok") }
	app.Post("/api/v1/emails/send", ok)
	app.Post("/api/v1/templates", ok)
	app.Post(env.ConsolePath+"/api/auth/login", ok)

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
		if got := post(t, ln.Addr(), tc.path, tc.size); got != tc.want {
			t.Errorf("%s with %d bytes answered %d, want %d", tc.path, tc.size, got, tc.want)
		}
	}
}

// post sends a body of the given size and reports the status code.
//
// Raw TCP rather than net/http, and the body written on a goroutine
// that ignores its own error: a refused body is answered 413 off the
// Content-Length alone and the connection closed, so a client still
// writing megabytes into it sees a broken pipe - which http.Client
// reports as the error instead of the response it was given.
func post(t *testing.T, addr net.Addr, path string, size int) int {
	t.Helper()

	conn, err := net.Dial("tcp", addr.String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if err := conn.SetDeadline(time.Now().Add(15 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}

	if _, err := fmt.Fprintf(conn, "POST %s HTTP/1.1\r\nHost: x\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n", path, size); err != nil {
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
