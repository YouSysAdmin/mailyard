// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package inbound

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/domain"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
)

// A filter value outside the set is refused rather than answered with
// an empty list, which would read as nothing to show.
func TestAnUnknownFilterValueIsRefused(t *testing.T) {
	h := &Handler{Runtime: &env.Runtime{}}
	app := fiber.New()
	app.Get("/x", func(c fiber.Ctx) error {
		c.Locals(domain.ContextKey, &domain.RequestContext{Project: &projmodel.Project{ID: "p"}})

		return h.List(c)
	})

	res, err := app.Test(httptest.NewRequest("GET", "/x?status=bogus", nil))
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()
	if res.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status %d, want 400", res.StatusCode)
	}
}
