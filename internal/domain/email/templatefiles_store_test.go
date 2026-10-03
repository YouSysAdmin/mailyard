// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/blob"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	templatedomain "github.com/yousysadmin/mailyard/internal/domain/template"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
	tmodel "github.com/yousysadmin/mailyard/internal/models/template"
)

const filesProject = "e66e7a4d-9e6c-4884-869a-cf9ffcf22181"

// templateWithFile stores a template holding one attachment, in the
// blob store when bs is set and inline otherwise.
func templateWithFile(t *testing.T, ts *templatedomain.Store, bs blob.Store, body string) (*tmodel.Template, *tmodel.Attachment) {
	t.Helper()
	ctx := t.Context()
	tpl := &tmodel.Template{ID: ids.New(), ProjectID: filesProject, Name: "terms-" + ids.New(), DefaultLanguage: "en"}
	if err := ts.Create(ctx, tpl, nil); err != nil {
		t.Fatal(err)
	}

	a := &tmodel.Attachment{
		ID: ids.New(), ProjectID: filesProject, TemplateID: tpl.ID, Filename: "terms.pdf",
		ContentType: "application/pdf", Size: int64(len(body)),
	}
	if bs != nil {
		a.StorageKey = "templates/" + tpl.ID + "/" + a.ID + "_terms.pdf"
		if err := bs.Put(ctx, a.StorageKey, strings.NewReader(body), "application/pdf"); err != nil {
			t.Fatal(err)
		}
	} else {
		a.Content = base64.StdEncoding.EncodeToString([]byte(body))
	}

	if err := ts.PutAttachment(ctx, a); err != nil {
		t.Fatal(err)
	}

	return tpl, a
}

// A message references a template attachment instead of copying it,
// and still reads the file after the attachment and the whole template
// are deleted. In both storage modes.
func TestAMessageReferencesTheTemplatesAttachment(t *testing.T) {
	for _, mode := range []string{"inline", "fs"} {
		t.Run(mode, func(t *testing.T) {
			es := newClaimStore(t)
			ts := templatedomain.NewStore(es.DB())
			ctx := t.Context()

			var bs blob.Store
			if mode == "fs" {
				var err error
				if bs, err = blob.New(blob.Config{Backend: "fs", FSPath: t.TempDir()}); err != nil {
					t.Fatal(err)
				}
			}

			tpl, att := templateWithFile(t, ts, bs, "PDF BYTES")
			svc := &Service{Store: &store.Store{Template: ts, Email: es}, Blob: bs}
			req := &SendRequest{}
			if err := svc.AttachTemplateFiles(ctx, filesProject, tpl.ID, req); err != nil {
				t.Fatal(err)
			}

			e := &emailmodel.Email{
				ID: ids.New(), ProjectID: filesProject, Sender: "a@b.test", Recipients: []string{"c@d.test"},
				Subject: "s", Status: emailmodel.StatusScheduled, MaxAttempts: 3, Attachments: req.Attachments,
			}
			if err := svc.offloadAttachments(ctx, e); err != nil {
				t.Fatal(err)
			}

			got := e.Attachments[0]
			if got.TemplateAttachmentID != att.ID || got.Content != "" || got.StorageKey != "" || got.Size != 9 {
				t.Fatalf("want a bare reference to %s, got %+v", att.ID, got)
			}

			if err := es.Put(ctx, e); err != nil {
				t.Fatal(err)
			}

			if err := ts.DeleteAttachment(ctx, filesProject, tpl.ID, att.ID); err != nil {
				t.Fatal(err)
			}

			if err := ts.Delete(ctx, filesProject, tpl.ID); err != nil {
				t.Fatal(err)
			}

			stored, err := es.Get(ctx, filesProject, e.ID)
			if err != nil || stored == nil {
				t.Fatalf("read back: %v", err)
			}

			atts, err := rehydrate(ctx, ts, bs, nil, stored)
			if err != nil {
				t.Fatalf("rehydrate after the template went: %v", err)
			}

			raw, _ := base64.StdEncoding.DecodeString(atts[0].Content)
			if !bytes.Equal(raw, []byte("PDF BYTES")) {
				t.Errorf("the message lost its file with the template: %q", raw)
			}
		})
	}
}

