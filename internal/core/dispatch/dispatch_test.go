package dispatch

import (
	"context"
	"crypto/hmac"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	whmodel "github.com/yousysadmin/mailyard/internal/models/webhook"
)

type memSink struct {
	mu         sync.Mutex
	hooks      []*whmodel.Webhook
	deliveries []*whmodel.Delivery
	disabled   int
}

// edit changes the stored hook under the lock, the way a console edit
// lands while a delivery waits.
func (s *memSink) edit(id string, fn func(*whmodel.Webhook)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, h := range s.hooks {
		if h.ID == id {
			if fn == nil {
				s.hooks = append(s.hooks[:i], s.hooks[i+1:]...)

				return
			}

			fn(h)
		}
	}
}

func (s *memSink) List(context.Context, string) ([]*whmodel.Webhook, error) {
	return s.hooks, nil
}

func (s *memSink) Get(_ context.Context, _, id string) (*whmodel.Webhook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, h := range s.hooks {
		if h.ID == id {
			c := *h

			return &c, nil
		}
	}

	return nil, nil
}

func (s *memSink) Disable(_ context.Context, h *whmodel.Webhook, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.disabled++
	for _, stored := range s.hooks {
		if stored.ID == h.ID {
			stored.DisabledAt, stored.DisabledReason = new(time.Now()), reason
		}
	}

	return nil
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
	var gotSig, gotTS, gotEvent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotSig = r.Header.Get(HeaderSignature)
		gotTS = r.Header.Get(HeaderTimestamp)
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

	// The timestamp is inside the signed string, so the signature
	// only verifies with the one that was sent.
	ts, err := strconv.ParseInt(gotTS, 10, 64)
	if err != nil || time.Since(time.Unix(ts, 0)) > time.Minute {
		t.Errorf("timestamp header %q is not a recent unix time", gotTS)
	}

	if !hmac.Equal([]byte(gotSig), []byte(Signature("topsecret", gotTS, gotBody))) {
		t.Errorf("signature mismatch: %q", gotSig)
	}

	if hmac.Equal([]byte(gotSig), []byte(Signature("topsecret", "0", gotBody))) {
		t.Error("the signature verifies under another timestamp, so a replay cannot be aged out")
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

// A project holds at most maxPerProject slots, so another project's
// delivery goes through while this one's endpoint hangs.
func TestATarpitProjectDoesNotStallTheOthers(t *testing.T) {
	release := make(chan struct{})
	tarpit := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer tarpit.Close()
	defer close(release)

	delivered := make(chan struct{}, 16)
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		delivered <- struct{}{}
	}))
	defer healthy.Close()

	// A sink whose List answers by project, so the two projects hold
	// different hooks.
	sink := &projectSink{hooks: map[string][]*whmodel.Webhook{
		"slow": {{ID: "36e3a1c2-0a5b-4b0a-9d1e-6f1e0c9b3a11", ProjectID: "slow", URL: tarpit.URL, Secret: "s", Events: []string{whmodel.EventEmailSent}}},
		"fast": {{ID: "7a9f0d21-2b4c-4c1d-8e2f-1a2b3c4d5e66", ProjectID: "fast", URL: healthy.URL, Secret: "s", Events: []string{whmodel.EventEmailSent}}},
	}}
	d := New(sink, Config{
		Timeout: 30 * time.Second, MaxAttempts: 1, RetryDelay: time.Millisecond, AllowPrivateTargets: true,
	}, slog.New(slog.DiscardHandler))

	// More tarpit deliveries than the global slot count.
	for range maxConcurrent + 2 {
		d.Emit(t.Context(), "slow", whmodel.EventEmailSent, "a@b.co", map[string]any{})
	}

	time.Sleep(50 * time.Millisecond)
	d.Emit(t.Context(), "fast", whmodel.EventEmailSent, "a@b.co", map[string]any{})
	select {
	case <-delivered:
	case <-time.After(3 * time.Second):
		t.Fatal("the healthy project's delivery waited behind the tarpit")
	}
}

type projectSink struct {
	memSink
	hooks map[string][]*whmodel.Webhook
}

