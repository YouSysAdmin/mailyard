// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/clientip"
	"github.com/yousysadmin/mailyard/internal/core/env"
)

// A handler's context carries a deadline, so a query cannot outlive the
// request by more than requestDeadline, and it is done once the handler
// has returned.
func TestAHandlerContextHasADeadline(t *testing.T) {
	resolver, err := clientip.New(nil)
	if err != nil {
		t.Fatal(err)
	}

	var left time.Duration
	var after func() error
	app := fiber.New()
	app.Use(requestContext(&env.Runtime{Config: &env.Config{}}, resolver))
	app.Get("/", func(c fiber.Ctx) error {
		deadline, ok := c.Context().Deadline()
		if ok {
			left = time.Until(deadline)
		}

		ctx := c.Context()
		after = ctx.Err

		return c.SendStatus(fiber.StatusNoContent)
	})

	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/", nil))
	if err != nil {
		t.Fatal(err)
	}

	_ = resp.Body.Close()
	if left <= 0 || left > requestDeadline {
		t.Fatalf("deadline %v away, expected within %v", left, requestDeadline)
	}

	if after() == nil {
		t.Fatal("the handler context is still live after the response")
	}
}
