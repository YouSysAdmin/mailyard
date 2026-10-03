// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/domain"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
)

// A project header that is not a uuid names nothing, and the request
// continues with no project like any unreachable one. The runtime has
// no store, so reaching the database would panic.
func TestAMalformedProjectHeaderIsNotARefusal(t *testing.T) {
	rt := &env.Runtime{Config: &env.Config{}}

	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		rc := &domain.RequestContext{User: &usermodel.User{ID: "u1"}}
		c.Locals(domain.ContextKey, rc)
		if ok, resp := stampProject(c, rt); !ok {
			return resp
		}

		if rc.Project != nil {
			t.Error("a malformed header resolved a project")
		}

		return c.SendStatus(fiber.StatusNoContent)
	})

	req := httptest.NewRequest(fiber.MethodGet, "/", nil)
	req.Header.Set(ProjectHeader, "banana")
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	if res.StatusCode != fiber.StatusNoContent {
		t.Fatalf("status %d, want the request to continue", res.StatusCode)
	}
}
