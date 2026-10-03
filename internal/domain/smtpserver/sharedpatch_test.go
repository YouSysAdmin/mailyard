// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpserver

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
	"github.com/yousysadmin/mailyard/internal/domain/relaynode"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

func sharedPatchApp(t *testing.T) (*SharedStore, *relaynode.Store, func(id, body string) int) {
	t.Helper()
	shared, nodes := testStores(t)
	h := &SharedHandler{Runtime: &env.Runtime{
		Config: &env.Config{},
		Store:  &store.Store{SharedSMTP: shared, RelayNode: nodes},
	}}
	app := fiber.New()
	app.Patch("/:id", h.Update)

	patch := func(id, body string) int {
		req := httptest.NewRequest(fiber.MethodPatch, "/"+id, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res, err := app.Test(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}

		defer func() { _ = res.Body.Close() }()

		return res.StatusCode
	}

	return shared, nodes, patch
}

// A suspended relay node's row cannot be re-enabled, or pointed
// elsewhere, through the server PATCH. Suspension and the node's own
// address are decided on the relay nodes page.
func TestARelayNodeRowIsNotRedirectedOrReenabledByAPatch(t *testing.T) {
	shared, nodes, patch := sharedPatchApp(t)
	srv := enrolledRow(t, shared, nodes, "listen")
	if err := shared.SetStatus(t.Context(), srv.ID, ssmodel.StatusDisabled, "suspended by an administrator", nil); err != nil {
		t.Fatal(err)
	}

	for _, body := range []string{`{"status": "enabled"}`, `{"host": "evil.example.com"}`, `{"port": 2525}`} {
		if code := patch(srv.ID, body); code != fiber.StatusBadRequest {
			t.Errorf("%s on a node row answered %d, want 400", body, code)
		}
	}

	got, err := shared.Get(t.Context(), srv.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got.Status != ssmodel.StatusDisabled || got.Host != "node.example.com" || got.Port != 587 {
		t.Errorf("node row changed: status %s host %s port %d", got.Status, got.Host, got.Port)
	}

	if code := patch(srv.ID, `{"name": "renamed", "priority": 5}`); code != fiber.StatusOK {
		t.Errorf("renaming a node row answered %d, want 200", code)
	}
}

// A shared SES row cannot lose its required region through a PATCH,
// the same rule the project path enforces.
func TestASharedPatchIsCheckedAgainstItsProvider(t *testing.T) {
	shared, _, patch := sharedPatchApp(t)
	srv := &ssmodel.Shared{
		ID: ids.New(), Name: "ses", Host: "email.eu-west-1.amazonaws.com", Port: 443,
		Encryption: smtpclient.EncryptionSSL, Status: ssmodel.StatusEnabled,
		AllowedEmails: []string{}, AllowedDomains: []string{}, SecurityMode: ssmodel.SecurityPermissive,
	}
	srv.Provider = "ses"
	srv.ProviderConfig = map[string]string{"region": "eu-west-1"}
	if err := shared.Put(t.Context(), srv); err != nil {
		t.Fatal(err)
	}

	if code := patch(srv.ID, `{"provider_config": {}}`); code != fiber.StatusBadRequest {
		t.Errorf("removing the SES region answered %d, want 400", code)
	}
}
