// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
)

// streamPath is the one endpoint whose body never ends.
var streamPath = env.ConsolePath + "/api/events/stream"

// unlistedStreamPath streams exactly like the real one and is NOT in
// streamingPaths, which makes it the control: everything asserted about
// the listed path has to fail here, or the test is measuring nothing.
const unlistedStreamPath = "/unlisted-stream"

// streamRuns is how long the stand-in streams for. Long enough that a
// drained stream is unmistakable, short enough not to park a test run.
const streamRuns = 1200 * time.Millisecond

// edgeApp stands up the two things the fixes below live in - the logger
// skip and the per-request write deadline - over a real listener, because
// neither is visible through app.Test: one is about when headers reach the
// client, the other about a deadline fasthttp sets on the connection.
//
// withReader is what separates the two tests, and it is not a convenience:
// a middleware that drains the stream buffers the whole body and writes it
// in one go, INSIDE any deadline, so a write-timeout test standing behind
// one measures nothing.
func edgeApp(t *testing.T, cfg fiber.Config, withReader bool) net.Addr {
	t.Helper()

	app := fiber.New(cfg)
	app.Server().HeaderReceived = perRequestLimits

	if withReader {
		// Stands in for slog-fiber at the one point that matters: it
		// reads the response body after the handler returned, and
		// reading a streamed body runs the stream.
		reader := func(c fiber.Ctx) error {
			if err := c.Next(); err != nil {
				return err
			}

			_ = len(c.Response().Body())

			return nil
		}
		app.Use(skipPaths(reader, streamingPaths...))
	}

	stream := func(c fiber.Ctx) error {
		return c.SendStreamWriter(func(w *bufio.Writer) {
			deadline := time.Now().Add(streamRuns)
			for i := 0; time.Now().Before(deadline); i++ {
				if _, err := fmt.Fprintf(w, ": ping %d\n\n", i); err != nil {
					return
				}

				if err := w.Flush(); err != nil {
					return
				}

				time.Sleep(50 * time.Millisecond)
			}
		})
	}
	app.Get(streamPath, stream)
	app.Get(unlistedStreamPath, stream)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	go func() { _ = app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true}) }()
	t.Cleanup(func() { _ = app.Shutdown() })

	return ln.Addr()
}

// get issues one request and reports the status line, how long the first
// byte took, and the whole body.
//
// Connection: close is what makes the read terminate: a stream that now
// survives its write deadline leaves the connection open for reuse, so
// reading to EOF without this waits for the idle timeout.
func get(t *testing.T, addr net.Addr, path string) (status int, ttfb time.Duration, body string) {
	t.Helper()

	conn, err := net.Dial("tcp", addr.String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if err := conn.SetDeadline(time.Now().Add(15 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}

	if _, err := fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n", path); err != nil {
		t.Fatalf("write request: %v", err)
	}

	start := time.Now()
	br := bufio.NewReader(conn)
	line, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("read status line: %v", err)
	}
	ttfb = time.Since(start)

	if _, err := fmt.Sscanf(line, "HTTP/1.1 %d", &status); err != nil {
		t.Fatalf("unreadable status line %q: %v", line, err)
	}

	rest, err := io.ReadAll(br)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	return status, ttfb, string(rest)
}

// TestTheLoggerSkipsEveryFormOfAStreamingPath asks the ROUTER which forms
// of the path reach the streaming handler, and requires the skip to agree
// with it.
//
// The access logger records the response size, which it reads off the
// response body - and reading a streamed body runs the stream. On an
// endless one the request never answers at all: no headers, a held
// connection, and a goroutine parked until the stream recycles itself
// half an hour later.
//
// A raw string compare against c.Path() did not agree with the router.
// Neither CaseSensitive nor StrictRouting is set, so /app/api/events/STREAM
// and /app/api/events/stream/ both arrive at the handler while matching
// neither the list nor each other. Which forms the router accepts is not
// asserted here, it is READ OFF THE STATUS: a 200 means the streaming
// handler ran and the skip had to cover it, a 404 means there was nothing
// to cover. A change in Fiber's matching therefore shows up here rather
// than as a hung request in production.
func TestTheLoggerSkipsEveryFormOfAStreamingPath(t *testing.T) {
	addr := edgeApp(t, fiber.Config{}, true)

	// The control first, so a test that measures nothing cannot pass. The
	// same stream at a path the list does not name must be drained by the
	// reader, which is what holds the headers back.
	if status, ttfb, _ := get(t, addr, unlistedStreamPath); status != fiber.StatusOK {
		t.Fatalf("the control answered %d, want 200", status)
	} else if ttfb < streamRuns/2 {
		t.Fatalf("the control answered its first byte in %v, so nothing drained the stream - "+
			"this test can no longer tell a skipped logger from a running one", ttfb)
	}

	for _, path := range []string{
		streamPath,
		streamPath + "/",
		streamPath + "//",
		strings.ToUpper(streamPath),
		strings.ToUpper(streamPath) + "/",
		streamPath + "/../stream",
		strings.Replace(streamPath, "/events/", "/events//", 1),
		streamPath + "x",
		env.ConsolePath + "/api/events",
	} {
		t.Run(path, func(t *testing.T) {
			status, ttfb, _ := get(t, addr, path)
			if status != fiber.StatusOK {
				// The router did not take this form to the streaming
				// handler, so there is nothing to skip.
				return
			}

			if ttfb > streamRuns/2 {
				t.Errorf("%s reached the streaming handler and its first byte took %v - "+
					"skipPaths did not recognise this form of the path, so the logger read the stream",
					path, ttfb)
			}
		})
	}
}

