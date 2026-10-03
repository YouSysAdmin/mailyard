// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpserver

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/relaynode"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
	nodemodel "github.com/yousysadmin/mailyard/internal/models/relaynode"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

// A pending node is approved on the relay nodes page. The enable action
// on its server row must not take it into rotation on smtp:write alone.
func TestEnablingANodeRowIsRefused(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	proj := ids.New()
	if _, err := db.ExecContext(t.Context(), `
		INSERT INTO projects (id, name, slug, owner_id, created_at)
		VALUES ($1, 'p', $2, NULL, now())`, proj, proj); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	s := NewStore(db, crypto.New("0123456789abcdef0123456789abcdef"))
	nodes := relaynode.NewStore(db)
	srv := &ssmodel.Server{
		ID: ids.New(), ProjectID: proj, Name: "node", Host: "node.example.com", Port: 587,
		Encryption: "starttls", Status: ssmodel.StatusPending, CreatedAt: time.Now().UTC(),
	}
	if err := s.Put(t.Context(), srv); err != nil {
		t.Fatalf("put server: %v", err)
	}

	if err := nodes.Put(t.Context(), &nodemodel.Node{
		ID: ids.New(), ServerID: srv.ID, ProjectID: proj, TokenHash: "x", Name: "node", Mode: "listen",
	}); err != nil {
		t.Fatalf("put node: %v", err)
	}

	h := &Handler{Runtime: &env.Runtime{Config: &env.Config{}, Store: &store.Store{SMTPServer: s}}}
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals(domain.ContextKey, &domain.RequestContext{Project: &projmodel.Project{ID: proj}})

		return c.Next()
	})
	app.Post("/:id/enable", h.Enable)
	app.Post("/:id/disable", h.Disable)

	for _, action := range []string{"enable", "disable"} {
		res, err := app.Test(httptest.NewRequest(fiber.MethodPost, "/"+srv.ID+"/"+action, nil))
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}

		_ = res.Body.Close()
		if res.StatusCode != fiber.StatusBadRequest {
			t.Errorf("%s on a node row: status %d, want 400", action, res.StatusCode)
		}
	}

	got, err := s.Get(t.Context(), proj, srv.ID)
	if err != nil || got == nil {
		t.Fatalf("read back: %v", err)
	}

	if got.Status != ssmodel.StatusPending {
		t.Errorf("status = %q, want it still pending", got.Status)
	}
}
