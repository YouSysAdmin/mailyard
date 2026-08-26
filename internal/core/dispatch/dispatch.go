// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package dispatch delivers outgoing event webhooks: matching
// subscriptions get a signed JSON POST (X-Mailyard-Signature,
// HMAC-SHA256 over the raw body) with bounded concurrency, linear
// retries, and a per-attempt delivery log. Emit never blocks the
// caller beyond acquiring a slot - delivery runs in the background
// and Close drains it on shutdown.
//
// Destination URLs come from project members, so the HTTP client is
// built by internal/core/safedial: private and reserved addresses are
// refused at dial time and redirects are not followed. See that
// package for why the check cannot live at the URL-parsing layer.
package dispatch

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/metrics"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/safedial"
	"github.com/yousysadmin/mailyard/internal/core/safego"
	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
	whmodel "github.com/yousysadmin/mailyard/internal/models/webhook"
)

// maxConcurrent bounds parallel deliveries across all webhooks.
const maxConcurrent = 8

// Sink is the persistence the dispatcher needs, implemented by the
// webhook domain store.
type Sink interface {
	List(ctx context.Context, projID string) ([]*whmodel.Webhook, error)
	RecordDelivery(ctx context.Context, d *whmodel.Delivery) error

	// Disable is called once, after the LAST attempt at a delivery
	// failed, with the failure that ended it. The hook is out of
	// rotation until somebody re-enables it - retrying a dead endpoint
	// on every event forever parked a goroutine per event and held a
	// delivery slot through every retry sleep, so eight dead hooks
	// stalled every project's deliveries.
	Disable(ctx context.Context, h *whmodel.Webhook, reason string) error
}

// Config tunes delivery. All values must be positive.
type Config struct {
	Timeout     time.Duration
	MaxAttempts int
	RetryDelay  time.Duration

	// AllowPrivateTargets disables the SSRF guard on the HTTP client.
	// See env.WebhookConfig for why it defaults off.
	AllowPrivateTargets bool
}

// Dispatcher fans events out to subscribed webhooks.
type Dispatcher struct {
	sink   Sink
	cfg    Config
	log    *slog.Logger
	client *http.Client
	sem    chan struct{}
	wg     sync.WaitGroup
	quit   chan struct{}

	// mu orders Emit's wg.Go against Close's wg.Wait. Close runs before
	// the HTTP server drains, so handlers still Emit while it waits -
	// and an Add landing while a zero-counter Wait is in progress is
	// the documented WaitGroup misuse, panicking in a goroutine nothing
	// recovers. Under the mutex an Emit either spawns before Wait can
	// see zero or observes closed and refuses.
	mu     sync.Mutex
	closed bool
}

// New builds a Dispatcher over sink. Deliveries run on their own
// goroutines and are bounded by cfg - Close waits for the ones in
// flight.
func New(sink Sink, cfg Config, log *slog.Logger) *Dispatcher {
	return &Dispatcher{
		sink:   sink,
		cfg:    cfg,
		log:    log,
		client: safedial.Client(cfg.Timeout, cfg.AllowPrivateTargets),
		sem:    make(chan struct{}, maxConcurrent),
		quit:   make(chan struct{}),
	}
}

// Emit delivers event to every matching webhook in the project.
// Sender narrows by the webhook's filters, payload becomes the "data"
// field. Runs asynchronously - errors surface in the delivery log,
// never to the caller.
func (d *Dispatcher) Emit(ctx context.Context, projID, event, sender string, payload any) {
	hooks, err := d.sink.List(ctx, projID)
	if err != nil {
		d.log.Error("dispatch: list webhooks", "project_id", projID, "err", err)

		return
	}

	// Through the wire policy in response - a webhook body is the
	// product's JSON the same way an API response is, so an empty list
	// in a payload is [] there too, and a bad byte out of received
	// mail cannot make the delivery fail unsent.
	body, err := response.Marshal(map[string]any{
		"event":     event,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data":      payload,
	})
	if err != nil {
		d.log.Error("dispatch: marshal payload", "event", event, "err", err)

		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		d.log.Warn("dispatch: dispatcher closed, event dropped", "event", event, "project_id", projID)

		return
	}

	for _, h := range hooks {
		if !h.Enabled() || !h.Subscribed(event) || !matchesFilters(h.Filters, sender) {
			continue
		}

		d.wg.Go(func() { d.deliver(h, event, body) })
	}
}

