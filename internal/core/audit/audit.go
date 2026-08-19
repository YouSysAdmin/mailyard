// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package audit records the operational and security trails.
//
// Writes are asynchronous through a bounded queue. An audit insert
// must never add latency to the request that triggered it, and must
// never fail that request either - a trail is evidence, not a
// precondition. The tradeoff is stated plainly: if the queue fills
// (the database is wedged, or a burst outruns the writer) events are
// dropped and the drop is logged loudly. That is the right failure
// for an operator console. An install that needs guaranteed-durable
// audit before the action completes needs a different design.
package audit

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/clientip"
	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/safego"
	"github.com/yousysadmin/mailyard/internal/core/safetext"
	amodel "github.com/yousysadmin/mailyard/internal/models/audit"
)

// Sink persists one event. Implemented by domain/audit.Store.
type Sink interface {
	Put(ctx context.Context, e *amodel.Event) error
}

// queueSize bounds the backlog. Deep enough to absorb a burst of
// console activity, shallow enough that a wedged database surfaces
// as dropped-event warnings quickly rather than unbounded memory.
const queueSize = 512

// Recorder buffers events and writes them on a background goroutine.
type Recorder struct {
	sink Sink
	log  *slog.Logger

	queue chan *amodel.Event
	quit  chan struct{}
	done  chan struct{}
	once  sync.Once

	// closed flips on Close and turns every later Record into a logged
	// drop. The queue channel itself is NEVER closed: Close runs before
	// the HTTP server drains (the writer must outlive nothing that can
	// still record), so producers - the auditWrites middleware, the
	// auth handlers - are still sending, and a send on a closed channel
	// panics even inside a select. That panic turned a COMMITTED
	// mutation into a 500 on every shutdown with a request in flight.
	closed atomic.Bool

	// watch is notified about every event, after the write.
	//
	// This goroutine is the one place both trails meet: Project and
	// Security both go through Record, so a watcher here sees the whole
	// stream and nothing has to be hooked twice. It is also already off
	// the request path, which is what makes it safe for a watcher to do
	// something as expensive as resolving recipients and sending mail.
	//
	// Set once at boot, before serving. Not a slice of watchers: one
	// consumer exists (alert mail) and a registry would invite the kind
	// of fan-out where a slow watcher stalls the trail.
	watch func(*amodel.Event)

	mu      sync.Mutex
	dropped int
}

// New builds a Recorder and starts its writer. A nil Recorder is a
// valid no-op so call sites never need a guard.
func New(sink Sink, log *slog.Logger) *Recorder {
	r := &Recorder{
		sink:  sink,
		log:   log,
		queue: make(chan *amodel.Event, queueSize),
		quit:  make(chan struct{}),
		done:  make(chan struct{}),
	}
	go r.run()

	return r
}

func (r *Recorder) run() {
	defer close(r.done)
	for {
		select {
		case e := <-r.queue:
			r.write(e)
		case <-r.quit:
			// Drain what is already buffered, then stop. Anything a
			// racing Record lands in the buffer after this loop empties
			// it is lost, which is the drop-on-shutdown the package doc
			// already promises - without the panic.
			for {
				select {
				case e := <-r.queue:
					r.write(e)
				default:
					return
				}
			}
		}
	}
}

// write persists one event and notifies the watcher.
func (r *Recorder) write(e *amodel.Event) {
	// Guard per event, not per loop: the trail must never fail the
	// application, and that promise is worth nothing if one unwritable
	// event kills the process instead of just being dropped.
	func() {
		defer safego.Recover(r.log, "audit: write", "type", e.Type)
		// Each write gets its own short deadline: the request that
		// produced the event is long gone, and a hung insert must not
		// stall the whole trail.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := r.sink.Put(ctx, e); err != nil {
			r.log.Error("audit: write failed", "type", e.Type, "err", err)
		}
	}()
	r.notify(e)
}

// notify hands the event to the watcher, if there is one.
//
// After the write and in its own guarded call. A watcher that panics or
// blocks must not lose the event it was told about - the trail is the
// evidence and the notification is a courtesy, in that order.
func (r *Recorder) notify(e *amodel.Event) {
	if r.watch == nil {
		return
	}

	defer safego.Recover(r.log, "audit: watch", "type", e.Type)
	r.watch(e)
}

