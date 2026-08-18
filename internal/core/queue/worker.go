// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package queue

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/safego"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
)

// Config sizes the worker pool and its retry policy. All fields must
// be positive - serve.go fills them from the validated worker
// config section.
type Config struct {
	Concurrency    int
	PollInterval   time.Duration
	MaxAttempts    int
	RetryBaseDelay time.Duration
	RetryMaxDelay  time.Duration
	ClaimTimeout   time.Duration
}

// Worker drains the email queue: one poll loop feeding Concurrency
// delivery goroutines. Start once, Stop before the database closes.
//
// OnFinal, when set, is invoked after a row reaches a terminal status
// (sent, failed, suppressed) - the webhook dispatcher hangs off it.
// Set it before Start.
type Worker struct {
	src     Source
	proc    Processor
	cfg     Config
	log     *slog.Logger
	OnFinal func(job *emailmodel.Email, status, errMsg string)

	// Broadcast, when set, carries a wake to the other nodes. Set it
	// before Start. See Wake.
	Broadcast func()

	wake    chan struct{}
	jobs    chan *emailmodel.Email
	stop    chan struct{}
	wg      sync.WaitGroup
	once    sync.Once
	started atomic.Bool
}

// NewWorker builds a Worker.
func NewWorker(src Source, proc Processor, cfg Config, log *slog.Logger) *Worker {
	return &Worker{
		src:  src,
		proc: proc,
		cfg:  cfg,
		log:  log,
		wake: make(chan struct{}, 1),
		jobs: make(chan *emailmodel.Email),
		stop: make(chan struct{}),
	}
}

// Wake nudges every node so a just-enqueued email is picked up
// immediately instead of on the next tick. Non-blocking.
//
// This is what the send path calls. The node that accepted an email
// is frequently not the node that will deliver it, so a purely local
// wake would leave the actual worker waiting out its poll interval.
func (w *Worker) Wake() {
	w.WakeLocal()
	if w.Broadcast != nil {
		w.Broadcast()
	}
}

// WakeLocal nudges this process only.
//
// The cross-node listener calls this rather than Wake, and the
// distinction is load-bearing: Wake would re-broadcast what it just
// received, so one enqueue would ping-pong between nodes forever,
// each hop generating the next.
func (w *Worker) WakeLocal() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Start launches the pool and blocks in the poll loop until Stop is
// called or ctx is cancelled. Run it in a goroutine.
func (w *Worker) Start(ctx context.Context) {
	// The pool outlives ctx on purpose. Cancelling ctx is how serve.go
	// stops the SCHEDULING - this poll loop, cron, the listener - but a
	// delivery whose DATA the remote has already accepted must still be
	// able to Finalize, or the row stays processing and crash recovery
	// re-sends a message the recipient already has. So the delivery leg
	// runs on a context that keeps ctx's values and drops its
	// cancellation, and the drain is bounded by Stop's timeout instead.
	flight := context.WithoutCancel(ctx)
	for range w.cfg.Concurrency {
		w.wg.Go(func() { w.deliver(flight) })
	}

	// After the Adds above, so a Stop racing this Start cannot begin
	// wg.Wait between started turning true and the first Add - that
	// ordering is the documented WaitGroup misuse and panics.
	w.started.Store(true)

	w.log.Info("queue: worker started",
		"concurrency", w.cfg.Concurrency, "poll_interval", w.cfg.PollInterval.String())

	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()
	for {
		w.pollOnce(ctx)
		select {
		case <-ticker.C:
		case <-w.wake:
		case <-w.stop:
			close(w.jobs)

			return
		case <-ctx.Done():
			close(w.jobs)

			return
		}
	}
}

// Stop shuts the pool down: the poll loop exits, in-flight deliveries
// finish (bounded by timeout), queued-but-unclaimed rows stay in the
// table for the next start.
//
// A no-op on a worker that was never started - an api-role node
// constructs one so Wake can reach the real workers, and reporting
// that it stopped would put a line in the log for something that
// never ran.
func (w *Worker) Stop(timeout time.Duration) {
	w.once.Do(func() { close(w.stop) })
	if !w.started.Load() {
		return
	}

	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		w.log.Info("queue: worker stopped")
	case <-time.After(timeout):
		w.log.Warn("queue: worker stop timed out, in-flight rows will be crash-recovered on next start")
	}
}

// pollOnce recovers stuck rows, claims due work, and hands it to the
// pool. Dispatch blocks when every worker is busy, which is exactly
// the backpressure we want - the claim batch stays small.
func (w *Worker) pollOnce(ctx context.Context) {
	now := time.Now().UTC()

	if n, err := w.src.RecoverStuck(ctx, now.Add(-w.cfg.ClaimTimeout)); err != nil {
		w.log.Error("queue: recover stuck", "err", err)
	} else if n > 0 {
		w.log.Warn("queue: recovered stuck emails", "count", n)
	}

	claimed, err := w.src.ClaimDue(ctx, now, w.cfg.Concurrency*2)
	if err != nil {
		w.log.Error("queue: claim due", "err", err)

		return
	}

	for _, job := range claimed {
		select {
		case w.jobs <- job:
		case <-w.stop:
			// Shutting down mid-batch: the claimed-but-undelivered
			// rows sit in processing and get crash-recovered.
			return
		case <-ctx.Done():
			return
		}
	}
}

