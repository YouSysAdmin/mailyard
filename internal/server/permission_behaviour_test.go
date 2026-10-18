// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/recover"

	"github.com/yousysadmin/mailyard/internal/core/clientip"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	akmodel "github.com/yousysadmin/mailyard/internal/models/apikey"
	"github.com/yousysadmin/mailyard/internal/models/permission"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
)

// permApp mounts one group the way routes.go does, with the caller's
// project and permissions stamped ahead of it.
func permApp(project bool, perms permission.Set, sandboxKey bool) *fiber.App {
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		rc := &domain.RequestContext{Ctx: c, Permissions: perms}
		if project {
			rc.Project = &projmodel.Project{ID: "p1"}
		}

		if sandboxKey {
			rc.APIKey = &akmodel.Key{Sandbox: true}
		}

		c.Locals(domain.ContextKey, rc)

		return c.Next()
	})

	ok := func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) }
	tpl := app.Group("/templates", permOn(permission.ResourceTemplates))
	tpl.Get("/", permRead, ok)
	tpl.Post("/", permWrite, ok)
	tpl.Delete("/", permDelete, ok)

	send := app.Group("/send", permOnSending())
	send.Post("/", permWrite, ok)

	log := app.Group("/log", permOn(permission.ResourceEmails), refuseSandboxCredential)
	log.Get("/", permRead, ok)

	return app
}

// The route-level gate answers from the permission set: no project is a
// 400 naming the header, no permission on the resource is refused at the
// group, and each action is its own permission.
func TestThePermissionGatesAnswerFromTheSet(t *testing.T) {
	read := permission.NewSet(permission.Of(permission.ResourceTemplates, permission.ActionRead))
	write := permission.NewSet(
		permission.Of(permission.ResourceTemplates, permission.ActionRead),
		permission.Of(permission.ResourceTemplates, permission.ActionWrite))
	sandbox := permission.NewSet(permission.Of(permission.ResourceSandbox, permission.ActionWrite))
	emails := permission.NewSet(
		permission.Of(permission.ResourceEmails, permission.ActionRead),
		permission.Of(permission.ResourceEmails, permission.ActionWrite))

	cases := []struct {
		name         string
		project      bool
		perms        permission.Set
		sandboxKey   bool
		method, path string
		status       int
	}{
		{"no project", false, write, false, "GET", "/templates", 400},
		{"nothing on the resource", true, sandbox, false, "GET", "/templates", 403},
		{"read reads", true, read, false, "GET", "/templates", 204},
		{"read does not write", true, read, false, "POST", "/templates", 403},
		{"write writes", true, write, false, "POST", "/templates", 204},
		{"write does not delete", true, write, false, "DELETE", "/templates", 403},

		{"a sandbox key sends on sandbox:write", true, sandbox, true, "POST", "/send", 204},
		{"a sandbox key is not judged on emails", true, emails, true, "POST", "/send", 403},
		{"an ordinary key sends on emails:write", true, emails, false, "POST", "/send", 204},
		{"an ordinary key is not judged on sandbox", true, sandbox, false, "POST", "/send", 403},

		{"a sandbox key never reads the log", true, emails, true, "GET", "/log", 403},
		{"an ordinary key reads the log", true, emails, false, "GET", "/log", 204},
	}
	for _, tc := range cases {
		resp, err := permApp(tc.project, tc.perms, tc.sandboxKey).Test(httptest.NewRequest(tc.method, tc.path, nil))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}

		_ = resp.Body.Close()
		if resp.StatusCode != tc.status {
			t.Errorf("%s: %d, want %d", tc.name, resp.StatusCode, tc.status)
		}
	}
}

// An action on a route whose group named no resource is refused, never
// allowed by default.
func TestAnActionWithoutAResourceIsRefused(t *testing.T) {
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals(domain.ContextKey, &domain.RequestContext{Ctx: c,
			Project: &projmodel.Project{ID: "p1"}, Permissions: permission.NewSet()})

		return c.Next()
	})
	app.Get("/", permRead, func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) })

	resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}

	_ = resp.Body.Close()
	if resp.StatusCode == fiber.StatusNoContent {
		t.Fatal("an action with no declared resource was allowed")
	}
}

// keyStore answers the one lookup machineAuth makes for a key.
type keyStore struct {
	store.APIKeyStore
	key *akmodel.Key
}

func (s keyStore) GetByPrefix(context.Context, string) (*akmodel.Key, error) { return s.key, nil }
func (s keyStore) TouchLastUsed(context.Context, string, time.Time) error    { return nil }

// projectStore answers the project a key names.
type projectStore struct{ store.ProjectStore }

func (projectStore) Get(_ context.Context, id string) (*projmodel.Project, error) {
	return &projmodel.Project{ID: id}, nil
}