// Watch registers the single consumer of the event stream. Call it at
// boot, before anything can record.
func (r *Recorder) Watch(fn func(*amodel.Event)) {
	if r == nil {
		return
	}

	r.watch = fn
}

// Record queues an event. Never blocks, and never panics: after Close
// it logs the drop and returns, because a request finishing its audit
// write mid-shutdown is ordinary, not exceptional.
func (r *Recorder) Record(e *amodel.Event) {
	if r == nil || e == nil {
		return
	}

	if r.closed.Load() {
		r.log.Warn("audit: recorder closed, event dropped", "type", e.Type)

		return
	}

	if e.ID == "" {
		e.ID = ids.New()
	}

	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}

	select {
	case r.queue <- e:
	default:
		r.mu.Lock()
		r.dropped++
		n := r.dropped
		r.mu.Unlock()
		r.log.Error("audit: queue full, event dropped", "type", e.Type, "dropped_total", n)
	}
}

// Project records an operational event.
//
// The request is a parameter so this can stamp where it came from - see
// Stamp. Every caller had that line of its own, twenty-five of them, and
// a new event type that forgot it would have recorded who and what with
// no trace of from where.
func (r *Recorder) Project(c fiber.Ctx, e *amodel.Event) {
	if e == nil {
		return
	}

	Stamp(e, c)
	e.Category = amodel.CategoryProject
	r.Record(e)
}

// Security records an account event.
func (r *Recorder) Security(c fiber.Ctx, e *amodel.Event) {
	if e == nil {
		return
	}

	Stamp(e, c)
	e.Category = amodel.CategorySecurity
	// Security events are about an account, not a tenant.
	e.ProjectID = ""
	r.Record(e)
}

// Dropped reports how many events were lost to a full queue, for the
// admin view.
func (r *Recorder) Dropped() int {
	if r == nil {
		return 0
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	return r.dropped
}

// Close drains the queue and stops the writer. Called during
// shutdown, before the database is closed.
func (r *Recorder) Close(timeout time.Duration) {
	if r == nil {
		return
	}

	r.closed.Store(true)
	r.once.Do(func() { close(r.quit) })
	select {
	case <-r.done:
	case <-time.After(timeout):
		r.log.Warn("audit: writer did not drain in time")
	}
}

// Stamp records where a request came from, on its way into the trail.
//
// Called by Project and Security rather than by their callers, so an
// event cannot be recorded without it. There were twenty-five copies of
// `ClientIP: clientip.From(c)` and the next event type to be added would
// have been the twenty-sixth or, more likely, the first without one.
//
// BOTH FIELDS ARE COPIES, and that is the whole reason this function is
// worth reading twice. The event is queued here and written on the
// writer goroutine, long after fasthttp has returned the request to its
// pool and reused the buffers - so a string that merely POINTS at a
// header reads as whatever arrived later. Reproduced before fixing:
// three requests kept their user agent, and the first event read the
// third request's, while all three read one address. clientip.From
// already answers with a fresh string, so only the header needs the
// clone.
//
// Neither field identifies anybody. The address is where the request
// reached us: an iCloud Private Relay user arrives from a Cloudflare,
// Akamai or Fastly egress shared with strangers, and no header carries
// their own, because the egress proxy is never told it. The user agent
// is forgeable. Together they are the best a request offers, and the
// trail says as much where a person reads it.
func Stamp(e *amodel.Event, c fiber.Ctx) {
	if e == nil || c == nil {
		return
	}

	e.ClientIP = clientip.From(c)
	// Capped, because it is a request header: something has to bound what
	// a caller can write into a column, and no real agent string is
	// anywhere near this long. Through safetext rather than a byte cut:
	// invalid UTF-8 here failed the async INSERT, which meant one bad
	// byte in a header suppressed the sender's own security trail.
	//
	// strings.Clone OUTSIDE the clamp, because Clamp returns the string
	// it was given when there is nothing to clamp - which is every
	// ordinary user agent, so the common case is the unsafe one.
	const maxUserAgent = 400
	e.UserAgent = safetext.Clamp(strings.Clone(c.Get(fiber.HeaderUserAgent)), maxUserAgent)
}
