// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/blob"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/queue"
	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
	"github.com/yousysadmin/mailyard/internal/core/transport"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
	tmodel "github.com/yousysadmin/mailyard/internal/models/template"
)

// oneTemplateFile answers the message lookup for a single attachment.
type oneTemplateFile struct {
	store.TemplateStore
	att *tmodel.Attachment
}

func (f *oneTemplateFile) GetAttachmentAny(_ context.Context, projID, id string) (*tmodel.Attachment, error) {
	if f.att.ProjectID != projID || f.att.ID != id {
		return nil, nil
	}

	return f.att, nil
}

// The processor delivers a reference entry with the template
// attachment's bytes, read from the inline column or the blob store.
func TestTheProcessorDeliversAReferencedTemplateFile(t *testing.T) {
	for _, mode := range []string{"inline", "fs"} {
		t.Run(mode, func(t *testing.T) {
			att := &tmodel.Attachment{ID: "4b1c4c1e-0d0e-4f53-9a41-6f0c6c3b4b11", ProjectID: "proj-a", Filename: "terms.pdf"}
			var bs blob.Store
			if mode == "fs" {
				var err error
				if bs, err = blob.New(blob.Config{Backend: "fs", FSPath: t.TempDir()}); err != nil {
					t.Fatal(err)
				}

				att.StorageKey = "templates/t/terms.pdf"
				if err := bs.Put(t.Context(), att.StorageKey, strings.NewReader("PDF BYTES"), "application/pdf"); err != nil {
					t.Fatal(err)
				}
			} else {
				att.Content = base64.StdEncoding.EncodeToString([]byte("PDF BYTES"))
			}

			var sent []smtpclient.Attachment
			p := failoverProcessor(t, []*ssmodel.Server{
				srv("only", func(s *ssmodel.Server) { s.Host = "only" }),
			}, &scriptedSend{})
			p.Store.Template = &oneTemplateFile{att: att}
			p.Blob = bs
			p.send = func(_ context.Context, _ transport.Spec, msg *smtpclient.Message) error {
				sent = msg.Attachments

				return nil
			}

			e := delivery()
			e.Attachments = []emailmodel.Attachment{{
				Filename: "terms.pdf", ContentType: "application/pdf", Size: 9, TemplateAttachmentID: att.ID,
			}}
			if out := p.Process(t.Context(), e); out.Kind != queue.KindDone {
				t.Fatalf("outcome %v, want done: %v", out.Kind, out.Err)
			}

			if len(sent) != 1 {
				t.Fatalf("sent %d attachments, want 1", len(sent))
			}

			raw, _ := base64.StdEncoding.DecodeString(sent[0].Content)
			if string(raw) != "PDF BYTES" || sent[0].Filename != "terms.pdf" {
				t.Errorf("delivered %q as %q, want the template's bytes", raw, sent[0].Filename)
			}

			if e.Attachments[0].Content != "" {
				t.Error("rehydrate wrote the bytes back into the stored row")
			}
		})
	}
}

// A caller cannot point an attachment at stored bytes or make it an
// embedded part. Only the server writes a storage key, a template
// reference, an image reference or a Content-ID.
func TestACallerCannotNameStoredBytes(t *testing.T) {
	got := callerAttachments([]emailmodel.Attachment{{
		Filename: "a.txt", Content: "eA==", StorageKey: "templates/other/secret.pdf",
		TemplateAttachmentID: "4b1c4c1e-0d0e-4f53-9a41-6f0c6c3b4b11",
		TemplateAssetID:      "0199a6a4-7c4e-7000-8000-0000000000aa",
		ContentID:            "0199a6a4-7c4e-7000-8000-0000000000aa@mailyard",
	}})
	if got[0].StorageKey != "" || got[0].TemplateAttachmentID != "" || got[0].Content != "eA==" {
		t.Errorf("caller attachment kept server fields: %+v", got[0])
	}

	if got[0].TemplateAssetID != "" || got[0].ContentID != "" {
		t.Errorf("caller attachment kept an image reference: %+v", got[0])
	}
}

// A reference carries no content, so its recorded size is what counts
// toward the message total.
func TestReferencesCountTowardTheAttachmentTotal(t *testing.T) {
	svc := &Service{Sending: env.SendingConfig{MaxAttachmentSize: 100, MaxTotalAttachmentSize: 100}}
	own := emailmodel.Attachment{Filename: "a.txt", Content: base64.StdEncoding.EncodeToString(make([]byte, 40))}
	ref := emailmodel.Attachment{Filename: "t.pdf", Size: 70, TemplateAttachmentID: "4b1c4c1e-0d0e-4f53-9a41-6f0c6c3b4b11"}
	if err := svc.validateAttachments([]emailmodel.Attachment{own, ref}); err == nil {
		t.Error("40 own bytes plus a 70 byte reference passed a 100 byte total")
	}

	ref.Size = 60
	if err := svc.validateAttachments([]emailmodel.Attachment{own, ref}); err != nil {
		t.Errorf("exactly the total was refused: %v", err)
	}

	img := emailmodel.Attachment{Filename: "logo.png", Size: 30, ContentID: "x@mailyard", TemplateAssetID: "0199a6a4-7c4e-7000-8000-0000000000aa"}
	if err := svc.validateAttachments([]emailmodel.Attachment{own, img}); err != nil {
		t.Errorf("40 own bytes plus a 30 byte image were refused: %v", err)
	}

	img.Size = 61
	if err := svc.validateAttachments([]emailmodel.Attachment{own, img}); err == nil {
		t.Error("an embedded image did not count toward the total")
	}
}

// Purging a message never deletes a template's object, even from a row
// written when messages shared it.
func TestAMessageOwnsOnlyItsOwnKeys(t *testing.T) {
	if ownsKey("templates/t/a_terms.pdf") || ownsKey("") {
		t.Error("a template key or an empty one was claimed by a message")
	}

	if !ownsKey("emails/e/0_terms.pdf") {
		t.Error("the message's own key was not claimed")
	}
}

func TestDecodedLen(t *testing.T) {
	for _, n := range []int{0, 1, 2, 3, 4, 40, 41} {
		if got := decodedLen(base64.StdEncoding.EncodeToString(make([]byte, n))); got != int64(n) {
			t.Errorf("decodedLen of %d bytes = %d", n, got)
		}
	}
}
