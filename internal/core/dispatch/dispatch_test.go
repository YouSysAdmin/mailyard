package dispatch

import (
	"context"
	"crypto/hmac"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	whmodel "github.com/yousysadmin/mailyard/internal/models/webhook"
)

type memSink struct {
	mu         sync.Mutex
	hooks      []*whmodel.Webhook
	deliveries []*whmodel.Delivery
}

func (s *memSink) List(context.Context, string) ([]*whmodel.Webhook, error) {
	return s.hooks, nil
}

func (s *memSink) RecordDelivery(_ context.Context, d *whmodel.Delivery) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deliveries = append(s.deliveries, d)

	return nil
}

// testDispatcher opts out of the SSRF guard because every test here
// points at an httptest server, which listens on loopback - exactly
// the class of address the guard exists to refuse. safedial's own
// tests cover the refusal.
func testDispatcher(sink Sink) *Dispatcher {
	return New(sink, Config{
		Timeout:             2 * time.Second,
		MaxAttempts:         3,
		RetryDelay:          10 * time.Millisecond,
		AllowPrivateTargets: true,
	}, slog.New(slog.DiscardHandler))
}

func TestEmitSignsAndDelivers(t *testing.T) {
	var gotBody []byte
	var gotSig, gotEvent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotSig = r.Header.Get("X-Mailyard-Signature")
		gotEvent = r.Header.Get("X-Mailyard-Event")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sink := &memSink{hooks: []*whmodel.Webhook{{
		ID: "6ce38800-147c-4a17-8ecb-7cdaf5557273", ProjectID: "proj", URL: srv.URL, Secret: "topsecret",
		Events: []string{whmodel.EventEmailSent},
	}}}
	d := testDispatcher(sink)
	d.Emit(t.Context(), "proj", whmodel.EventEmailSent, "a@b.co", map[string]any{"id": "9e2f6f11-cdd3-4058-86f2-29f3ad60b06a"})
	d.Close(2 * time.Second)

	if gotEvent != whmodel.EventEmailSent {
		t.Errorf("event header = %q", gotEvent)
	}

	if !hmac.Equal([]byte(gotSig), []byte(Signature("topsecret", gotBody))) {
		t.Errorf("signature mismatch: %q", gotSig)
	}

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.deliveries) != 1 || sink.deliveries[0].Status != whmodel.DeliverySuccess {
		t.Fatalf("deliveries = %+v", sink.deliveries)
	}
}

func TestEmitSkipsUnsubscribedAndFiltered(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sink := &memSink{hooks: []*whmodel.Webhook{
		{ID: "other-event", ProjectID: "proj", URL: srv.URL, Events: []string{whmodel.EventEmailFailed}},
		{ID: "other-sender", ProjectID: "proj", URL: srv.URL, Events: []string{whmodel.EventEmailSent}, Filters: []string{"*@corp.example"}},
	}}
	d := testDispatcher(sink)
	d.Emit(t.Context(), "proj", whmodel.EventEmailSent, "someone@else.example", nil)
	d.Close(2 * time.Second)
	if hits != 0 {
		t.Errorf("expected no deliveries, got %d", hits)
	}
}

func TestEmitRetriesAndLogsFailures(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	sink := &memSink{hooks: []*whmodel.Webhook{{
		ID: "6ce38800-147c-4a17-8ecb-7cdaf5557273", ProjectID: "proj", URL: srv.URL, Events: []string{"*"},
	}}}
	d := testDispatcher(sink)
	d.Emit(t.Context(), "proj", whmodel.EventEmailFailed, "", nil)
	d.Close(5 * time.Second)

	if calls != 3 {
		t.Errorf("calls = %d, want 3 attempts", calls)
	}

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.deliveries) != 3 {
		t.Fatalf("delivery rows = %d, want 3", len(sink.deliveries))
	}

	for i, del := range sink.deliveries {
		if del.Status != whmodel.DeliveryFailed || del.Attempt != i+1 || del.HTTPStatus != 500 {
			t.Errorf("delivery %d = %+v", i, del)
		}
	}
}

func TestFilterMatching(t *testing.T) {
	cases := []struct {
		filters []string
		sender  string
		want    bool
	}{
		{nil, "a@b.co", true},
		{[]string{"a@b.co"}, "a@b.co", true},
		{[]string{"a@b.co"}, "Name <A@B.CO>", true},
		{[]string{"*@b.co"}, "x@b.co", true},
		{[]string{"*@b.co"}, "x@other.co", false},
		{[]string{"a@b.co"}, "x@b.co", false},
	}
	for _, tc := range cases {
		if got := matchesFilters(tc.filters, tc.sender); got != tc.want {
			t.Errorf("matchesFilters(%v, %q) = %v, want %v", tc.filters, tc.sender, got, tc.want)
		}
	}
}
