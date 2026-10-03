// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"bytes"
	"context"
	"encoding/base64"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/attachcache"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
	tmodel "github.com/yousysadmin/mailyard/internal/models/template"
)

// fakeAssets holds builder images of several projects and counts reads
// of one image with its bytes.
type fakeAssets struct {
	store.TemplateStore
	assets  []*tmodel.Asset
	asked   [][]string
	lookups atomic.Int32
}

func (f *fakeAssets) ListAttachments(context.Context, string, string) ([]*tmodel.Attachment, error) {
	return nil, nil
}

func (f *fakeAssets) AssetsByTokens(_ context.Context, projID string, tokens []string) ([]*tmodel.Asset, error) {
	f.asked = append(f.asked, tokens)
	var out []*tmodel.Asset
	for _, a := range f.assets {
		for _, tok := range tokens {
			if a.ProjectID == projID && a.PublicToken == tok {
				out = append(out, a)
			}
		}
	}

	return out, nil
}

func (f *fakeAssets) GetAsset(_ context.Context, projID, id string) (*tmodel.Asset, error) {
	f.lookups.Add(1)
	for _, a := range f.assets {
		if a.ProjectID == projID && a.ID == id {
			return a, nil
		}
	}

	return nil, nil
}

const (
	embedProject = "proj-a"
	logoID       = "0199a6a4-7c4e-7000-8000-0000000000a1"
	bannerID     = "0199a6a4-7c4e-7000-8000-0000000000a2"
	foreignID    = "0199a6a4-7c4e-7000-8000-0000000000b1"
	logoToken    = "LogoTokenLogoTokenLogoTokenLogoTokenLogo_-1"
	bannerToken  = "BannerTokenBannerTokenBannerTokenBannerTok2"
	foreignToken = "ForeignTokenForeignTokenForeignTokenForeig3"
	host         = "https://mail.example.test"
)

func embedFixture() *fakeAssets {
	return &fakeAssets{assets: []*tmodel.Asset{
		{ID: logoID, ProjectID: embedProject, Filename: "logo.png", ContentType: "image/png", Size: 3,
			PublicToken: logoToken, Content: base64.StdEncoding.EncodeToString([]byte("PNG"))},
		{ID: bannerID, ProjectID: embedProject, Filename: "banner.jpg", ContentType: "image/jpeg", Size: 4,
			PublicToken: bannerToken, Content: base64.StdEncoding.EncodeToString([]byte("JPEG"))},
		{ID: foreignID, ProjectID: "proj-b", Filename: "theirs.png", ContentType: "image/png", Size: 5,
			PublicToken: foreignToken},
	}}
}

// The project's own images are rewritten to cid: wherever a client
// loads them, once per image however often the HTML names it. Another
// project's image, an unknown token, a link and a URL with a query stay
// as written.
func TestEmbeddingRewritesOnlyTheProjectsOwnImages(t *testing.T) {
	assets := embedFixture()
	svc := &Service{Store: &store.Store{Template: assets}, PublicURL: host + "/"}
	logo := host + "/assets/" + logoToken
	banner := host + "/assets/" + bannerToken
	html := `<img src="` + logo + `" alt="a">` +
		`<img src='` + logo + `'>` +
		`<td style="background-image: url(` + banner + `)">` +
		`<div style="background:url(&quot;` + banner + `&quot;)">` +
		`<img src="` + host + `/assets/` + foreignToken + `">` +
		`<img src="` + host + `/assets/UnknownTokenUnknownTokenUnknownTokenUnkno4">` +
		`<a href="` + logo + `">full size</a>` +
		`<img src="` + logo + `?v=2">`
	req := &SendRequest{HTML: html}
	tpl := &tmodel.Template{ID: "t1", EmbedImages: true}
	if err := svc.AttachTemplateFiles(t.Context(), embedProject, tpl, req); err != nil {
		t.Fatal(err)
	}

	if len(assets.asked) != 1 || len(assets.asked[0]) != 4 {
		t.Fatalf("asked for tokens %v, want the four distinct ones in one lookup", assets.asked)
	}

	if len(req.Attachments) != 2 {
		t.Fatalf("attachments %+v, want one per own image", req.Attachments)
	}

	want := []emailmodel.Attachment{
		{Filename: "logo.png", ContentType: "image/png", Size: 3, ContentID: logoID + "@mailyard", TemplateAssetID: logoID},
		{Filename: "banner.jpg", ContentType: "image/jpeg", Size: 4, ContentID: bannerID + "@mailyard", TemplateAssetID: bannerID},
	}
	for i, w := range want {
		if req.Attachments[i] != w {
			t.Errorf("attachment %d = %+v, want %+v", i, req.Attachments[i], w)
		}
	}

	for _, s := range []string{
		`<img src="cid:` + logoID + `@mailyard" alt="a">`,
		`<img src='cid:` + logoID + `@mailyard'>`,
		`url(cid:` + bannerID + `@mailyard)`,
		`url(&quot;cid:` + bannerID + `@mailyard&quot;)`,
		host + `/assets/` + foreignToken,
		`/assets/UnknownTokenUnknownTokenUnknownTokenUnkno4`,
		`<a href="` + logo + `">`,
		logo + `?v=2`,
	} {
		if !strings.Contains(req.HTML, s) {
			t.Errorf("HTML lacks %q:\n%s", s, req.HTML)
		}
	}

	if strings.Count(req.HTML, "cid:") != 4 {
		t.Errorf("want exactly four cid: references:\n%s", req.HTML)
	}
}

