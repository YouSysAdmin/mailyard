// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	coreaudit "github.com/yousysadmin/mailyard/internal/core/audit"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/domain"
	amodel "github.com/yousysadmin/mailyard/internal/models/audit"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
)

type discardSink struct{}

func (discardSink) Put(context.Context, *amodel.Event) error { return nil }

// The router ignores case, so the audit type must too: a mutation sent
// as /API/v1/... reaches the same handler and has to be filed, and
// alerted on, exactly as the lowercase spelling is.
func TestEverySpellingOfARouteGetsOneAuditType(t *testing.T) {
	rec := coreaudit.New(discardSink{}, slog.New(slog.DiscardHandler))
	defer rec.Close(time.Second)

	types := make(chan string, 16)
	rec.Watch(func(e *amodel.Event) { types <- e.Type })

	rt := &env.Runtime{Audit: rec}
	app := fiber.New()
	v1 := app.Group("/api/v1", func(c fiber.Ctx) error {
		c.Locals(domain.ContextKey, &domain.RequestContext{Project: &projmodel.Project{ID: "p"}})

		return c.Next()
	}, auditWrites(rt))
	v1.Post("/api-keys", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusCreated) })
	v1.Patch("/projects/:id/members/:uid", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	for _, tc := range []struct{ method, path, want string }{
		{fiber.MethodPost, "/api/v1/api-keys", "apikey.created"},
		{fiber.MethodPost, "/API/v1/api-keys", "apikey.created"},
		{fiber.MethodPost, "/api/V1/api-keys/", "apikey.created"},
		{fiber.MethodPatch, "/api/V1/projects/0190a0b0-0000-7000-8000-000000000001/members/0190a0b0-0000-7000-8000-000000000002", "project.member.updated"},
	} {
		res, err := app.Test(httptest.NewRequest(tc.method, tc.path, nil))
		if err != nil {
			t.Fatalf("%s %s: %v", tc.method, tc.path, err)
		}

		_ = res.Body.Close()

		select {
		case got := <-types:
			if got != tc.want {
				t.Errorf("%s %s: type %q, want %q", tc.method, tc.path, got, tc.want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s %s: no audit event", tc.method, tc.path)
		}
	}
}