func (s *projectSink) List(_ context.Context, projID string) ([]*whmodel.Webhook, error) {
	return s.hooks[projID], nil
}

func (s *projectSink) Get(_ context.Context, projID, id string) (*whmodel.Webhook, error) {
	for _, h := range s.hooks[projID] {
		if h.ID == id {
			return h, nil
		}
	}

	return nil, nil
}

// panicTransport stands in for a network stack that panics mid-post.
type panicTransport struct{}

func (panicTransport) RoundTrip(*http.Request) (*http.Response, error) { panic("boom") }

// A delivery that panics inside the post returns its slots. Held
// inline rather than deferred, two such panics left a project's two
// slots taken for good and its webhooks never delivered again.
func TestAPanickingPostReturnsItsSlots(t *testing.T) {
	sink := &memSink{hooks: []*whmodel.Webhook{{
		ID: "6ce38800-147c-4a17-8ecb-7cdaf5557273", ProjectID: "proj", URL: "http://127.0.0.1:9/x", Secret: "s",
		Events: []string{whmodel.EventEmailSent},
	}}}
	d := testDispatcher(sink)
	d.client.Transport = panicTransport{}
	for range 3 {
		d.Emit(t.Context(), "proj", whmodel.EventEmailSent, "a@b.co", map[string]any{"id": "x"})
	}

	d.Close(2 * time.Second)

	if got := len(d.sem); got != 0 {
		t.Errorf("%d global slots still held after the panics", got)
	}

	if got := len(d.slot("proj")); got != 0 {
		t.Errorf("%d project slots still held after the panics", got)
	}
}

// A delivery re-reads its hook before every attempt: a retry after the
// URL was edited goes to the new one, and a hook deleted while the
// delivery waited ends it with no further attempt and no disable.
func TestARetryFollowsTheHookAsItIsNow(t *testing.T) {
	var mu sync.Mutex
	hitsA, hitsB := 0, 0
	sink := &memSink{}
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		hitsB++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer b.Close()

	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		hitsA++
		mu.Unlock()
		sink.edit("h1", func(h *whmodel.Webhook) { h.URL = b.URL })
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer a.Close()

	sink.hooks = []*whmodel.Webhook{{ID: "h1", ProjectID: "proj", URL: a.URL, Events: []string{whmodel.EventEmailSent}}}
	d := testDispatcher(sink)
	d.Emit(t.Context(), "proj", whmodel.EventEmailSent, "", nil)
	d.Close(2 * time.Second)

	mu.Lock()
	defer mu.Unlock()
	if hitsA != 1 || hitsB != 1 {
		t.Errorf("hits: old url %d, new url %d, want 1 and 1", hitsA, hitsB)
	}
}

func TestADeletedHookStopsRetrying(t *testing.T) {
	sink := &memSink{}
	var mu sync.Mutex
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		sink.edit("h1", nil)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	sink.hooks = []*whmodel.Webhook{{ID: "h1", ProjectID: "proj", URL: srv.URL, Events: []string{whmodel.EventEmailSent}}}
	d := testDispatcher(sink)
	d.Emit(t.Context(), "proj", whmodel.EventEmailSent, "", nil)
	d.Close(2 * time.Second)

	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Errorf("attempts = %d, want 1 - the hook was deleted after the first", hits)
	}

	if sink.disabled != 0 {
		t.Errorf("Disable called %d times for a deleted hook", sink.disabled)
	}
}

func TestADisabledHookStopsRetrying(t *testing.T) {
	sink := &memSink{}
	var mu sync.Mutex
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		sink.edit("h1", func(h *whmodel.Webhook) { h.DisabledAt = new(time.Now()) })
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	sink.hooks = []*whmodel.Webhook{{ID: "h1", ProjectID: "proj", URL: srv.URL, Events: []string{whmodel.EventEmailSent}}}
	d := testDispatcher(sink)
	d.Emit(t.Context(), "proj", whmodel.EventEmailSent, "", nil)
	d.Close(2 * time.Second)

	mu.Lock()
	defer mu.Unlock()
	if hits != 1 || sink.disabled != 0 {
		t.Errorf("attempts %d, disables %d, want 1 and 0", hits, sink.disabled)
	}
}
