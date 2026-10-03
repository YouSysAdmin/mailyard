// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package oauthprovider

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	coreoidc "github.com/yousysadmin/mailyard/internal/core/oidc"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	opmodel "github.com/yousysadmin/mailyard/internal/models/oauthprovider"
)

type memProviders struct {
	store.OAuthProviderStore
	rows map[string]*opmodel.Provider
}

func (m *memProviders) Get(_ context.Context, id string) (*opmodel.Provider, error) {
	if p, ok := m.rows[id]; ok {
		cp := *p

		return &cp, nil
	}

	return nil, nil
}

func (m *memProviders) Put(_ context.Context, p *opmodel.Provider) error {
	cp := *p
	m.rows[p.ID] = &cp

	return nil
}

func (m *memProviders) SlugTaken(context.Context, string, string) (bool, error) { return false, nil }

// A PATCH naming one field leaves every other one as stored: the text
// fields are not cleared, the slug is not re-derived from a new name,
// and the type is not reset.
func TestAPatchKeepsWhatItDoesNotName(t *testing.T) {
	const id = "01a101aa-0000-7000-8000-000000000001"
	mem := &memProviders{rows: map[string]*opmodel.Provider{id: {
		ID: id, Name: "Okta", Slug: "okta-prod", Type: opmodel.TypeOIDC,
		ClientID: "client-1", Issuer: "https://okta.example.com", GroupsClaim: "groups",
		Enabled: true, AutoRegister: true, RequireEmailVerified: true,
	}}}

	h := &Handler{Runtime: &env.Runtime{
		Config: &env.Config{},
		Store:  &store.Store{OAuthProvider: mem},
		OAuth:  coreoidc.NewRegistry("https://mail.example.com", nil),
	}}
	app := fiber.New()
	app.Patch("/oauth-providers/:id", h.Update)

	patch := func(body string) int {
		req := httptest.NewRequest("PATCH", "/oauth-providers/"+id, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}

		return res.StatusCode
	}

	if code := patch(`{"enabled": false}`); code != fiber.StatusOK {
		t.Fatalf("a body without name answered %d", code)
	}

	if code := patch(`{"name": "Okta Production"}`); code != fiber.StatusOK {
		t.Fatalf("rename answered %d", code)
	}

	got := mem.rows[id]
	if got.Name != "Okta Production" || got.Slug != "okta-prod" || got.Type != opmodel.TypeOIDC ||
		got.ClientID != "client-1" || got.GroupsClaim != "groups" || got.Issuer != "https://okta.example.com" ||
		got.Enabled || !got.AutoRegister {
		t.Errorf("after two partial patches: %+v", got)
	}

	if code := patch(`{"groups_claim": ""}`); code != fiber.StatusOK || mem.rows[id].GroupsClaim != "" {
		t.Errorf("an explicit empty value must clear: %d %+v", code, mem.rows[id])
	}
}
