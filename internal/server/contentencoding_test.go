// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
)

// gzipped compresses b once.
func gzipped(t *testing.T, b []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(b); err != nil {
		t.Fatal(err)
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

// encodingApp mounts handlers that read the body the two ways the
// product does, c.Body() and a multipart form value, and records how
// much of it they saw.
func encodingApp(refuse bool, seen *int) *fiber.App {
	cfg := &env.Config{}
	cfg.Sending.MaxTotalAttachmentSize = 25 << 20
	app := fiber.New(fiber.Config{BodyLimit: bodyLimitFor(cfg)})
	if refuse {
		app.Use(refuseContentEncoding)
	}

	app.Post("/webhooks/ses", func(c fiber.Ctx) error {
		*seen = len(c.Body())

		return c.SendStatus(fiber.StatusOK)
	})
	app.Post("/tracking/unsubscribe/x", func(c fiber.Ctx) error {
		*seen = len(c.FormValue("scope"))

		return c.SendStatus(fiber.StatusOK)
	})

	return app
}

func postEncoded(t *testing.T, app *fiber.App, path, contentType, encoding string, body []byte) int {
	t.Helper()

	req := httptest.NewRequest(fiber.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	if encoding != "" {
		req.Header.Set("Content-Encoding", encoding)
	}

	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}

	_, _ = io.Copy(io.Discard, res.Body)
	_ = res.Body.Close()

	return res.StatusCode
}

// A compressed body is refused before any handler reads it, because the
// framework would inflate it far past the ceiling the bytes on the wire
// were held to. The control shows it would: without the refusal a few
// kilobytes reach the handler as megabytes.
func TestACompressedRequestBodyIsRefused(t *testing.T) {
	big := []byte(`{"x":"` + strings.Repeat("a", 4<<20) + `"}`)
	twice := gzipped(t, gzipped(t, big))

	form := "--b\r\nContent-Disposition: form-data; name=\"scope\"\r\n\r\n" +
		strings.Repeat("a", 4<<20) + "\r\n--b--\r\n"
	formGz := gzipped(t, []byte(form))

	var seen int
	if got := postEncoded(t, encodingApp(false, &seen), "/webhooks/ses", "application/json", "gzip, gzip", twice); got != fiber.StatusOK || seen < 4<<20 {
		t.Fatalf("control: %d with %d bytes seen from %d on the wire, want the body inflated", got, seen, len(twice))
	}

	app := encodingApp(true, &seen)
	for _, tc := range []struct {
		path, ctype, enc string
		body             []byte
	}{
		{"/webhooks/ses", "application/json", "gzip, gzip", twice},
		{"/webhooks/ses", "application/json", "GZIP", gzipped(t, big)},
		{"/webhooks/ses", "application/json", "identity, br", twice},
		{"/tracking/unsubscribe/x", "multipart/form-data; boundary=b", "gzip", formGz},
	} {
		seen = -1
		if got := postEncoded(t, app, tc.path, tc.ctype, tc.enc, tc.body); got != fiber.StatusUnsupportedMediaType {
			t.Errorf("%s with %q: %d, want 415", tc.path, tc.enc, got)
		}

		if seen != -1 {
			t.Errorf("%s with %q: the handler read %d bytes", tc.path, tc.enc, seen)
		}
	}
}

// An uncompressed body passes, with or without an explicit identity.
func TestAnUncompressedRequestBodyPasses(t *testing.T) {
	var seen int
	app := encodingApp(true, &seen)

	for _, enc := range []string{"", "identity", " Identity "} {
		if got := postEncoded(t, app, "/webhooks/ses", "application/json", enc, []byte(`{"ok":true}`)); got != fiber.StatusOK || seen != 11 {
			t.Errorf("Content-Encoding %q: %d with %d bytes seen, want 200 and 11", enc, got, seen)
		}
	}
}
