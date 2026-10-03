// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"bytes"
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/blob"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	templatedomain "github.com/yousysadmin/mailyard/internal/domain/template"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
	tmodel "github.com/yousysadmin/mailyard/internal/models/template"
)

// A message carries its own copy of a template attachment. Sharing the
// template's object meant deleting the attachment, or the template,
// failed mail still queued, and purging the message deleted the
// template's file.
func TestAMessageCopiesTheTemplatesAttachment(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)

	const projID = "e66e7a4d-9e6c-4884-869a-cf9ffcf22181"
	dbtest.Schema(t, db, `
        INSERT INTO projects (id, name, slug, default_language, created_at)
        VALUES ('`+projID+`', 'Test', 'test', 'en', now())`)

	bs, err := blob.New(blob.Config{Backend: "fs", FSPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	ctx := t.Context()
	ts := templatedomain.NewStore(db)
	tpl := &tmodel.Template{ID: ids.New(), ProjectID: projID, Name: "terms", DefaultLanguage: "en"}
	if err := ts.Create(ctx, tpl, nil); err != nil {
		t.Fatal(err)
	}

	key := "templates/" + tpl.ID + "/terms.pdf"
	if err := bs.Put(ctx, key, strings.NewReader("PDF BYTES"), "application/pdf"); err != nil {
		t.Fatal(err)
	}

	if err := ts.PutAttachment(ctx, &tmodel.Attachment{
		ID: ids.New(), ProjectID: projID, TemplateID: tpl.ID, Filename: "terms.pdf",
		ContentType: "application/pdf", Size: 9, StorageKey: key,
	}); err != nil {
		t.Fatal(err)
	}

	svc := &Service{Store: &store.Store{Template: ts}, Blob: bs}
	req := &SendRequest{}
	if err := svc.AttachTemplateFiles(ctx, projID, tpl.ID, req); err != nil {
		t.Fatal(err)
	}

	e := &emailmodel.Email{ID: ids.New(), Attachments: req.Attachments}
	if err := svc.offloadAttachments(ctx, e); err != nil {
		t.Fatal(err)
	}

	got := e.Attachments[0]
	if got.StorageKey == key || !strings.HasPrefix(got.StorageKey, "emails/"+e.ID+"/") {
		t.Fatalf("the message shares or lacks a key: %q", got.StorageKey)
	}

	if err := bs.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}

	raw, err := LoadAttachment(ctx, bs, &got)
	if err != nil || !bytes.Equal(raw, []byte("PDF BYTES")) {
		t.Errorf("the message lost its file with the template's: %q %v", raw, err)
	}
}