// deliver is one pool goroutine: process a job, route the outcome.
func (w *Worker) deliver(ctx context.Context) {
	for job := range w.jobs {
		w.deliverOne(ctx, job)
	}
}

// deliverOne handles a single job with the guard inside the loop, so a
// panic costs one email rather than the goroutine.
//
// The distinction matters more than it looks. process has its own
// recover and turns a panic into a failed outcome, but finish can
// panic too - it writes to the store and calls OnFinal, which is the
// webhook dispatcher and the contact tracker. A guard on the loop
// function would let such a panic unwind past the range, retiring that
// pool goroutine for good: nothing respawns it, so the pool silently
// shrinks and, after Concurrency panics, delivery stops entirely with
// the process still up and reporting healthy.
func (w *Worker) deliverOne(ctx context.Context, job *emailmodel.Email) {
	defer safego.Recover(w.log, "queue: finish", "email_id", job.ID, "project_id", job.ProjectID)
	w.finish(ctx, job, w.process(ctx, job))
}

// process runs the delivery leg, turning a panic into a permanent
// failure for that one email.
//
// KindFail rather than KindRetry on purpose: a panic is deterministic
// far more often than not, so retrying re-panics on the same row.
// Leaving it unhandled would be worse still - the row stays in
// processing, RecoverStuck requeues it after the claim timeout, and
// the message panics the process again on a loop.
func (w *Worker) process(ctx context.Context, job *emailmodel.Email) (out Outcome) {
	defer func() {
		if r := recover(); r != nil {
			safego.Report(w.log, "queue: process", r, "email_id", job.ID, "project_id", job.ProjectID)
			out = Outcome{Kind: KindFail, Err: fmt.Errorf("delivery panicked: %v", r)}
		}
	}()

	return w.proc.Process(ctx, job)
}

func (w *Worker) finish(ctx context.Context, job *emailmodel.Email, out Outcome) {
	errMsg := ""
	if out.Err != nil {
		errMsg = out.Err.Error()
	}

	switch out.Kind {
	case KindDone:
		now := time.Now().UTC()
		if err := w.src.Finalize(ctx, job.ID, job.CreatedAt, emailmodel.StatusSent, "", out.ServerID, &now); err != nil {
			w.log.Error("queue: finalize sent", "email_id", job.ID, "err", err)

			return
		}

		job.SentAt = &now
		w.log.Info("queue: sent", "email_id", job.ID, "project_id", job.ProjectID, "attempts", job.Attempts)
		w.notify(job, emailmodel.StatusSent, "")
	case KindRetry:
		maxi := job.MaxAttempts
		if maxi <= 0 {
			maxi = w.cfg.MaxAttempts
		}

		if job.Attempts >= maxi {
			if err := w.src.Finalize(ctx, job.ID, job.CreatedAt, emailmodel.StatusFailed, errMsg, "", nil); err != nil {
				w.log.Error("queue: finalize failed", "email_id", job.ID, "err", err)

				return
			}

			w.log.Warn("queue: attempts exhausted", "email_id", job.ID, "attempts", job.Attempts, "err", errMsg)
			w.notify(job, emailmodel.StatusFailed, errMsg)

			return
		}

		next := time.Now().UTC().Add(w.backoff(job.Attempts))
		if err := w.src.Requeue(ctx, job.ID, job.CreatedAt, next, errMsg); err != nil {
			w.log.Error("queue: requeue", "email_id", job.ID, "err", err)

			return
		}

		w.log.Info("queue: retry scheduled", "email_id", job.ID, "attempts", job.Attempts, "next_attempt_at", next, "err", errMsg)
	case KindSuppressed:
		if err := w.src.Finalize(ctx, job.ID, job.CreatedAt, emailmodel.StatusSuppressed, errMsg, "", nil); err != nil {
			w.log.Error("queue: finalize suppressed", "email_id", job.ID, "err", err)

			return
		}

		w.notify(job, emailmodel.StatusSuppressed, errMsg)
	default: // KindFail
		if err := w.src.Finalize(ctx, job.ID, job.CreatedAt, emailmodel.StatusFailed, errMsg, "", nil); err != nil {
			w.log.Error("queue: finalize failed", "email_id", job.ID, "err", err)

			return
		}

		w.log.Warn("queue: permanent failure", "email_id", job.ID, "err", errMsg)
		w.notify(job, emailmodel.StatusFailed, errMsg)
	}
}

func (w *Worker) notify(job *emailmodel.Email, status, errMsg string) {
	if w.OnFinal != nil {
		w.OnFinal(job, status, errMsg)
	}
}

// backoff computes the delay before the next attempt:
// base * 2^(attempts-1), capped, with up to 20 percent jitter so a
// burst of failures does not re-arrive as a thundering herd.
func (w *Worker) backoff(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}

	d := w.cfg.RetryBaseDelay << (attempts - 1)
	if d > w.cfg.RetryMaxDelay || d <= 0 {
		d = w.cfg.RetryMaxDelay
	}

	jitter := time.Duration(rand.Int64N(int64(d)/5 + 1))

	return d + jitter
}
