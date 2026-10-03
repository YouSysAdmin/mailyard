// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package sender

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
	smodel "github.com/yousysadmin/mailyard/internal/models/sender"
)

// attach_key is a PGP setting. Turning it on for an S/MIME key is
// refused, where a PGP key takes it.
func TestAttachKeyIsRefusedForAnSMIMEKey(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	proj := ids.New()
	dbtest.Schema(t, db, `
        INSERT INTO projects (id, name, slug, default_language, created_at)
        VALUES ('`+proj+`', 'Test', 'test', 'en', now())`)
	s := NewStore(db, crypto.New("test-key-test-key-test-key-test-key-test"))
	ctx := t.Context()

	seed := func(email, kind string) string {
		m := &smodel.Sender{ID: ids.New(), ProjectID: proj, Email: email}
		if err := s.Put(ctx, m); err != nil {
			t.Fatal(err)
		}

		if _, err := s.AddSigning(ctx, &smodel.SigningKey{
			SenderID: m.ID, ProjectID: proj, Kind: kind, PrivateKey: "k", PublicKey: "p", Sign: true,
		}); err != nil {
			t.Fatal(err)
		}

		return m.ID
	}

	smime := seed("smime@example.com", smodel.SigningSMIME)
	pgp := seed("pgp@example.com", smodel.SigningPGP)

	h := &Handler{Runtime: &env.Runtime{Config: &env.Config{}, Store: &store.Store{Sender: s}}}
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals(domain.ContextKey, &domain.RequestContext{Project: &projmodel.Project{ID: proj}})

		return c.Next()
	})
	app.Patch("/:id/signing", h.SetSigningFlags)

	patch := func(id string) int {
		req := httptest.NewRequest(fiber.MethodPatch, "/"+id+"/signing", strings.NewReader(`{"attach_key": true}`))
		req.Header.Set("Content-Type", "application/json")
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}

		_ = res.Body.Close()

		return res.StatusCode
	}

	if got := patch(smime); got != http.StatusBadRequest {
		t.Errorf("S/MIME attach_key answered %d, want 400", got)
	}

	if got := patch(pgp); got != http.StatusOK {
		t.Errorf("PGP attach_key answered %d, want 200", got)
	}

	m, _ := s.Get(ctx, proj, smime)
	if m.Signing.AttachKey {
		t.Error("the S/MIME key was left with attach_key on")
	}
}
