// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/attachcache"
	"github.com/yousysadmin/mailyard/internal/core/queue"
	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
	"github.com/yousysadmin/mailyard/internal/core/transport"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
	tmodel "github.com/yousysadmin/mailyard/internal/models/template"
)

// countingTemplates answers one template attachment and counts lookups.
type countingTemplates struct {
	store.TemplateStore
	att     *tmodel.Attachment
	lookups atomic.Int32
}

func (c *countingTemplates) GetAttachmentAny(_ context.Context, projID, id string) (*tmodel.Attachment, error) {
	c.lookups.Add(1)
	if projID != c.att.ProjectID || id != c.att.ID {
		return nil, nil
	}

	return c.att, nil
}

// countingBlobs serves fixed bytes for any key and counts reads.
type countingBlobs struct {
	body  []byte
	reads atomic.Int32
}

func (c *countingBlobs) Put(context.Context, string, io.Reader, string) error { return nil }

func (c *countingBlobs) Delete(context.Context, string) error { return nil }

func (c *countingBlobs) Get(context.Context, string) (io.ReadCloser, error) {
	c.reads.Add(1)

	// Slow enough that concurrent deliveries overlap on the miss.
	time.Sleep(20 * time.Millisecond)

	return io.NopCloser(bytes.NewReader(c.body)), nil
}

// Fifty messages of one campaign referencing the same template
// attachment read it once, and every one carries the whole file.
func TestOneTemplateAttachmentIsReadOncePerCampaign(t *testing.T) {
	body := []byte(strings.Repeat("PDF", 4096))
	templates := &countingTemplates{att: &tmodel.Attachment{
		ID: "0199a6a4-7c4e-7000-8000-000000000001", ProjectID: "proj-a",
		Filename: "terms.pdf", StorageKey: "templates/t/terms.pdf",
	}}
	blobs := &countingBlobs{body: body}

	var mu sync.Mutex
	var delivered []string
	p := failoverProcessor(t, []*ssmodel.Server{srv("only", func(s *ssmodel.Server) { s.Host = "only" })}, &scriptedSend{})
	p.Store.Template = templates
	p.Blob = blobs
	p.Attachments = attachcache.New(1<<20, time.Minute)
	p.send = func(_ context.Context, _ transport.Spec, msg *smtpclient.Message) error {
		mu.Lock()
		defer mu.Unlock()
		delivered = append(delivered, msg.Attachments[0].Content)

		return nil
	}

	const n = 50
	var wg sync.WaitGroup
	outcomes := make(chan queue.Outcome, n)
	for range n {
		wg.Go(func() {
			e := delivery()
			e.Attachments = []emailmodel.Attachment{{
				Filename: "terms.pdf", ContentType: "application/pdf",
				TemplateAttachmentID: templates.att.ID, Size: int64(len(body)),
			}}
			outcomes <- p.Process(t.Context(), e)
		})
	}

	wg.Wait()
	close(outcomes)
	for out := range outcomes {
		if out.Kind != queue.KindDone {
			t.Fatalf("outcome %v: %v", out.Kind, out.Err)
		}
	}

	if got := templates.lookups.Load(); got != 1 {
		t.Errorf("template store lookups %d, want 1", got)
	}

	if got := blobs.reads.Load(); got != 1 {
		t.Errorf("blob reads %d, want 1", got)
	}

	want := base64.StdEncoding.EncodeToString(body)
	if len(delivered) != n {
		t.Fatalf("delivered %d, want %d", len(delivered), n)
	}

	for i, c := range delivered {
		if c != want {
			t.Fatalf("message %d carried %d bytes of base64, want the whole file", i, len(c))
		}
	}
}

// A missing template attachment is reported every time, never kept.
func TestAGoneTemplateAttachmentIsNotCached(t *testing.T) {
	templates := &countingTemplates{att: &tmodel.Attachment{ID: "other", ProjectID: "proj-a"}}
	cache := attachcache.New(1<<20, time.Minute)
	a := &emailmodel.Attachment{Filename: "x.pdf", TemplateAttachmentID: "0199a6a4-7c4e-7000-8000-000000000002"}
	for range 2 {
		if _, err := LoadAttachment(t.Context(), templates, nil, cache, "proj-a", a); !errors.Is(err, ErrAttachmentGone) {
			t.Fatal("want ErrAttachmentGone")
		}
	}

	if got := templates.lookups.Load(); got != 2 {
		t.Fatalf("lookups %d, want 2", got)
	}
}