// TestAStreamOutlivesTheWriteTimeout pins the reason perRequestLimits
// exists.
//
// fasthttp sets the write deadline ONCE, after the handler returns and
// before it writes the response, and the whole streamed body is written
// under that one deadline. So the app-wide WriteTimeout is not a budget
// per write for a stream, it is the stream's entire lifetime: at two
// minutes the console's event feed was cut and reconnected every two
// minutes, whatever eventstream.MaxStreamLife said.
//
// The hook raises it for the streaming paths only, to a value derived
// from that stream's own life - so the stream is what ends, not the
// deadline. The control is the same stream at an unlisted path, which
// must still be cut: without it this would pass just as well if the hook
// raised the deadline for everything and removed the backstop.
func TestAStreamOutlivesTheWriteTimeout(t *testing.T) {
	// Short enough to keep the test quick, and well inside streamRuns so
	// a cut is unambiguous.
	const writeTimeout = 400 * time.Millisecond

	addr := edgeApp(t, fiber.Config{WriteTimeout: writeTimeout}, false)

	full := strings.Count(streamBody(t, addr, streamPath), ": ping")
	cut := strings.Count(streamBody(t, addr, unlistedStreamPath), ": ping")

	// The listed path streams for its whole life. 20 ticks at 50ms is
	// what streamRuns allows, and the count is not asserted exactly
	// because the sleep is a floor, not a clock.
	if full < 15 {
		t.Errorf("the streaming path delivered %d pings, want the whole stream - "+
			"the app-wide write timeout cut it", full)
	}

	if cut >= full {
		t.Errorf("an unlisted path delivered %d pings against the streaming path's %d, "+
			"so the write timeout is no longer applied to ordinary responses", cut, full)
	}
}

// streamBody reads one streamed response to its end.
func streamBody(t *testing.T, addr net.Addr, path string) string {
	t.Helper()

	status, _, body := get(t, addr, path)
	if status != fiber.StatusOK {
		t.Fatalf("%s answered %d, want 200", path, status)
	}

	return body
}

// TestAnErrorReachingTheTopIsTheEnvelope covers what Fiber's stock error
// handler did instead: text/plain carrying err.Error().
//
// Three SDKs are generated from a document that promises {"error": ...},
// and a store failure propagated to the top of a handler answered with
// the Postgres message in the body - which is both a leak and a parse
// failure at the client.
func TestAnErrorReachingTheTopIsTheEnvelope(t *testing.T) {
	app := fiber.New(fiber.Config{ErrorHandler: errorHandler})
	app.Get("/store-error", func(fiber.Ctx) error {
		return errors.New(`ERROR: relation "emails_d2026_08_19" does not exist (SQLSTATE 42P01)`)
	})
	app.Get("/fiber-error", func(fiber.Ctx) error {
		return fiber.ErrConflict
	})

	for _, tc := range []struct {
		path   string
		status int
		body   string
	}{
		// The raw error is logged, never sent.
		{"/store-error", fiber.StatusInternalServerError, `{"error":"internal server error"}`},

		// A status Fiber chose, repeated in our envelope.
		{"/fiber-error", fiber.StatusConflict, `{"error":"conflict"}`},

		// Fiber's own 404, which used to read "Cannot GET /nope".
		{"/nope", fiber.StatusNotFound, `{"error":"not found"}`},
	} {
		res, err := app.Test(httptest.NewRequest(fiber.MethodGet, tc.path, nil))
		if err != nil {
			t.Fatalf("%s: %v", tc.path, err)
		}

		body, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()

		if res.StatusCode != tc.status {
			t.Errorf("%s answered %d, want %d", tc.path, res.StatusCode, tc.status)
		}

		if strings.TrimSpace(string(body)) != tc.body {
			t.Errorf("%s answered %q, want %q", tc.path, body, tc.body)
		}

		if ct := res.Header.Get(fiber.HeaderContentType); !strings.HasPrefix(ct, fiber.MIMEApplicationJSON) {
			t.Errorf("%s answered content type %q, want json", tc.path, ct)
		}
	}
}
