// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package webhook

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/dispatch"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	"github.com/yousysadmin/mailyard/internal/models/permission"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
	whmodel "github.com/yousysadmin/mailyard/internal/models/webhook"
)

// A webhook carries what its events describe, so subscribing to an
// event and reading a delivery body both take read on that resource.
func TestAWebhookNeedsReadOnWhatItsEventsDescribe(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := NewStore(db, crypto.New("0123456789abcdef0123456789abcdef"))
	ctx := t.Context()

	proj := ids.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, owner_id, created_at)
		VALUES ($1, 'acme', $2, NULL, now())`, proj, proj); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	cfg := &env.Config{}
	cfg.Webhook.AllowPrivateTargets = true
	disp := dispatch.New(&DispatchSink{Store: s}, dispatch.Config{Timeout: time.Second, MaxAttempts: 1}, slog.New(slog.DiscardHandler))
	t.Cleanup(func() { disp.Close(time.Second) })

	h := &Handler{Runtime: &env.Runtime{Config: cfg, Store: &store.Store{Webhook: s}, Dispatch: disp}}

	hooksOnly := permission.NewSet(
		permission.Of(permission.ResourceWebhook, permission.ActionRead),
		permission.Of(permission.ResourceWebhook, permission.ActionWrite))
	withEmails := permission.NewSet(
		permission.Of(permission.ResourceWebhook, permission.ActionRead),
		permission.Of(permission.ResourceWebhook, permission.ActionWrite),
		permission.Of(permission.ResourceEmails, permission.ActionRead))

	app := func(perms permission.Set) *fiber.App {
		a := fiber.New()
		a.Use(func(c fiber.Ctx) error {
			c.Locals(domain.ContextKey, &domain.RequestContext{
				Ctx: c, Project: &projmodel.Project{ID: proj}, Permissions: perms,
			})

			return c.Next()
		})
		a.Post("/webhooks", h.Create)
		a.Patch("/webhooks/:id", h.Update)
		a.Get("/webhooks/:id/deliveries", h.Deliveries)
		a.Post("/webhooks/:id/deliveries/:deliveryId/redeliver", h.Redeliver)

		return a
	}

	call := func(perms permission.Set, method, path, body string) (int, string) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app(perms).Test(req)
		if err != nil {
			t.Fatal(err)
		}

		defer func() { _ = resp.Body.Close() }()

		raw, _ := io.ReadAll(resp.Body)

		return resp.StatusCode, string(raw)
	}

	subscribe := func(events string) string {
		return `{"url":"https://hooks.example.test/in","events":[` + events + `]}`
	}

	refused := []struct{ name, events string }{
		{"email.sent", `"email.sent"`},
		{"inbound.received", `"inbound.received"`},
		{"campaign.started", `"campaign.started"`},
		{"wildcard", `"*"`},
	}
	for _, tc := range refused {
		if code, body := call(hooksOnly, "POST", "/webhooks", subscribe(tc.events)); code != fiber.StatusForbidden {
			t.Errorf("webhooks only, subscribing to %s: %d %s, want 403", tc.name, code, body)
		}
	}

	code, body := call(withEmails, "POST", "/webhooks", subscribe(`"email.sent","email.failed"`))
	if code != fiber.StatusCreated {
		t.Fatalf("with emails:read, subscribing to email.*: %d %s, want 201", code, body)
	}

	var created CreateResponse
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatal(err)
	}

	hookID := created.Webhook.ID

	if code, body := call(withEmails, "POST", "/webhooks", subscribe(`"*"`)); code != fiber.StatusForbidden {
		t.Errorf("with emails:read only, subscribing to *: %d %s, want 403", code, body)
	}

	if code, body := call(hooksOnly, "PATCH", "/webhooks/"+hookID, `{"events":["email.queued"]}`); code != fiber.StatusForbidden {
		t.Errorf("webhooks only, editing in email.queued: %d %s, want 403", code, body)
	}

	sent := &whmodel.Delivery{WebhookID: hookID, ProjectID: proj, Event: whmodel.EventEmailSent,
		Status: whmodel.DeliverySuccess, Attempt: 1, CreatedAt: time.Now().UTC(),
		Payload: `{"event":"email.sent","data":{"subject":"SECRET-SUBJECT"}}`}
	if err := s.RecordDelivery(ctx, sent); err != nil {
		t.Fatal(err)
	}

	ping := &whmodel.Delivery{WebhookID: hookID, ProjectID: proj, Event: whmodel.EventWebhookTest,
		Status: whmodel.DeliverySuccess, Attempt: 1, CreatedAt: time.Now().UTC().Add(time.Second),
		Payload: `{"event":"webhook.test","data":{"message":"PING-BODY"}}`}
	if err := s.RecordDelivery(ctx, ping); err != nil {
		t.Fatal(err)
	}

	code, body = call(hooksOnly, "GET", "/webhooks/"+hookID+"/deliveries", "")
	if code != fiber.StatusOK || strings.Contains(body, "SECRET-SUBJECT") {
		t.Errorf("webhooks only read an email.sent body from the log: %d %s", code, body)
	}

	if !strings.Contains(body, "PING-BODY") {
		t.Errorf("the test post body describes nothing and should stay readable: %s", body)
	}

	if code, body = call(withEmails, "GET", "/webhooks/"+hookID+"/deliveries", ""); !strings.Contains(body, "SECRET-SUBJECT") {
		t.Errorf("with emails:read the email.sent body was withheld: %d %s", code, body)
	}

	if code, body := call(hooksOnly, "POST", "/webhooks/"+hookID+"/deliveries/"+sent.ID+"/redeliver", ""); code != fiber.StatusForbidden {
		t.Errorf("webhooks only, redelivering email.sent: %d %s, want 403", code, body)
	}
}
