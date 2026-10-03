// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package sesfeedback

import (
	"bytes"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
)

// The endpoint is public, so a body that is not an SNS delivery is
// refused without a log line above debug, like an unlisted topic.
func TestAnUnparseableBodyLogsNothingAboveDebug(t *testing.T) {
	var buf bytes.Buffer
	h := &Handler{Runtime: &env.Runtime{Log: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))}}

	app := fiber.New()
	app.Post("/webhooks/ses", h.Receive)

	resp, err := app.Test(httptest.NewRequest("POST", "/webhooks/ses", strings.NewReader("not json")))
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}

	if buf.Len() != 0 {
		t.Errorf("logged above debug: %s", buf.String())
	}
}
