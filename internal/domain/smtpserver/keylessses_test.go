// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/settings"
	"github.com/yousysadmin/mailyard/internal/core/transport"
	setmodel "github.com/yousysadmin/mailyard/internal/models/setting"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

type platformLoader struct{ project string }

func (l platformLoader) All(context.Context) ([]*setmodel.Setting, error) {
	return []*setmodel.Setting{{Key: setmodel.KeyPlatformMailProject, Value: l.project}}, nil
}

// A project's SES row must carry its own key pair. Only the project
// platform mail is sent through may leave it empty and sign with the
// machine's credentials, as the shared pool does.
func TestAKeylessSESRowIsRefusedOutsideThePlatformProject(t *testing.T) {
	st := settings.New(platformLoader{project: "proj-platform"})
	if err := st.Reload(t.Context()); err != nil {
		t.Fatal(err)
	}

	rt := &env.Runtime{Config: &env.Config{}, Settings: st}
	check := func(srv *ssmodel.Server) int {
		app := fiber.New()
		app.Post("/", func(c fiber.Ctx) error {
			if ok, resp := refuseKeylessSES(c, rt, srv); !ok {
				return resp
			}

			return response.NoContent(c)
		})
		res, err := app.Test(httptest.NewRequest(fiber.MethodPost, "/", nil))
		if err != nil {
			t.Fatal(err)
		}

		_ = res.Body.Close()

		return res.StatusCode
	}

	ses := func(proj, user, pass string) *ssmodel.Server {
		return &ssmodel.Server{ProjectID: proj, Provider: transport.ProviderSES, Username: user, Password: pass}
	}

	for name, tc := range map[string]struct {
		srv  *ssmodel.Server
		want int
	}{
		"tenant without a key":     {ses("proj-a", "", ""), http.StatusBadRequest},
		"tenant without a secret":  {ses("proj-a", "AKIA", ""), http.StatusBadRequest},
		"tenant with a key pair":   {ses("proj-a", "AKIA", "secret"), http.StatusNoContent},
		"platform project keyless": {ses("proj-platform", "", ""), http.StatusNoContent},
		"smtp row without a login": {&ssmodel.Server{ProjectID: "proj-a"}, http.StatusNoContent},
	} {
		if got := check(tc.srv); got != tc.want {
			t.Errorf("%s: %d, want %d", name, got, tc.want)
		}
	}
}
