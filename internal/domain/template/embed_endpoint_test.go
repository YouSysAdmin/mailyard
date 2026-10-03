// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package template

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
)

// The embed flag goes in on create, survives a PATCH that does not name
// it, turns off on one that does, and travels through export and
// import.
func TestTheEmbedFlagThroughTheEndpoints(t *testing.T) {
	s, _ := openStore(t)
	h := &Handler{Runtime: &env.Runtime{Store: &store.Store{Template: s}, Config: &env.Config{}}}
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals(domain.ContextKey, &domain.RequestContext{Project: &projmodel.Project{ID: projID}})

		return c.Next()
	})
	app.Post("/templates", h.Create)
	app.Patch("/templates/:id", h.Update)
	app.Get("/templates/:id/export", h.Export)
	app.Post("/templates/import", h.Import)

	call := func(method, path, body string) (int, string) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}

		defer func() { _ = res.Body.Close() }()
		b, _ := io.ReadAll(res.Body)

		return res.StatusCode, string(b)
	}

	type tplBody struct {
		Template struct {
			ID          string `json:"id"`
			Description string `json:"description"`
			EmbedImages bool   `json:"embed_images"`
		} `json:"template"`
	}

	read := func(body string) tplBody {
		t.Helper()
		var out tplBody
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Fatalf("decode %s: %v", body, err)
		}

		return out
	}

	code, body := call("POST", "/templates", `{"name":"welcome","embed_images":true}`)
	if code != 201 || !read(body).Template.EmbedImages {
		t.Fatalf("create = %d %s, want the flag on", code, body)
	}

	id := read(body).Template.ID
	code, body = call("PATCH", "/templates/"+id, `{"description":"new"}`)
	if got := read(body).Template; code != 200 || !got.EmbedImages || got.Description != "new" {
		t.Fatalf("partial patch = %d %s, want the flag kept", code, body)
	}

	code, body = call("GET", "/templates/"+id+"/export", "")
	if code != 200 || !strings.Contains(body, `"embed_images":true`) {
		t.Fatalf("export = %d %s, want the flag", code, body)
	}

	var exported struct {
		Export jsontext.Value `json:"export"`
	}
	if err := json.Unmarshal([]byte(body), &exported); err != nil {
		t.Fatal(err)
	}

	doc := strings.Replace(string(exported.Export), `"name":"welcome"`, `"name":"welcome-copy"`, 1)
	code, body = call("POST", "/templates/import", doc)
	if code != 201 || !read(body).Template.EmbedImages {
		t.Fatalf("import = %d %s, want the flag", code, body)
	}

	code, body = call("PATCH", "/templates/"+id, `{"embed_images":false}`)
	if got := read(body).Template; code != 200 || got.EmbedImages || got.Description != "new" {
		t.Fatalf("patch off = %d %s", code, body)
	}
}
