// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package template

import (
	"bytes"
	"encoding/base64"
	"encoding/json/v2"
	"io"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/blob"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
	tmodel "github.com/yousysadmin/mailyard/internal/models/template"
)

const otherProjID = "1d7c2f0e-5b3a-4c8d-9e6f-7a1b2c3d4e5f"

// A PNG signature followed by filler, which is all the sniffer reads.
var pngBytes = append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), bytes.Repeat([]byte{1}, 64)...)

func openAssetStore(t *testing.T) *Store {
	t.Helper()
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	dbtest.Schema(t, db, `
        INSERT INTO projects (id, name, slug, default_language, created_at)
        VALUES ('`+projID+`', 'Test', 'test', 'en', now()),
               ('`+otherProjID+`', 'Other', 'other', 'en', now())`)

	return NewStore(db)
}

func newAsset(projID, sum, key string) *tmodel.Asset {
	return &tmodel.Asset{
		ID: ids.New(), ProjectID: projID, Filename: "logo.png", ContentType: "image/png",
		Size: 3, SHA256: sum, PublicToken: newAssetToken(), StorageKey: key,
	}
}

// The same bytes are one row per project, and a public token finds its
// image whichever project holds it.
func TestAnAssetIsStoredOncePerProject(t *testing.T) {
	s := openAssetStore(t)
	ctx := t.Context()

	first := newAsset(projID, "abc", "k1")
	if ok, err := s.CreateAsset(ctx, first); err != nil || !ok {
		t.Fatalf("first create = %v, %v", ok, err)
	}

	if ok, err := s.CreateAsset(ctx, newAsset(projID, "abc", "k2")); err != nil || ok {
		t.Fatalf("a second copy of the same bytes = %v, %v, want refused", ok, err)
	}

	if ok, err := s.CreateAsset(ctx, newAsset(otherProjID, "abc", "k3")); err != nil || !ok {
		t.Fatalf("another project's copy = %v, %v, want stored", ok, err)
	}

	got, err := s.GetAssetBySHA256(ctx, projID, "abc")
	if err != nil || got == nil || got.ID != first.ID {
		t.Fatalf("GetAssetBySHA256 = %+v, %v, want the first row", got, err)
	}

	byToken, err := s.GetAssetByToken(ctx, first.PublicToken)
	if err != nil || byToken == nil || byToken.ID != first.ID {
		t.Fatalf("GetAssetByToken = %+v, %v", byToken, err)
	}

	if none, err := s.GetAssetByToken(ctx, "nope"); err != nil || none != nil {
		t.Fatalf("an unknown token = %+v, %v, want nil", none, err)
	}

	list, total, err := s.FindAssets(ctx, projID, 0, 0)
	if err != nil || total != 1 || len(list) != 1 || list[0].Content != "" {
		t.Fatalf("FindAssets = %d rows, total %d, %v", len(list), total, err)
	}

	keys, err := s.StorageKeysForProject(ctx, projID)
	if err != nil || !slices.Contains(keys, "k1") || slices.Contains(keys, "k3") {
		t.Fatalf("StorageKeysForProject = %v, %v, want k1 and not the other project's", keys, err)
	}
}

// An image a template still references cannot be deleted, and one
// from another project is not found.
func TestAnAssetInUseIsNotDeleted(t *testing.T) {
	s := openAssetStore(t)
	ctx := t.Context()
	a := newAsset(projID, "abc", "")
	if _, err := s.CreateAsset(ctx, a); err != nil {
		t.Fatal(err)
	}

	tpl := newTemplate(t, s, "welcome")
	l := loc("en")
	l.VersionID = *tpl.ActiveVersionID
	l.HTML = `<img src="https://mail.example.com/assets/` + a.PublicToken + `">`
	if err := s.PutLocalization(ctx, projID, l); err != nil {
		t.Fatal(err)
	}

	if gone, err := s.DeleteAsset(ctx, otherProjID, a.ID); err != nil || gone {
		t.Fatalf("another project deleted the image = %v, %v", gone, err)
	}

	if gone, err := s.DeleteAsset(ctx, projID, a.ID); err != nil || gone {
		t.Fatalf("delete while used = %v, %v, want refused", gone, err)
	}

	l.HTML = "<p>no image</p>"
	if err := s.PutLocalization(ctx, projID, l); err != nil {
		t.Fatal(err)
	}

	if gone, err := s.DeleteAsset(ctx, projID, a.ID); err != nil || !gone {
		t.Fatalf("delete once unused = %v, %v", gone, err)
	}
}

