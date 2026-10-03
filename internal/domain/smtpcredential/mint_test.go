// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpcredential

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

// The host a client is told to dial is never the bare default greeting
// name.
func TestTheListenerHostIsDialable(t *testing.T) {
	cases := []struct {
		addr, hostname, public, want string
	}{
		{":587", "mailyard", "https://mail.example.com", "mail.example.com"},
		{":587", "mailyard", "", "localhost"},
		{"10.0.0.5:587", "mailyard", "https://mail.example.com", "10.0.0.5"},
		{":587", "smtp.example.com", "https://mail.example.com", "smtp.example.com"},
	}
	for _, tc := range cases {
		cfg := &env.Config{}
		cfg.Submission.Addr = tc.addr
		cfg.Submission.Hostname = tc.hostname
		cfg.Server.PublicURL = tc.public

		got := (&Handler{Runtime: &env.Runtime{Config: cfg}}).listenerInfo().Host
		if got != tc.want {
			t.Errorf("%+v: host %q, want %q", tc, got, tc.want)
		}
	}
}

// A sandbox credential writes into the sandbox, so minting one needs
// sandbox:write.
func TestASandboxCredentialNeedsSandboxWrite(t *testing.T) {
	app := fiber.New()
	app.Post("/", func(c fiber.Ctx) error {
		c.Locals(domain.ContextKey, &domain.RequestContext{
			Project:     &projmodel.Project{ID: "p"},
			Permissions: perm.NewSet(perm.Of(perm.ResourceAPIKeys, perm.ActionWrite)),
		})

		return (&Handler{Runtime: &env.Runtime{}}).Create(c)
	})

	req := httptest.NewRequest(fiber.MethodPost, "/", strings.NewReader(`{"name":"ci","sandbox":true}`))
	req.Header.Set("Content-Type", "application/json")
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = res.Body.Close() }()

	got, _ := io.ReadAll(res.Body)
	if res.StatusCode != fiber.StatusForbidden || !strings.Contains(string(got), "sandbox:write") {
		t.Fatalf("status %d body %s, want 403 naming sandbox:write", res.StatusCode, got)
	}
}
