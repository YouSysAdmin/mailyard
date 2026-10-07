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
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/blob"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/plan"
	"github.com/yousysadmin/mailyard/internal/domain/project"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
	tmodel "github.com/yousysadmin/mailyard/internal/models/template"
)

const otherProjID = "1d7c2f0e-5b3a-4c8d-9e6f-7a1b2c3d4e5f"

// A PNG signature followed by filler, which is all the sniffer reads.
var pngBytes = append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), bytes.Repeat([]byte{1}, 64)...)

// paddedPNG is a PNG header padded with fill to exactly size bytes, so
// two sizes or two fills are two distinct images of a known size.
func paddedPNG(t *testing.T, size int, fill byte) []byte {
	t.Helper()
	head := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	if size < len(head) {
		t.Fatalf("a png cannot be %d bytes", size)
	}

	return append(slices.Clone(head), bytes.Repeat([]byte{fill}, size-len(head))...)
}

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
	// The upload asks the plan how much it may store, so the stores a
	// plan is resolved through are wired in: no plan here means no cap.
	h := &Handler{Runtime: &env.Runtime{
		Store:  &store.Store{Template: s, Project: project.NewStore(s.DB()), Plan: plan.NewStore(s.DB())},
		Config: cfg, Blob: files,
	}}

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

// unreferencedSince reads the sweep's stamp on one image, nil when
// unset.
func unreferencedSince(t *testing.T, s *Store, id string) *time.Time {
	t.Helper()
	var at *time.Time
	if err := s.QueryRow(t.Context(),
		`SELECT unreferenced_since FROM template_assets WHERE id = ?`, id).Scan(&at); err != nil {
		t.Fatalf("read unreferenced_since: %v", err)
	}

	return at
}

// The sweep stamps an image no template uses, leaves a used one alone,
// and clears the stamp once a template uses the image again.
func TestUnusedAssetsAreMarkedAndUsedOnesCleared(t *testing.T) {
	s := openAssetStore(t)
	ctx := t.Context()
	used := newAsset(projID, "used", "k-used")
	unused := newAsset(projID, "unused", "k-unused")
	for _, a := range []*tmodel.Asset{used, unused} {
		if _, err := s.CreateAsset(ctx, a); err != nil {
			t.Fatal(err)
		}
	}

	tpl := newTemplate(t, s, "welcome")
	l := loc("en")
	l.VersionID = *tpl.ActiveVersionID
	l.HTML = `<img src="https://mail.example.com/assets/` + used.PublicToken + `">`
	if err := s.PutLocalization(ctx, projID, l); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	if marked, cleared, err := s.MarkUnreferencedAssets(ctx, now); err != nil || marked != 1 || cleared != 0 {
		t.Fatalf("first mark = %d, %d, %v, want 1 marked", marked, cleared, err)
	}

	if at := unreferencedSince(t, s, unused.ID); at == nil || !at.Equal(now) {
		t.Fatalf("unused image stamp = %v, want %v", at, now)
	}

	if at := unreferencedSince(t, s, used.ID); at != nil {
		t.Fatalf("used image stamped %v", at)
	}

	// A later pass keeps the first stamp, the image has been unused
	// since then.
	if marked, _, err := s.MarkUnreferencedAssets(ctx, now.Add(time.Hour)); err != nil || marked != 0 {
		t.Fatalf("second mark = %d, %v, want nothing new", marked, err)
	}

	if at := unreferencedSince(t, s, unused.ID); at == nil || !at.Equal(now) {
		t.Fatalf("the stamp moved to %v", at)
	}

	l.HTML = `<img src="https://mail.example.com/assets/` + unused.PublicToken + `">`
	if err := s.PutLocalization(ctx, projID, l); err != nil {
		t.Fatal(err)
	}

	if marked, cleared, err := s.MarkUnreferencedAssets(ctx, now); err != nil || marked != 1 || cleared != 1 {
		t.Fatalf("swap mark = %d, %d, %v, want one each way", marked, cleared, err)
	}

	if at := unreferencedSince(t, s, unused.ID); at != nil {
		t.Fatalf("an image back in a template kept its stamp %v", at)
	}

	if at := unreferencedSince(t, s, used.ID); at == nil {
		t.Fatal("the image taken out of the template was not stamped")
	}
}

