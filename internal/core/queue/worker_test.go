package queue

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
)

// memSource is an in-memory Source that mimics the optimistic-claim
// semantics of the SQL implementation.
type memSource struct {
	mu     sync.Mutex
	rows   map[string]*emailmodel.Email
	claims map[string]int // id -> times claimed, to assert claim-once
	final  map[string]string
}

func newMemSource(rows ...*emailmodel.Email) *memSource {
	s := &memSource{rows: map[string]*emailmodel.Email{}, claims: map[string]int{}, final: map[string]string{}}
	for _, r := range rows {
		s.rows[r.ID] = r
	}

	return s
}

func (s *memSource) ClaimDue(_ context.Context, now time.Time, limit int) ([]*emailmodel.Email, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*emailmodel.Email
	for _, r := range s.rows {
		if len(out) >= limit {
			break
		}

		due := r.NextAttemptAt == nil || !r.NextAttemptAt.After(now)
		if (r.Status == emailmodel.StatusQueued || r.Status == emailmodel.StatusScheduled) && due {
			r.Status = emailmodel.StatusProcessing
			r.Attempts++
			s.claims[r.ID]++
			cp := *r
			out = append(out, &cp)
		}
	}

	return out, nil
}

func (s *memSource) Requeue(_ context.Context, id string, _ time.Time, next time.Time, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rows[id]
	r.Status = emailmodel.StatusQueued
	r.NextAttemptAt = &next
	r.ErrorMessage = errMsg

	return nil
}

func (s *memSource) Finalize(_ context.Context, id string, _ time.Time, status, errMsg, deliveredVia string, sentAt *time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rows[id]
	r.Status = status
	r.ErrorMessage = errMsg
	r.SentAt = sentAt
	if deliveredVia != "" {
		r.DeliveredVia = deliveredVia
	}

	s.final[id] = status

	return nil
}

func (s *memSource) RecoverStuck(context.Context, time.Time) (int, error) { return 0, nil }

func (s *memSource) statusOf(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.rows[id].Status
}

type funcProcessor func(job *emailmodel.Email) Outcome

func (f funcProcessor) Process(_ context.Context, job *emailmodel.Email) Outcome { return f(job) }

func testConfig() Config {
	return Config{
		Concurrency:    4,
		PollInterval:   10 * time.Millisecond,
		MaxAttempts:    3,
		RetryBaseDelay: time.Millisecond,
		RetryMaxDelay:  5 * time.Millisecond,
		ClaimTimeout:   time.Minute,
	}
}

func run(t *testing.T, src Source, proc Processor, until func() bool) {
	t.Helper()
	w := NewWorker(src, proc, testConfig(), slog.New(slog.DiscardHandler))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go w.Start(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for !until() {
		if time.Now().After(deadline) {
			w.Stop(time.Second)
			t.Fatal("condition not reached before deadline")
		}

		time.Sleep(5 * time.Millisecond)
	}

	w.Stop(time.Second)
}

func queuedEmail(id string) *emailmodel.Email {
	return &emailmodel.Email{ID: id, Status: emailmodel.StatusQueued, MaxAttempts: 3}
}

func TestWorkerDeliversEachJobOnce(t *testing.T) {
	var rows []*emailmodel.Email
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		rows = append(rows, queuedEmail(id))
	}

	src := newMemSource(rows...)

	var mu sync.Mutex
	processed := map[string]int{}
	proc := funcProcessor(func(job *emailmodel.Email) Outcome {
		mu.Lock()
		processed[job.ID]++
		mu.Unlock()

		return Done()
	})

	run(t, src, proc, func() bool {
		src.mu.Lock()
		defer src.mu.Unlock()

		return len(src.final) == len(rows)
	})

	for id, n := range processed {
		if n != 1 {
			t.Errorf("job %s processed %d times", id, n)
		}
	}

	for _, r := range rows {
		if src.claims[r.ID] != 1 {
			t.Errorf("job %s claimed %d times", r.ID, src.claims[r.ID])
		}

		if src.statusOf(r.ID) != emailmodel.StatusSent {
			t.Errorf("job %s status %s", r.ID, src.statusOf(r.ID))
		}
	}
}

