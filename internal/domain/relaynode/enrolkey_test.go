// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relaynode

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/settings"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	akmodel "github.com/yousysadmin/mailyard/internal/models/apikey"
	settingmodel "github.com/yousysadmin/mailyard/internal/models/setting"
)

type oneKey struct {
	store.APIKeyStore
	key *akmodel.Key
}

func (s oneKey) GetByPrefix(context.Context, string) (*akmodel.Key, error) { return s.key, nil }

// A sandbox key enrols no node, whatever it holds, because a node
// carries the project's real mail.
func TestASandboxKeyEnrolsNoNode(t *testing.T) {
	const raw = "myk_sandboxsandboxsandboxsandboxsandbox"

	enrols := func(sandbox bool) bool {
		key := &akmodel.Key{ID: "k1", ProjectID: "p1", KeyHash: akmodel.Hash(raw), Sandbox: sandbox,
			Permissions: []string{"*"}}
		h := &Handler{Runtime: &env.Runtime{Config: &env.Config{},
			Store: &store.Store{APIKey: oneKey{key: key}}}}

		var ok bool
		app := fiber.New()
		app.Post("/", func(c fiber.Ctx) error {
			_, ok = h.projectForKey(c, raw)

			return c.SendStatus(fiber.StatusNoContent)
		})

		resp, err := app.Test(httptest.NewRequest("POST", "/", nil))
		if err != nil {
			t.Fatal(err)
		}

		_ = resp.Body.Close()

		return ok
	}

	if enrols(true) {
		t.Error("a sandbox key enrolled a relay node")
	}

	if !enrols(false) {
		t.Error("an ordinary key holding relay:write was refused")
	}
}

// fixedSettings is a settings loader holding fixed rows.
type fixedSettings []*settingmodel.Setting

func (s fixedSettings) All(context.Context) ([]*settingmodel.Setting, error) { return s, nil }

// A project key enrols no node into the platform mail project, since a
// project key is never a platform administrator. A key of any other
// project still enrols.
func TestAProjectKeyEnrolsNoNodeIntoThePlatformMailProject(t *testing.T) {
	const raw = "myk_projectprojectprojectprojectproject"

	set := settings.New(fixedSettings{{Key: settingmodel.KeyPlatformMailProject, Value: "platform"}})
	if err := set.Reload(t.Context()); err != nil {
		t.Fatal(err)
	}

	enrol := func(project string) (string, int) {
		key := &akmodel.Key{ID: "k1", ProjectID: project, KeyHash: akmodel.Hash(raw), Permissions: []string{"*"}}
		h := &Handler{Runtime: &env.Runtime{Config: &env.Config{}, Settings: set,
			Store: &store.Store{APIKey: oneKey{key: key}}}}

		var scope string
		app := fiber.New()
		app.Post("/", func(c fiber.Ctx) error {
			projID, resp, ok := h.enrolmentScope(c, raw)
			if !ok {
				return resp
			}

			scope = projID

			return c.SendStatus(fiber.StatusNoContent)
		})

		resp, err := app.Test(httptest.NewRequest("POST", "/", nil))
		if err != nil {
			t.Fatal(err)
		}

		_ = resp.Body.Close()

		return scope, resp.StatusCode
	}

	if scope, status := enrol("platform"); status != fiber.StatusForbidden || scope != "" {
		t.Errorf("a key of the platform mail project: %d into %q, want 403", status, scope)
	}

	if scope, status := enrol("other"); status != fiber.StatusNoContent || scope != "other" {
		t.Errorf("a key of another project: %d into %q, want it enrolled there", status, scope)
	}
}
