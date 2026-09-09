// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpserver

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
	"github.com/yousysadmin/mailyard/internal/domain/relaynode"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	nodemodel "github.com/yousysadmin/mailyard/internal/models/relaynode"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

// sharedTestApp mounts the shared pool's Test route over real stores.
// RelayNodeTLS is left nil on purpose: dialling a node then fails
// before any network is touched, which is a failed test with nothing
// to wait for.
func sharedTestApp(t *testing.T) (*fiber.App, *SharedStore, *relaynode.Store) {
	t.Helper()
	shared, nodes := testStores(t)
	h := &SharedHandler{Runtime: &env.Runtime{
		Config: &env.Config{},
		Store:  &store.Store{SharedSMTP: shared, RelayNode: nodes},
	}}
	app := fiber.New()
	app.Post("/:id/test", h.Test)

	return app, shared, nodes
}

func enrolledRow(t *testing.T, shared *SharedStore, nodes *relaynode.Store, mode string) *ssmodel.Shared {
	t.Helper()
	srv := &ssmodel.Shared{
		Server: ssmodel.Server{
			ID: ids.New(), Name: "node-" + mode, Host: "node.example.com", Port: 587,
			Encryption: smtpclient.EncryptionSTARTTLS, Status: ssmodel.StatusEnabled,
			AllowedEmails: []string{}, AllowedDomains: []string{},
		},
		SecurityMode: ssmodel.SecurityPermissive,
	}
	if err := shared.Put(t.Context(), srv); err != nil {
		t.Fatalf("Put shared: %v", err)
	}

	if err := nodes.Put(t.Context(), &nodemodel.Node{
		ID: ids.New(), ServerID: srv.ID, TokenHash: "x", Name: srv.Name, Mode: mode,
	}); err != nil {
		t.Fatalf("Put node: %v", err)
	}

	return srv
}

func postTest(t *testing.T, app *fiber.App, id string) (int, map[string]any) {
	t.Helper()
	res, err := app.Test(httptest.NewRequest(fiber.MethodPost, "/"+id+"/test", nil), fiber.TestConfig{Timeout: 0})
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	defer func() { _ = res.Body.Close() }()
	var body map[string]any
	if err := json.UnmarshalRead(res.Body, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	return res.StatusCode, body
}

// A relay node's status is its approval, so a connection test must not
// write there. A pull node is refused because nothing dials it, a
// listen node's failed dial is reported and nothing more - both rows
// stay enabled, where an ordinary server's failed test marks it invalid.
func TestAConnectionTestNeverUnapprovesARelayNode(t *testing.T) {
	app, shared, nodes := sharedTestApp(t)

	pull := enrolledRow(t, shared, nodes, nodemodel.ModePull)
	code, body := postTest(t, app, pull.ID)
	if code != http.StatusBadRequest {
		t.Fatalf("pull node test answered %d %v, want 400", code, body)
	}

	listen := enrolledRow(t, shared, nodes, nodemodel.ModeListen)
	code, body = postTest(t, app, listen.ID)
	if code != http.StatusOK || body["ok"] != false {
		t.Fatalf("listen node test answered %d %v, want 200 ok:false", code, body)
	}

	for _, id := range []string{pull.ID, listen.ID} {
		got, err := shared.Get(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}

		if got.Status != ssmodel.StatusEnabled || got.ValidationError != "" {
			t.Errorf("node row %s after test: status %q error %q, want enabled and no error",
				got.Name, got.Status, got.ValidationError)
		}
	}

	// The contrast: a plain server that fails its test does go invalid.
	plain := &ssmodel.Shared{
		Server: ssmodel.Server{
			ID: ids.New(), Name: "plain", Host: "127.0.0.1", Port: 1,
			Encryption: smtpclient.EncryptionNone, Status: ssmodel.StatusEnabled,
			AllowedEmails: []string{}, AllowedDomains: []string{},
		},
		SecurityMode: ssmodel.SecurityPermissive,
	}
	if err := shared.Put(t.Context(), plain); err != nil {
		t.Fatal(err)
	}

	code, body = postTest(t, app, plain.ID)
	if code != http.StatusOK || body["ok"] != false {
		t.Fatalf("plain server test answered %d %v, want 200 ok:false", code, body)
	}

	got, err := shared.Get(t.Context(), plain.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got.Status != ssmodel.StatusInvalid {
		t.Errorf("plain server after a failed test: status %q, want invalid", got.Status)
	}
}
