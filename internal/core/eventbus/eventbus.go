// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package eventbus is the in-process fan-out behind the server-sent
// event stream.
//
// In process, deliberately. The alternative is a shared broker, and
// Mailyard ships as a single binary with no external services - the
// same reason the delivery queue lives in the database rather than
// Redis. The honest cost is that a subscriber only sees events raised
// by the node it is connected to. On a multi-node deployment a live
// stream is therefore partial, exactly like the per-process rate
// limits, and the durable record (the email log, the notifications
// table) remains the authority. A reader who reloads sees everything.
//
// Nothing depends on delivery: an event that cannot be handed to a
// slow subscriber is dropped rather than buffered without bound. A
// live view falling behind is a cosmetic problem, memory growth
// driven by a stalled HTTP client is not.
package eventbus

import (
	"sync"
	"sync/atomic"
	"time"
)

// Event types. The set is closed so the console can switch on it.
const (
	TypeEmailQueued      = "email.queued"
	TypeEmailSent        = "email.sent"
	TypeEmailFailed      = "email.failed"
	TypeInboundReceived  = "email.inbound.received"
	TypeNotification     = "notification.created"
	TypeCampaignProgress = "campaign.progress"
)

// Event is one thing that happened in a project.
type Event struct {
	// Type is one of the constants above.
	Type string `json:"type"`

	// ProjectID scopes delivery. An event is only ever handed to
	// subscribers of its own project - the bus is the enforcement
	// point, so a handler cannot leak another tenant's activity by
	// forgetting a filter.
	ProjectID string `json:"-"`

	// Data is the payload the browser receives, already shaped for
	// display. Keep it small: this is a live ticker, not an API.
	Data any `json:"data,omitempty"`

	// At is when it happened.
	At time.Time `json:"at"`
}

// buffer is how many events a single subscriber may fall behind by
// before its events start being dropped. Small on purpose: a browser
// that cannot keep up with sixty-four events has stopped reading, and
// what it needs then is a reload, not a backlog.
const buffer = 64

// Subscription is one live listener.
type Subscription struct {
	// C delivers events. Closed when the subscription is cancelled.
	C <-chan Event

	bus       *Bus
	id        uint64
	projectID string
	ch        chan Event
}

// Close cancels the subscription and releases its slot. Safe to call
// more than once.
func (s *Subscription) Close() {
	if s == nil || s.bus == nil {
		return
	}

	s.bus.unsubscribe(s)
}

// Bus fans events out to the subscribers of one project.
type Bus struct {
	mu     sync.RWMutex
	nextID uint64

	// subs is keyed by project so a publish walks only the listeners
	// that could receive it.
	subs map[string]map[uint64]*Subscription

	// dropped counts events discarded because a subscriber was full,
	// so the condition is observable rather than silent. Atomic
	// because Publish increments it while holding only a read lock -
	// several publishers run concurrently there by design.
	dropped atomic.Uint64

	// closed is set by Close and never cleared. A bus is closed once,
	// during shutdown.
	closed bool
}

// New builds an empty Bus. It is in-process, so on a multi-node
// deployment a subscriber only sees what this node published.
func New() *Bus {
	return &Bus{subs: map[string]map[uint64]*Subscription{}}
}

// Subscribe returns a subscription to one project's events.
//
// After Close the returned subscription is already finished, so a
// request that arrives during shutdown ends immediately instead of
// waiting for a heartbeat that will never come.
func (b *Bus) Subscribe(projectID string) *Subscription {
	ch := make(chan Event, buffer)
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		close(ch)

		return &Subscription{C: ch, bus: b, projectID: projectID, ch: ch}
	}

	b.nextID++
	sub := &Subscription{
		C:         ch,
		bus:       b,
		id:        b.nextID,
		projectID: projectID,
		ch:        ch,
	}
	if b.subs[projectID] == nil {
		b.subs[projectID] = map[uint64]*Subscription{}
	}

	b.subs[projectID][sub.id] = sub
	b.mu.Unlock()

	return sub
}

func (b *Bus) unsubscribe(s *Subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()
	m := b.subs[s.projectID]
	if m == nil {
		return
	}

	if _, ok := m[s.id]; !ok {
		return // already closed
	}

	delete(m, s.id)
	if len(m) == 0 {
		delete(b.subs, s.projectID)
	}

	close(s.ch)
}

// Publish hands an event to every subscriber of its project.
//
// Never blocks. A subscriber whose buffer is full misses the event -
// see the package comment. Nil-safe so call sites do not have to
// check whether the bus was wired.
func (b *Bus) Publish(e Event) {
	if b == nil || e.ProjectID == "" {
		return
	}

	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, sub := range b.subs[e.ProjectID] {
		select {
		case sub.ch <- e:
		default:
			b.dropped.Add(1)
		}
	}
}

// Close ends every live subscription.
//
// This is how an in-flight SSE handler learns the process is going
// down. Without it the stream loop only ends when the CLIENT hangs up
// or the 30 minute recycle fires, and because fasthttp counts a
// streaming response as an active connection, app.Shutdown waits for
// it - one idle console tab was enough to hang shutdown indefinitely.
//
// Safe to call more than once. Publish after Close is a no-op because
// there is nothing left subscribed.
func (b *Bus) Close() {
	if b == nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}

	b.closed = true
	for projID, m := range b.subs {
		for _, sub := range m {
			close(sub.ch)
		}

		delete(b.subs, projID)
	}
}

// Stats reports live subscriber and drop counts, for the admin view
// and for tests.
func (b *Bus) Stats() (subscribers int, projects int, dropped uint64) {
	if b == nil {
		return 0, 0, 0
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, m := range b.subs {
		subscribers += len(m)
	}

	return subscribers, len(b.subs), b.dropped.Load()
}
