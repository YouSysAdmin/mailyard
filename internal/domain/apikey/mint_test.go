// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package apikey

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/domain"
	perm "github.com/yousysadmin/mailyard/internal/models/permission"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
)

// A key may carry the whole catalogue, so the cap follows its size.
func TestTheWholeCatalogueFitsOnAKey(t *testing.T) {
	var all []string
	for _, d := range perm.Registry {
		for _, a := range d.Actions {
			all = append(all, string(perm.Of(d.Resource, a)))
		}
	}

	if len(all) != perm.Size() {
		t.Fatalf("Size() = %d, catalogue enumerates %d", perm.Size(), len(all))
	}

	out, bad := normalizePermissions(append(all, perm.All))
	if bad != "" {
		t.Fatalf("the whole catalogue plus the wildcard was refused: %s", bad)
	}

	if len(out) != len(all)+1 {
		t.Fatalf("normalized %d of %d", len(out), len(all)+1)
	}
}

// The sandbox flag adds sandbox grants, and those count as minted.
func TestASandboxKeyNeedsTheSandboxPermissions(t *testing.T) {
	held := perm.NewSet(
		perm.Of(perm.ResourceAPIKeys, perm.ActionWrite),
		perm.Of(perm.ResourceEmails, perm.ActionWrite),
	)

	app := fiber.New()
	app.Post("/", func(c fiber.Ctx) error {
		c.Locals(domain.ContextKey, &domain.RequestContext{
			Project:     &projmodel.Project{ID: "p"},
			Permissions: held,
		})

		return (&Handler{Runtime: &env.Runtime{}}).Create(c)
	})

	body := `{"name":"ci","permissions":["emails:write"],"sandbox":true}`
	req := httptest.NewRequest(fiber.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = res.Body.Close() }()

	got, _ := io.ReadAll(res.Body)
	if res.StatusCode != fiber.StatusForbidden || !strings.Contains(string(got), "sandbox:read") {
		t.Fatalf("status %d body %s, want 403 naming sandbox:read", res.StatusCode, got)
	}
}