// The purge takes only images unused since before the cutoff, never
// one a template uses again, and returns their storage keys.
func TestThePurgeTakesOnlyLongUnusedAssets(t *testing.T) {
	s := openAssetStore(t)
	ctx := t.Context()
	old := newAsset(projID, "old", "k-old")
	inline := newAsset(otherProjID, "inline", "")
	young := newAsset(projID, "young", "k-young")
	back := newAsset(projID, "back", "k-back")
	for _, a := range []*tmodel.Asset{old, inline, young, back} {
		if _, err := s.CreateAsset(ctx, a); err != nil {
			t.Fatal(err)
		}
	}

	now := time.Now().UTC()
	if _, _, err := s.MarkUnreferencedAssets(ctx, now.AddDate(0, 0, -40)); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Exec(ctx, `UPDATE template_assets SET unreferenced_since = ? WHERE id = ?`,
		now.AddDate(0, 0, -5), young.ID); err != nil {
		t.Fatal(err)
	}

	// Put back into a template after the stamp, before any pass clears it.
	tpl := newTemplate(t, s, "welcome")
	l := loc("en")
	l.VersionID = *tpl.ActiveVersionID
	l.HTML = `<img src="https://mail.example.com/assets/` + back.PublicToken + `">`
	if err := s.PutLocalization(ctx, projID, l); err != nil {
		t.Fatal(err)
	}

	keys, err := s.PurgeUnreferencedAssets(ctx, now.AddDate(0, 0, -30))
	if err != nil {
		t.Fatal(err)
	}

	slices.Sort(keys)
	if want := []string{"", "k-old"}; !slices.Equal(keys, want) {
		t.Fatalf("purged keys %q, want %q", keys, want)
	}

	for _, a := range []*tmodel.Asset{old, inline} {
		if got, err := s.GetAsset(ctx, a.ProjectID, a.ID); err != nil || got != nil {
			t.Fatalf("%s survived the purge: %+v, %v", a.SHA256, got, err)
		}
	}

	for _, a := range []*tmodel.Asset{young, back} {
		if got, err := s.GetAsset(ctx, a.ProjectID, a.ID); err != nil || got == nil {
			t.Fatalf("%s was purged: %v", a.SHA256, err)
		}
	}
}

// embeddingMessage stores a message of projID that embeds the image,
// with its status and creation time.
func embeddingMessage(t *testing.T, s *Store, projID, assetID, status string, createdAt time.Time) string {
	t.Helper()
	id := ids.New()
	atts := `[{"filename":"logo.png","size":3,"template_asset_id":"` + assetID + `","content_id":"` + assetID + `@mailyard"}]`
	if _, err := s.Exec(t.Context(), `
        INSERT INTO emails (id, project_id, sender, recipients, subject, attachments_json, status, created_at)
        VALUES (?, ?, 'a@b.test', '["c@d.test"]', 's', ?, ?, ?)`,
		id, projID, atts, status, createdAt); err != nil {
		t.Fatalf("store a message embedding the image: %v", err)
	}

	return id
}

