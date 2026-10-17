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
	"github.com/yousysadmin/mailyard/internal/core/settings"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	akmodel "github.com/yousysadmin/mailyard/internal/models/apikey"
	smodel "github.com/yousysadmin/mailyard/internal/models/setting"
)

// adminKeyStore answers the one lookup machineAuth makes for a
// platform credential.
type adminKeyStore struct {
	store.AdminAPIKeyStore
	key *akmodel.Admin
}

func (s adminKeyStore) GetByPrefix(context.Context, string) (*akmodel.Admin, error) {
	return s.key, nil
}

func (s adminKeyStore) TouchLastUsed(context.Context, string, time.Time) error { return nil }

// storedSettings is a settings loader holding fixed rows.
type storedSettings []*smodel.Setting

func (s storedSettings) All(context.Context) ([]*smodel.Setting, error) { return s, nil }

// The delivery path of the platform mail project is written only by a
// platform administrator. A full project key of that project is
// refused, an admin key gets through, and a key of another project is
// not touched by the rule. Driven through the real router, since the
// refusal is a property of the routes registered there.
func TestOnlyAnAdminChangesThePlatformMailDelivery(t *testing.T) {
	const (
		projectRaw = "myk_projectprojectprojectprojectproject"
		adminRaw   = "mya_adminadminadminadminadminadminadmin"
		platform   = "0190a0b0-0000-7000-8000-0000000000aa"
		other      = "0190a0b0-0000-7000-8000-0000000000bb"
		id         = "0190a0b0-0000-7000-8000-000000000001"
	)

	resolver, err := clientip.New(nil)
	if err != nil {
		t.Fatal(err)
	}

	set := settings.New(storedSettings{{Key: smodel.KeyPlatformMailProject, Value: platform}})
	if err := set.Reload(t.Context()); err != nil {
		t.Fatal(err)
	}

	call := func(raw, project, method, path string) (int, string) {
		key := &akmodel.Key{ID: "k1", ProjectID: project, KeyHash: akmodel.Hash(projectRaw),
			Permissions: []string{"*"}}
		admin := &akmodel.Admin{ID: "a1", KeyHash: akmodel.Hash(adminRaw)}
		rt := &env.Runtime{Config: &env.Config{}, Settings: set,
			Store: &store.Store{APIKey: keyStore{key: key}, AdminAPIKey: adminKeyStore{key: admin},
				Project: projectStore{}}}
		app := fiber.New()
		// A route past the gates reaches a handler over empty stores,
		// which is all this asks.
		app.Use(recover.New())
		app.Use(requestContext(rt, resolver))
		registerRoutes(app, rt, false)

		req := httptest.NewRequest(method, path, strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+raw)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(ProjectHeader, project)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}

		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		return resp.StatusCode, string(body)
	}

	routes := [][2]string{
		{"PATCH", "/api/v1/smtp-servers/" + id},
		{"POST", "/api/v1/my/relay-nodes/" + id + "/approve"},
	}
	for _, r := range routes {
		status, body := call(projectRaw, platform, r[0], r[1])
		if status != fiber.StatusForbidden || !strings.Contains(body, domain.PlatformMailManaged) {
			t.Errorf("project key %s %s in the platform mail project: %d %s, want 403 naming why",
				r[0], r[1], status, body)
		}

		if status, body := call(adminRaw, platform, r[0], r[1]); strings.Contains(body, domain.PlatformMailManaged) {
			t.Errorf("admin key %s %s: %d %s, want it past the guard", r[0], r[1], status, body)
		}

		if status, body := call(projectRaw, other, r[0], r[1]); status == fiber.StatusForbidden {
			t.Errorf("project key %s %s in another project: %d %s, want it past the gate", r[0], r[1], status, body)
		}
	}

	// Reading the configuration stays open to the project's members.
	if status, body := call(projectRaw, platform, "GET", "/api/v1/smtp-servers/"); strings.Contains(body, domain.PlatformMailManaged) {
		t.Errorf("project key GET /smtp-servers in the platform mail project: %d %s, want it past the guard", status, body)
	}
}