// A template that does not embed keeps its hosted URLs and gets no
// image parts.
func TestATemplateThatDoesNotEmbedKeepsItsURLs(t *testing.T) {
	assets := embedFixture()
	svc := &Service{Store: &store.Store{Template: assets}, PublicURL: host}
	html := `<img src="` + host + `/assets/` + logoToken + `">`
	req := &SendRequest{HTML: html}
	if err := svc.AttachTemplateFiles(t.Context(), embedProject, &tmodel.Template{ID: "t1"}, req); err != nil {
		t.Fatal(err)
	}

	if req.HTML != html || len(req.Attachments) != 0 || len(assets.asked) != 0 {
		t.Errorf("a non-embedding template was changed: %q %+v", req.HTML, req.Attachments)
	}
}

// An embedded image is read through the cache under a key of its own,
// once for many messages, and the message carries it as an inline part
// whose Content-ID is the one the HTML names.
func TestAnEmbeddedImageIsReadOnceAndSentInline(t *testing.T) {
	assets := embedFixture()
	cache := attachcache.New(1<<20, time.Minute)
	ref := emailmodel.Attachment{Filename: "logo.png", ContentType: "image/png", Size: 3,
		ContentID: logoID + "@mailyard", TemplateAssetID: logoID}

	for range 20 {
		e := &emailmodel.Email{
			ID: "e1", ProjectID: embedProject, Sender: "a@b.test", Recipients: []string{"c@d.test"},
			Subject: "s", HTMLBody: `<img src="cid:` + logoID + `@mailyard">`,
			Attachments: []emailmodel.Attachment{ref},
		}
		atts, err := rehydrate(t.Context(), assets, nil, cache, e)
		if err != nil {
			t.Fatal(err)
		}

		raw, err := newMessage(e, atts).Build()
		if err != nil {
			t.Fatal(err)
		}

		for _, s := range []string{"multipart/related", "Content-ID: <" + logoID + "@mailyard>",
			"Content-Disposition: inline", base64.StdEncoding.EncodeToString([]byte("PNG"))} {
			if !bytes.Contains(raw, []byte(s)) {
				t.Fatalf("message lacks %q:\n%s", s, raw)
			}
		}
	}

	if n := assets.lookups.Load(); n != 1 {
		t.Errorf("image read %d times for 20 messages, want once", n)
	}

	// A template attachment sharing the id is a different entry.
	got, err := LoadAttachment(t.Context(), &countingTemplates{att: &tmodel.Attachment{
		ID: logoID, ProjectID: embedProject, Content: base64.StdEncoding.EncodeToString([]byte("PDF")),
	}}, nil, cache, embedProject, &emailmodel.Attachment{Filename: "t.pdf", TemplateAttachmentID: logoID})
	if err != nil || string(got) != "PDF" {
		t.Errorf("template attachment read %q, %v, want its own bytes", got, err)
	}
}

// An image of another project, or one no longer stored, is gone rather
// than read.
func TestAnEmbeddedImageOfAnotherProjectIsGone(t *testing.T) {
	assets := embedFixture()
	_, err := LoadAttachment(t.Context(), assets, nil, nil, embedProject,
		&emailmodel.Attachment{Filename: "theirs.png", TemplateAssetID: foreignID, ContentID: "x@mailyard"})
	if err == nil || !strings.Contains(err.Error(), ErrAttachmentGone.Error()) {
		t.Errorf("read another project's image: %v", err)
	}
}