// A sandbox key never reaches the credential routes, whatever it
// holds, so it cannot mint a credential that sends real mail. Driven
// through the real router, since the refusal is a property of the
// groups registered there.
func TestASandboxKeyCannotMintACredential(t *testing.T) {
	const raw = "myk_sandboxsandboxsandboxsandboxsandbox"

	resolver, err := clientip.New(nil)
	if err != nil {
		t.Fatal(err)
	}

	call := func(sandbox bool, path string) int {
		key := &akmodel.Key{ID: "k1", ProjectID: "p1", KeyHash: akmodel.Hash(raw), Sandbox: sandbox,
			Permissions: []string{"*"}}
		rt := &env.Runtime{Config: &env.Config{},
			Store: &store.Store{APIKey: keyStore{key: key}, Project: projectStore{}}}
		app := fiber.New()
		app.Use(requestContext(rt, resolver))
		registerRoutes(app, rt, false)

		req := httptest.NewRequest("POST", path, strings.NewReader(`{"sandbox":false}`))
		req.Header.Set("Authorization", "Bearer "+raw)
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}

		_ = resp.Body.Close()

		return resp.StatusCode
	}

	for _, path := range []string{"/api/v1/api-keys", "/api/v1/smtp-credentials"} {
		if got := call(true, path); got != fiber.StatusForbidden {
			t.Errorf("sandbox key POST %s: %d, want 403", path, got)
		}

		if got := call(false, path); got == fiber.StatusForbidden {
			t.Errorf("ordinary key POST %s: 403, want it past the gate", path)
		}
	}
}

// A sandbox key writes only to the sandbox and the two accepting
// routes, and reads what its permissions allow, the POST previews
// included. Every other write on
// /api/v1 is refused however broad the key, and an ordinary key with
// the same permissions is let through.
func TestASandboxKeyWritesOnlyToTheSandbox(t *testing.T) {
	const (
		raw = "myk_sandboxsandboxsandboxsandboxsandbox"
		id  = "0190a0b0-0000-7000-8000-000000000001"
	)

	resolver, err := clientip.New(nil)
	if err != nil {
		t.Fatal(err)
	}

	call := func(sandbox bool, method, path string) (int, string) {
		key := &akmodel.Key{ID: "k1", ProjectID: "p1", KeyHash: akmodel.Hash(raw), Sandbox: sandbox,
			Permissions: []string{"*"}}
		rt := &env.Runtime{Config: &env.Config{},
			Store: &store.Store{APIKey: keyStore{key: key}, Project: projectStore{}}}
		app := fiber.New()
		// A route past the gates reaches a handler over empty stores,
		// which is all this asks.
		app.Use(recover.New())
		app.Use(requestContext(rt, resolver))
		registerRoutes(app, rt, false)

		req := httptest.NewRequest(method, path, strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+raw)
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}

		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		return resp.StatusCode, string(body)
	}

	refused := [][2]string{
		{"POST", "/api/v1/projects/p1/invitations"},
		{"POST", "/api/v1/projects/p1/roles"},
		{"POST", "/api/v1/webhooks"},
		{"POST", "/api/v1/webhooks/" + id + "/test"},
		{"PATCH", "/api/v1/templates/" + id},
		{"POST", "/api/v1/templates/" + id + "/send-test"},
		{"POST", "/api/v1/campaigns/" + id + "/send"},
		{"POST", "/api/v1/smtp-servers/" + id + "/test"},
		{"POST", "/api/v1/domains"},
		{"DELETE", "/api/v1/suppressions"},
		{"POST", "/api/v1/data/delete-contacts"},
		{"POST", "/api/v1/Webhooks/"},
		{"POST", "/api/v1/templates/" + id + "/versions"},
		{"POST", "/api/v1/campaigns/" + id + "/duplicate"},
		{"POST", "/api/v1/subscriber-lists"},
	}
	for _, r := range refused {
		status, body := call(true, r[0], r[1])
		if status != fiber.StatusForbidden || !strings.Contains(body, sandboxOnly) {
			t.Errorf("sandbox key %s %s: %d %s, want 403 naming the sandbox", r[0], r[1], status, body)
		}

		if status, _ := call(false, r[0], r[1]); status == fiber.StatusForbidden {
			t.Errorf("ordinary key %s %s: 403, want it past the gate", r[0], r[1])
		}
	}

	allowed := [][2]string{
		{"POST", "/api/v1/emails/send"},
		{"POST", "/api/v1/emails/send-template"},
		{"POST", "/api/v1/sandbox/inboxes"},
		{"POST", "/api/v1/sandbox/clear"},
		{"GET", "/api/v1/templates"},
		{"POST", "/api/v1/templates/preview"},
		{"POST", "/api/v1/templates/" + id + "/versions/" + id + "/preview"},
		{"POST", "/api/v1/campaigns/" + id + "/preview"},
		{"POST", "/api/v1/subscriber-lists/preview-segment/"},
	}
	for _, r := range allowed {
		if status, body := call(true, r[0], r[1]); status == fiber.StatusForbidden {
			t.Errorf("sandbox key %s %s: 403 %s, want it past the gate", r[0], r[1], body)
		}
	}

	// A read of real mail stays refused.
	if status, _ := call(true, "GET", "/api/v1/emails"); status != fiber.StatusForbidden {
		t.Errorf("sandbox key GET /api/v1/emails: %d, want 403", status)
	}
}
