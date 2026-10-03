// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package webhook

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	coreaudit "github.com/yousysadmin/mailyard/internal/core/audit"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	amodel "github.com/yousysadmin/mailyard/internal/models/audit"
	whmodel "github.com/yousysadmin/mailyard/internal/models/webhook"
)

// onceDisabled reports a change to the first Disable only, the way the
// fenced UPDATE does.
type onceDisabled struct {
	store.WebhookStore
	mu   sync.Mutex
	done bool
}

func (s *onceDisabled) Disable(context.Context, string, string, string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return false, nil
	}

	s.done = true

	return true, nil
}

type auditCount struct {
	mu sync.Mutex
	n  int
}

func (a *auditCount) Put(context.Context, *amodel.Event) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.n++

	return nil
}

// The scheme is http or https exactly, with or without the private
// address guard - a prefix match let httpz:// through.
func TestAWebhookURLIsHTTPOrHTTPS(t *testing.T) {
	cfg := &env.Config{}
	cfg.Webhook.AllowPrivateTargets = true
	h := &Handler{Runtime: &env.Runtime{Config: cfg}}
	app := fiber.New()
	app.Post("/", func(c fiber.Ctx) error {
		return c.SendString(h.refuseTarget(c, string(c.Body())))
	})

	for target, refused := range map[string]bool{
		"https://hooks.example.com/x": false,
		"HTTP://hooks.example.com/x":  false,
		"httpz://hooks.example.com/x": true,
		"httpsx://hooks.example.com":  true,
		"ftp://hooks.example.com":     true,
	} {
		res, err := app.Test(httptest.NewRequest(fiber.MethodPost, "/", strings.NewReader(target)))
		if err != nil {
			t.Fatal(err)
		}

		body, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if got := len(body) > 0; got != refused {
			t.Errorf("%s: refused %v (%q), want %v", target, got, body, refused)
		}
	}
}

// Every delivery in flight on a dead endpoint ends in Disable. One hook
// taken out is one audit event, however many of them get there.
func TestOneDisableIsOneAuditEvent(t *testing.T) {
	sink := &auditCount{}
	rec := coreaudit.New(sink, slog.New(slog.DiscardHandler))
	ds := &DispatchSink{Store: &onceDisabled{}, Audit: rec}

	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			hook := &whmodel.Webhook{ID: "h1", ProjectID: "proj", URL: "https://hooks.example.com"}
			if err := ds.Disable(t.Context(), hook, "endpoint returned status 500"); err != nil {
				t.Error(err)
			}
		})
	}

	wg.Wait()
	rec.Close(2 * time.Second)

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.n != 1 {
		t.Errorf("audit events = %d, want 1", sink.n)
	}
}