func TestWorkerRetriesThenSucceeds(t *testing.T) {
	src := newMemSource(queuedEmail("x"))
	var attempts int
	var mu sync.Mutex
	proc := funcProcessor(func(job *emailmodel.Email) Outcome {
		mu.Lock()
		attempts++
		n := attempts
		mu.Unlock()
		if n < 3 {
			return Retry(errors.New("transient"))
		}

		return Done()
	})
	run(t, src, proc, func() bool { return src.statusOf("x") == emailmodel.StatusSent })
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestWorkerExhaustsAttempts(t *testing.T) {
	src := newMemSource(queuedEmail("x"))
	proc := funcProcessor(func(*emailmodel.Email) Outcome { return Retry(errors.New("always down")) })
	run(t, src, proc, func() bool { return src.statusOf("x") == emailmodel.StatusFailed })
	src.mu.Lock()
	defer src.mu.Unlock()
	if src.rows["x"].Attempts != 3 {
		t.Errorf("attempts = %d, want 3", src.rows["x"].Attempts)
	}

	if src.rows["x"].ErrorMessage != "always down" {
		t.Errorf("error = %q", src.rows["x"].ErrorMessage)
	}
}

func TestWorkerPermanentFailureSkipsRetry(t *testing.T) {
	src := newMemSource(queuedEmail("x"))
	proc := funcProcessor(func(*emailmodel.Email) Outcome { return Fail(errors.New("550 user unknown")) })
	run(t, src, proc, func() bool { return src.statusOf("x") == emailmodel.StatusFailed })
	if src.rows["x"].Attempts != 1 {
		t.Errorf("attempts = %d, want 1 (no retries on permanent failure)", src.rows["x"].Attempts)
	}
}

func TestWorkerSuppressedOutcome(t *testing.T) {
	src := newMemSource(queuedEmail("x"))
	proc := funcProcessor(func(*emailmodel.Email) Outcome { return Suppressed(errors.New("all recipients suppressed")) })
	run(t, src, proc, func() bool { return src.statusOf("x") == emailmodel.StatusSuppressed })
}

func TestWorkerScheduledWaitsForDueTime(t *testing.T) {
	future := time.Now().UTC().Add(60 * time.Millisecond)
	e := &emailmodel.Email{ID: "s", Status: emailmodel.StatusScheduled, MaxAttempts: 3, NextAttemptAt: &future}
	src := newMemSource(e)
	var deliveredAt time.Time
	var mu sync.Mutex
	proc := funcProcessor(func(*emailmodel.Email) Outcome {
		mu.Lock()
		deliveredAt = time.Now().UTC()
		mu.Unlock()

		return Done()
	})
	run(t, src, proc, func() bool { return src.statusOf("s") == emailmodel.StatusSent })
	if deliveredAt.Before(future) {
		t.Errorf("delivered %s before scheduled time %s", deliveredAt, future)
	}
}

// OnFinal is operator-supplied (the webhook dispatcher and the contact
// tracker hang off it) and runs on the pool goroutine, outside the
// recover that guards Process. A guard on the loop FUNCTION instead of
// the iteration would let one such panic retire that goroutine for
// good - nothing respawns it - so a repeatable panic silently drains
// the pool to zero and delivery stops while the process still looks
// healthy. This pins that the pool survives more panics than it has
// goroutines.
func TestWorkerSurvivesPanicInOnFinal(t *testing.T) {
	cfg := testConfig()
	// More jobs than pool goroutines: if a panic killed the goroutine,
	// the run would stall well before the last one.
	total := cfg.Concurrency * 3
	rows := make([]*emailmodel.Email, 0, total)
	for i := range total {
		rows = append(rows, queuedEmail("panic-"+string(rune('a'+i))))
	}

	src := newMemSource(rows...)

	var mu sync.Mutex
	panicked := 0
	w := NewWorker(src, funcProcessor(func(*emailmodel.Email) Outcome { return Done() }),
		cfg, slog.New(slog.DiscardHandler))
	w.OnFinal = func(*emailmodel.Email, string, string) {
		mu.Lock()
		panicked++
		mu.Unlock()
		panic("hook exploded")
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go w.Start(ctx)
	defer w.Stop(time.Second)

	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := panicked
		mu.Unlock()
		if n >= total {
			break
		}

		if time.Now().After(deadline) {
			t.Fatalf("pool stalled after %d of %d jobs - a panicking hook killed the workers", n, total)
		}

		time.Sleep(5 * time.Millisecond)
	}

	// Every row still reached a terminal status: the panic cost the
	// hook, not the delivery.
	for _, r := range rows {
		if got := src.statusOf(r.ID); got != emailmodel.StatusSent {
			t.Errorf("%s status = %q, want sent", r.ID, got)
		}
	}
}

// Wake must reach the other nodes, WakeLocal must not. The listener
// calls WakeLocal on every notification it receives, so if that path
// rebroadcast, one enqueue would circle the cluster forever with each
// hop generating the next.
func TestWakeBroadcastsAndWakeLocalDoesNot(t *testing.T) {
	w := NewWorker(&memSource{}, nil, Config{Concurrency: 1}, slog.New(slog.DiscardHandler))
	var broadcasts int
	w.Broadcast = func() { broadcasts++ }

	w.Wake()
	if broadcasts != 1 {
		t.Fatalf("Wake produced %d broadcasts, want 1", broadcasts)
	}

	w.WakeLocal()
	if broadcasts != 1 {
		t.Fatalf("WakeLocal produced a broadcast (total %d), want it to stay local", broadcasts)
	}
}

// An api-role node builds a worker so Wake can reach the real ones,
// but never starts it. Stop must return immediately there instead of
// waiting out its timeout on a loop that was never running.
func TestStopReturnsImmediatelyWhenNeverStarted(t *testing.T) {
	w := NewWorker(&memSource{}, nil, Config{Concurrency: 1}, slog.New(slog.DiscardHandler))
	done := make(chan struct{})
	go func() {
		w.Stop(30 * time.Second)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop blocked on a worker that was never started")
	}
}

// The winner of a failover walk is a local in the processor loop, and
// nothing outside it can know which server carried the message. That
// is why it comes back through Outcome - and why the delivery log
// could not answer "which server sent this" until it did.
func TestTheDeliveringServerIsRecorded(t *testing.T) {
	row := queuedEmail("x")
	row.SMTPServerID = "pinned-server"
	src := newMemSource(row)

	proc := funcProcessor(func(*emailmodel.Email) Outcome {
		return DoneVia("server-that-actually-sent-it")
	})
	run(t, src, proc, func() bool { return src.statusOf("x") == emailmodel.StatusSent })

	src.mu.Lock()
	defer src.mu.Unlock()
	got := src.rows["x"]
	if got.DeliveredVia != "server-that-actually-sent-it" {
		t.Errorf("delivered_via = %q, want the server that carried it", got.DeliveredVia)
	}

	// The pinned server is an INPUT to routing and must keep meaning
	// that. Overwriting it would silently re-pin the message to
	// whatever the failover happened to land on.
	if got.SMTPServerID != "pinned-server" {
		t.Errorf("the pinned server was overwritten with %q", got.SMTPServerID)
	}
}

// A failure never left, so there is no server to record.
func TestAFailureRecordsNoDeliveringServer(t *testing.T) {
	src := newMemSource(queuedEmail("x"))
	proc := funcProcessor(func(*emailmodel.Email) Outcome {
		return Fail(errors.New("nope"))
	})
	run(t, src, proc, func() bool { return src.statusOf("x") == emailmodel.StatusFailed })

	src.mu.Lock()
	defer src.mu.Unlock()
	if via := src.rows["x"].DeliveredVia; via != "" {
		t.Errorf("a failed message recorded %q as its delivering server", via)
	}
}