func TestTheAssetEndpoints(t *testing.T) {
	s := openAssetStore(t)
	files, err := blob.New(blob.Config{Backend: "fs", FSPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	cfg := &env.Config{}
	cfg.Server.PublicURL = "https://mail.example.com/"
	cfg.Sending.MaxAttachmentSize = 1024
	h := &Handler{Runtime: &env.Runtime{Store: &store.Store{Template: s}, Config: cfg, Blob: files}}

	app := func(projID string) *fiber.App {
		a := fiber.New()
		a.Get("/assets/:token", h.ServeAsset)
		a.Use(func(c fiber.Ctx) error {
			c.Locals(domain.ContextKey, &domain.RequestContext{Project: &projmodel.Project{ID: projID}})

			return c.Next()
		})
		a.Get("/template-assets", h.ListAssets)
		a.Post("/template-assets", h.UploadAsset)
		a.Delete("/template-assets/:id", h.DeleteAsset)

		return a
	}

	call := func(a *fiber.App, method, path, body string) (int, string, map[string]string) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res, err := a.Test(req)
		if err != nil {
			t.Fatal(err)
		}

		defer func() { _ = res.Body.Close() }()
		b, _ := io.ReadAll(res.Body)
		hdr := map[string]string{}
		for k := range res.Header {
			hdr[k] = res.Header.Get(k)
		}

		return res.StatusCode, string(b), hdr
	}

	upload := func(raw []byte) string {
		return `{"filename":"logo.png","content":"` + base64.StdEncoding.EncodeToString(raw) + `"}`
	}

	own, other := app(projID), app(otherProjID)

	refused := []struct {
		name string
		raw  []byte
	}{
		{"svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"></svg>`)},
		{"xml svg", []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"></svg>`)},
		{"html", []byte(`<html><body>hi</body></html>`)},
		{"text", []byte("just text")},
		{"oversize png", append(slices.Clone(pngBytes), make([]byte, 2048)...)},
	}
	for _, tc := range refused {
		if code, body, _ := call(own, "POST", "/template-assets", upload(tc.raw)); code != 400 {
			t.Errorf("%s: %d %s, want 400", tc.name, code, body)
		}
	}

	code, body, _ := call(own, "POST", "/template-assets", upload(pngBytes))
	if code != 201 {
		t.Fatalf("upload: %d %s", code, body)
	}

	var created AssetResponse
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(created.Asset.URL, "https://mail.example.com/assets/") || created.Asset.ContentType != "image/png" {
		t.Fatalf("uploaded = %+v", created.Asset)
	}

	code, body, _ = call(own, "POST", "/template-assets", upload(pngBytes))
	if code != 200 || !strings.Contains(body, created.Asset.ID) {
		t.Fatalf("the same bytes again: %d %s, want 200 with the stored image", code, body)
	}

	token := created.Asset.URL[strings.LastIndex(created.Asset.URL, "/")+1:]
	code, body, hdr := call(other, "GET", "/assets/"+token, "")
	if code != 200 || body != string(pngBytes) {
		t.Fatalf("serve: %d, %d bytes", code, len(body))
	}

	for k, want := range map[string]string{
		"Content-Type":           "image/png",
		"X-Content-Type-Options": "nosniff",
		"Cache-Control":          "public, max-age=31536000, immutable",
	} {
		if hdr[k] != want {
			t.Errorf("%s = %q, want %q", k, hdr[k], want)
		}
	}

	if !strings.HasPrefix(hdr["Content-Disposition"], "inline") {
		t.Errorf("Content-Disposition = %q, want inline", hdr["Content-Disposition"])
	}

	if code, _, _ := call(own, "GET", "/assets/"+newAssetToken(), ""); code != 404 {
		t.Errorf("an unknown token answered %d, want 404", code)
	}

	// Another project neither lists nor deletes it.
	if _, body, _ := call(other, "GET", "/template-assets", ""); !strings.Contains(body, `"assets":[]`) {
		t.Errorf("another project's list = %s", body)
	}

	if code, _, _ := call(other, "DELETE", "/template-assets/"+created.Asset.ID, ""); code != 404 {
		t.Errorf("another project's delete answered %d, want 404", code)
	}

	tpl := newTemplate(t, s, "welcome")
	l := loc("en")
	l.VersionID = *tpl.ActiveVersionID
	l.HTML = `<img src="` + created.Asset.URL + `">`
	if err := s.PutLocalization(t.Context(), projID, l); err != nil {
		t.Fatal(err)
	}

	if code, body, _ := call(own, "DELETE", "/template-assets/"+created.Asset.ID, ""); code != 409 {
		t.Errorf("delete while used: %d %s, want 409", code, body)
	}

	l.HTML = "<p>none</p>"
	if err := s.PutLocalization(t.Context(), projID, l); err != nil {
		t.Fatal(err)
	}

	if code, body, _ := call(own, "DELETE", "/template-assets/"+created.Asset.ID, ""); code != 204 {
		t.Errorf("delete once unused: %d %s, want 204", code, body)
	}

	if code, _, _ := call(own, "GET", "/assets/"+token, ""); code != 404 {
		t.Errorf("a deleted image answered %d, want 404", code)
	}

	// No public address, no upload: a relative URL loads in no mail client.
	cfg.Server.PublicURL = ""
	if code, body, _ := call(own, "POST", "/template-assets", upload(pngBytes)); code != 400 ||
		!strings.Contains(body, "public_url") {
		t.Errorf("upload without public_url: %d %s, want 400 naming it", code, body)
	}
}