// Close waits for in-flight deliveries - retries included - bounded by
// timeout. Later Emits are refused with a logged drop, and once the
// timeout is spent the stragglers are told to abandon their remaining
// attempts instead of sleeping on into a process whose database is
// about to close.
func (d *Dispatcher) Close(timeout time.Duration) {
	d.mu.Lock()
	already := d.closed
	d.closed = true
	d.mu.Unlock()

	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		d.log.Warn("dispatch: close timed out, dropping in-flight deliveries")

		if !already {
			close(d.quit)
		}
	}
}

// deliver POSTs with retries, recording every attempt. Uses a
// background context: the originating request is long gone.
func (d *Dispatcher) deliver(h *whmodel.Webhook, event string, body []byte) {
	// wg.Go owns the Done, so a panic here still releases the waiter
	// Close is blocked on.
	defer safego.Recover(d.log, "dispatch: deliver", "webhook_id", h.ID, "event", event)
	d.sem <- struct{}{}
	defer func() { <-d.sem }()

	ctx := context.Background()
	var lastFailure string
	for attempt := 1; attempt <= d.cfg.MaxAttempts; attempt++ {
		status, err := d.post(ctx, h, event, body)
		del := &whmodel.Delivery{
			WebhookID:  h.ID,
			ProjectID:  h.ProjectID,
			Event:      event,
			HTTPStatus: status,
			Attempt:    attempt,
		}
		if err == nil && status >= 200 && status < 300 {
			del.Status = whmodel.DeliverySuccess
			metrics.WebhookDeliveries.WithLabelValues(del.Status).Inc()
			if rerr := d.sink.RecordDelivery(ctx, del); rerr != nil {
				d.log.Error("dispatch: record delivery", "webhook_id", h.ID, "err", rerr)
			}

			return
		}

		del.Status = whmodel.DeliveryFailed
		metrics.WebhookDeliveries.WithLabelValues(del.Status).Inc()
		if err != nil {
			del.ErrorMessage = err.Error()
		} else {
			del.ErrorMessage = fmt.Sprintf("endpoint returned status %d", status)
		}

		lastFailure = del.ErrorMessage
		if rerr := d.sink.RecordDelivery(ctx, del); rerr != nil {
			d.log.Error("dispatch: record delivery", "webhook_id", h.ID, "err", rerr)
		}

		if attempt < d.cfg.MaxAttempts {
			select {
			case <-time.After(d.cfg.RetryDelay):
			case <-d.quit:
				d.log.Warn("dispatch: shutting down, abandoning retries",
					"webhook_id", h.ID, "event", event, "attempt", attempt)

				return
			}
		}
	}

	d.log.Warn("dispatch: delivery failed permanently, disabling the webhook",
		"webhook_id", h.ID, "event", event, "attempts", d.cfg.MaxAttempts, "reason", lastFailure)
	if err := d.sink.Disable(ctx, h, lastFailure); err != nil {
		d.log.Error("dispatch: disable webhook", "webhook_id", h.ID, "err", err)
	}
}

func (d *Dispatcher) post(ctx context.Context, h *whmodel.Webhook, event string, body []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.URL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}

	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mailyard-Webhook/1.0")
	req.Header.Set("X-Mailyard-Event", event)
	req.Header.Set(HeaderTimestamp, timestamp)
	req.Header.Set(HeaderSignature, Signature(h.Secret, timestamp, body))
	resp, err := d.client.Do(req)
	if err != nil {
		return 0, err
	}

	defer func() { _ = resp.Body.Close() }()

	return resp.StatusCode, nil
}

// Headers a delivery carries for the receiver to verify it with.
const (
	// HeaderSignature is sha256=hex(hmac(secret, timestamp + "." + body)).
	HeaderSignature = "X-Mailyard-Signature"

	// HeaderTimestamp is the unix time the delivery was signed at, in
	// seconds. It is inside the signed string, so a receiver that
	// refuses a stale timestamp refuses a replayed delivery with it -
	// the body alone would verify forever.
	HeaderTimestamp = "X-Mailyard-Timestamp"
)

// Signature computes the signature header for one delivery.
func Signature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)

	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// matchesFilters reports whether sender passes the webhook's filter
// list (empty list = everything, entries are exact addresses or
// *@domain).
func matchesFilters(filters []string, sender string) bool {
	if len(filters) == 0 || sender == "" {
		return true
	}

	bare := strings.ToLower(smtpclient.EnvelopeAddress(sender))
	at := strings.LastIndex(bare, "@")
	for _, f := range filters {
		f = strings.ToLower(f)
		if f == bare {
			return true
		}

		if strings.HasPrefix(f, "*") && at >= 0 && f[1:] == bare[at:] {
			return true
		}
	}

	return false
}