// Deleting marks the row and keeps it. Lists and the template's own
// lookups stop seeing it, the message lookup still finds it, and the
// template delete keeps the row detached rather than cascading.
func TestADeletedTemplateAttachmentIsHiddenAndKept(t *testing.T) {
	es := newClaimStore(t)
	ts := templatedomain.NewStore(es.DB())
	ctx := t.Context()

	tpl, att := templateWithFile(t, ts, nil, "x")
	_, other := templateWithFile(t, ts, nil, "y")
	if err := ts.DeleteAttachment(ctx, filesProject, tpl.ID, att.ID); err != nil {
		t.Fatal(err)
	}

	if list, err := ts.ListAttachments(ctx, filesProject, tpl.ID); err != nil || len(list) != 0 {
		t.Errorf("list after delete = %d rows, %v, want none", len(list), err)
	}

	if a, err := ts.GetAttachment(ctx, filesProject, tpl.ID, att.ID); err != nil || a != nil {
		t.Errorf("get after delete = %v, %v, want nil", a, err)
	}

	a, err := ts.GetAttachmentAny(ctx, filesProject, att.ID)
	if err != nil || a == nil || a.DeletedAt == nil || a.Content == "" {
		t.Fatalf("GetAttachmentAny after delete = %+v, %v, want the row with its bytes", a, err)
	}

	if a, err := ts.GetAttachmentAny(ctx, ids.New(), att.ID); err != nil || a != nil {
		t.Errorf("another project read the attachment: %v, %v", a, err)
	}

	if err := ts.Delete(ctx, filesProject, other.TemplateID); err != nil {
		t.Fatal(err)
	}

	kept, err := ts.GetAttachmentAny(ctx, filesProject, other.ID)
	if err != nil || kept == nil {
		t.Fatalf("the template delete took its attachment row: %v", err)
	}

	if kept.DeletedAt == nil || kept.TemplateID != "" {
		t.Errorf("template delete left the attachment live or attached: %+v", kept)
	}

	cands, err := ts.DeletedAttachmentsBefore(ctx, time.Now().Add(time.Minute))
	if err != nil || len(cands) != 2 {
		t.Fatalf("DeletedAttachmentsBefore = %d, %v, want both", len(cands), err)
	}

	if cands[0].Content != "" {
		t.Error("the sweep listing read the bytes it does not need")
	}

	if err := ts.PurgeAttachment(ctx, filesProject, kept.ID); err != nil {
		t.Fatal(err)
	}

	if a, _ := ts.GetAttachmentAny(ctx, filesProject, kept.ID); a != nil {
		t.Error("PurgeAttachment left the row")
	}
}

// A live attachment is never purged, whatever the caller asks.
func TestPurgeLeavesALiveTemplateAttachment(t *testing.T) {
	es := newClaimStore(t)
	ts := templatedomain.NewStore(es.DB())
	_, att := templateWithFile(t, ts, nil, "x")
	if err := ts.PurgeAttachment(t.Context(), filesProject, att.ID); err != nil {
		t.Fatal(err)
	}

	if a, _ := ts.GetAttachmentAny(t.Context(), filesProject, att.ID); a == nil {
		t.Error("a live attachment was purged")
	}
}

// The retention check finds a message referencing the attachment, in
// any status, and only before the bound.
func TestReferencesTemplateAttachment(t *testing.T) {
	es := newClaimStore(t)
	ctx := t.Context()
	attID := ids.New()
	e := &emailmodel.Email{
		ID: ids.New(), ProjectID: filesProject, Sender: "a@b.test", Recipients: []string{"c@d.test"},
		Subject: "s", Status: emailmodel.StatusSent, MaxAttempts: 3,
		Attachments: []emailmodel.Attachment{{Filename: "t.pdf", Size: 1, TemplateAttachmentID: attID}},
	}
	if err := es.Put(ctx, e); err != nil {
		t.Fatal(err)
	}

	later := time.Now().Add(time.Hour)
	if used, err := es.ReferencesTemplateAttachment(ctx, filesProject, attID, later); err != nil || !used {
		t.Errorf("referenced = %v, %v, want true", used, err)
	}

	if used, _ := es.ReferencesTemplateAttachment(ctx, filesProject, ids.New(), later); used {
		t.Error("an unrelated id was reported referenced")
	}

	if used, _ := es.ReferencesTemplateAttachment(ctx, ids.New(), attID, later); used {
		t.Error("another project's message was counted")
	}

	if used, _ := es.ReferencesTemplateAttachment(ctx, filesProject, attID, time.Now().Add(-time.Hour)); used {
		t.Error("a message created after the bound was counted")
	}
}

// A message whose reference outlived its template attachment row is a
// distinct error, which the download endpoints answer as missing.
func TestAReferenceToAPurgedAttachmentIsGone(t *testing.T) {
	es := newClaimStore(t)
	ts := templatedomain.NewStore(es.DB())
	a := &emailmodel.Attachment{Filename: "t.pdf", TemplateAttachmentID: ids.New()}
	_, err := LoadAttachment(t.Context(), ts, nil, nil, filesProject, a)
	if !errors.Is(err, ErrAttachmentGone) {
		t.Errorf("err = %v, want ErrAttachmentGone", err)
	}
}
