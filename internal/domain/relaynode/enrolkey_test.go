// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relaynode

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	akmodel "github.com/yousysadmin/mailyard/internal/models/apikey"
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
