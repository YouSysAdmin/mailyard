// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpserver

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

// projectApp mounts the project PATCH route over a real store, with
// the request context the middleware would have set.
func projectApp(t *testing.T) (*fiber.App, *Store, string) {
	t.Helper()
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	proj := ids.New()
	if _, err := db.ExecContext(t.Context(), `
		INSERT INTO projects (id, name, slug, owner_id, created_at)
		VALUES ($1, 'p', $2, NULL, now())`, proj, proj); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	s := NewStore(db, crypto.New("0123456789abcdef0123456789abcdef"))
	cfg := &env.Config{}
	cfg.Sending.AllowPrivateSMTPTargets = true
	h := &Handler{Runtime: &env.Runtime{Config: cfg, Store: &store.Store{SMTPServer: s}}}
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals(domain.ContextKey, &domain.RequestContext{Project: &projmodel.Project{ID: proj}})

		return c.Next()
	})
	app.Patch("/:id", h.Update)

	return app, s, proj
}

func patch(t *testing.T, app *fiber.App, id, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(fiber.MethodPatch, "/"+id, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := app.Test(req, fiber.TestConfig{Timeout: 0})
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	defer func() { _ = res.Body.Close() }()
	var out map[string]any
	if err := json.UnmarshalRead(res.Body, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}

	return res.StatusCode, out
}

// An empty password leaves the stored one, a username is trimmed, a
// priority edit leaves an invalid server invalid with its reason, and a
// dial edit replaces the reason without putting the server back.
func TestAPatchNeverPutsAServerBackIntoRotation(t *testing.T) {
	app, s, proj := projectApp(t)
	srv := &ssmodel.Server{
		ID: ids.New(), ProjectID: proj, Name: "relay", Host: "smtp.example.net", Port: 587,
		Username: "box", Password: "secret", Encryption: "starttls",
		Status: ssmodel.StatusEnabled, CreatedAt: time.Now().UTC(),
	}
	if err := s.Put(t.Context(), srv); err != nil {
		t.Fatal(err)
	}

	if _, err := s.MarkInvalid(t.Context(), proj, srv.ID, "535 bad credentials"); err != nil {
		t.Fatal(err)
	}

	code, body := patch(t, app, srv.ID, `{"priority": 3, "password": "", "username": "  box2  "}`)
	if code != http.StatusOK {
		t.Fatalf("PATCH answered %d %v", code, body)
	}

	got, err := s.Get(t.Context(), proj, srv.ID)
	if err != nil || got == nil {
		t.Fatalf("Get: %v", err)
	}

	if got.Password != "secret" {
		t.Errorf("password = %q, an empty PATCH value must leave it", got.Password)
	}

	if got.Username != "box2" {
		t.Errorf("username = %q, want it trimmed", got.Username)
	}

	if got.Status != ssmodel.StatusInvalid {
		t.Errorf("status = %q after a credential edit, want invalid until a test", got.Status)
	}

	if got.ValidationError != ssmodel.SettingsChangedNote {
		t.Errorf("reason = %q, want the settings note after a login change", got.ValidationError)
	}

	answered, _ := body["smtp_server"].(map[string]any)
	if answered["status"] != ssmodel.StatusInvalid {
		t.Errorf("answer status = %v, want the stored invalid", answered["status"])
	}

	if err := s.SetStatus(t.Context(), proj, srv.ID, ssmodel.StatusInvalid, "535 again", nil); err != nil {
		t.Fatal(err)
	}

	if code, body = patch(t, app, srv.ID, `{"priority": 4}`); code != http.StatusOK {
		t.Fatalf("PATCH answered %d %v", code, body)
	}

	got, _ = s.Get(t.Context(), proj, srv.ID)
	if got.Status != ssmodel.StatusInvalid || got.ValidationError != "535 again" {
		t.Errorf("after a priority edit: %q %q, want invalid with its reason", got.Status, got.ValidationError)
	}
}
