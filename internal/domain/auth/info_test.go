// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package auth

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	"github.com/yousysadmin/mailyard/internal/models/oauthprovider"
)

type loginableProviders struct{ store.OAuthProviderStore }

func (loginableProviders) ListLoginable(context.Context) ([]*oauthprovider.Provider, error) {
	return []*oauthprovider.Provider{{Name: "Acme", Slug: "acme", Type: "oidc"}}, nil
}

// The login page's first requests arrive together, and the handler they
// share is built once. Run under -race.
func TestConcurrentFirstInfoRequestsShareOneMemo(t *testing.T) {
	h := &Handler{Runtime: &env.Runtime{
		Config: &env.Config{},
		Store:  &store.Store{OAuthProvider: loginableProviders{}},
	}}
	app := fiber.New()
	app.Get("/", h.Info)

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/", nil))
			if err != nil {
				t.Error(err)

				return
			}

			resp.Body.Close()
			if resp.StatusCode != fiber.StatusOK {
				t.Errorf("status %d", resp.StatusCode)
			}
		})
	}

	wg.Wait()
}