// The project's own images are found by token, without their bytes,
// and another project's token matches nothing.
func TestAssetsAreFoundByTokenWithinTheProject(t *testing.T) {
	s := openAssetStore(t)
	ctx := t.Context()
	mine := newAsset(projID, "mine", "")
	mine.Content = "UE5H"
	theirs := newAsset(otherProjID, "theirs", "k-theirs")
	for _, a := range []*tmodel.Asset{mine, theirs} {
		if _, err := s.CreateAsset(ctx, a); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.AssetsByTokens(ctx, projID, []string{mine.PublicToken, theirs.PublicToken, "nope"})
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 || got[0].ID != mine.ID || got[0].Content != "" {
		t.Fatalf("AssetsByTokens = %+v, want only the project's image without bytes", got)
	}

	if got, err := s.AssetsByTokens(ctx, projID, nil); err != nil || len(got) != 0 {
		t.Fatalf("no tokens = %v, %v", got, err)
	}
}

// An image a message waiting to be sent embeds is not deleted, and the
// handler can tell that apart from a template using it. A sent message
// does not hold it back, nor a message of another project.
func TestAnAssetEmbeddedInAWaitingMessageIsNotDeleted(t *testing.T) {
	s := openAssetStore(t)
	ctx := t.Context()
	a := newAsset(projID, "held", "k-held")
	if _, err := s.CreateAsset(ctx, a); err != nil {
		t.Fatal(err)
	}

	embeddingMessage(t, s, otherProjID, a.ID, "scheduled", time.Now())
	embeddingMessage(t, s, projID, a.ID, "sent", time.Now())
	if pending, err := s.AssetPending(ctx, projID, a.ID); err != nil || pending {
		t.Fatalf("pending with only a sent message = %v, %v", pending, err)
	}

	waiting := embeddingMessage(t, s, projID, a.ID, "scheduled", time.Now())
	if pending, err := s.AssetPending(ctx, projID, a.ID); err != nil || !pending {
		t.Fatalf("pending with a scheduled message = %v, %v, want true", pending, err)
	}

	if gone, err := s.DeleteAsset(ctx, projID, a.ID); err != nil || gone {
		t.Fatalf("delete under a scheduled message = %v, %v, want refused", gone, err)
	}

	if _, err := s.Exec(ctx, `UPDATE emails SET status = 'sent' WHERE id = ?`, waiting); err != nil {
		t.Fatal(err)
	}

	if gone, err := s.DeleteAsset(ctx, projID, a.ID); err != nil || !gone {
		t.Fatalf("delete once sent = %v, %v, want deleted", gone, err)
	}
}

// The purge keeps a long unused image while any stored message embeds
// it, and takes it once that message is gone.
func TestThePurgeKeepsAnAssetAMessageEmbeds(t *testing.T) {
	s := openAssetStore(t)
	ctx := t.Context()
	a := newAsset(projID, "embedded", "k-embedded")
	if _, err := s.CreateAsset(ctx, a); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	if _, _, err := s.MarkUnreferencedAssets(ctx, now.AddDate(0, 0, -40)); err != nil {
		t.Fatal(err)
	}

	msg := embeddingMessage(t, s, projID, a.ID, "scheduled", now.AddDate(0, 0, -45))
	keys, err := s.PurgeUnreferencedAssets(ctx, now.AddDate(0, 0, -30))
	if err != nil || len(keys) != 0 {
		t.Fatalf("purge under an embedding message = %q, %v, want nothing", keys, err)
	}

	if _, err := s.Exec(ctx, `DELETE FROM emails WHERE id = ?`, msg); err != nil {
		t.Fatal(err)
	}

	keys, err = s.PurgeUnreferencedAssets(ctx, now.AddDate(0, 0, -30))
	if err != nil || !slices.Equal(keys, []string{"k-embedded"}) {
		t.Fatalf("purge once the message went = %q, %v", keys, err)
	}
}

// A plan caps builder images in BYTES across the project, judged when
// an image is uploaded: the image that would cross the line is
// refused, one that fits is stored, and the same bytes again cost
// nothing because they are not stored twice.
func TestAnUploadIsBoundedByThePlansBytes(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	dbtest.Schema(t, db, `
        INSERT INTO plans (id, name, is_default, max_template_asset_bytes, created_at)
        VALUES ('0f1e2d3c-4b5a-4968-8776-655443322110', 'Small', TRUE, 200, now());
        INSERT INTO projects (id, name, slug, default_language, created_at)
        VALUES ('`+projID+`', 'Test', 'test', 'en', now())`)
	s := NewStore(db)
	files, err := blob.New(blob.Config{Backend: "fs", FSPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	cfg := &env.Config{}
	cfg.Server.PublicURL = "https://mail.example.com/"
	cfg.Sending.MaxAttachmentSize = 1024
	h := &Handler{Runtime: &env.Runtime{
		Store:  &store.Store{Template: s, Project: project.NewStore(db), Plan: plan.NewStore(db)},
		Config: cfg, Blob: files,
	}}
	a := fiber.New()
	a.Use(func(c fiber.Ctx) error {
		c.Locals(domain.ContextKey, &domain.RequestContext{Project: &projmodel.Project{ID: projID}})

		return c.Next()
	})
	a.Post("/template-assets", h.UploadAsset)

	upload := func(name string, raw []byte) (int, string) {
		t.Helper()
		body := `{"filename":"` + name + `","content":"` + base64.StdEncoding.EncodeToString(raw) + `"}`
		req := httptest.NewRequest("POST", "/template-assets", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res, err := a.Test(req)
		if err != nil {
			t.Fatal(err)
		}

		defer func() { _ = res.Body.Close() }()
		b, _ := io.ReadAll(res.Body)

		return res.StatusCode, string(b)
	}

	// Two distinct images of 120 bytes each: the first fits under 200,
	// the second would not.
	first, second := paddedPNG(t, 120, 1), paddedPNG(t, 120, 2)
	if code, body := upload("one.png", first); code != 201 {
		t.Fatalf("the first image: %d %s", code, body)
	}

	if code, body := upload("two.png", second); code != 429 || !strings.Contains(body, "bytes of template images") {
		t.Fatalf("an image past the cap: %d %s, want 429 naming the cap", code, body)
	}

	if code, _ := upload("one-again.png", first); code != 200 {
		t.Errorf("the same bytes again: %d, want 200 and no second row", code)
	}

	used, err := s.AssetBytes(t.Context(), projID)
	if err != nil || used != 120 {
		t.Errorf("bytes in use = %d, %v, want 120", used, err)
	}
}
