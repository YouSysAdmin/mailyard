// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package iplimit is a per-IP fixed-window counter for the SMTP
// listeners.
//
// It lives in core because three listeners on two different sides of the
// deployment need it: submission, the MX, and the MX a relay node runs.
// The node one is why it moved here - relayagent must not import a
// domain package, and a private copy would be the same algorithm
// maintained twice.
//
// IN PROCESS, so a deployment running several nodes multiplies every
// budget by the node count. That is the accepted cost of a control with
// no shared state: this exists to shed a client hammering one listener
// before it can spend auth or parsing time, not to meter fairly across a
// cluster.
package iplimit

import (
	"sync"
	"time"
)

// Limiter counts connections per remote IP and refuses the ones over
// budget.
//
// Each address gets its OWN window, starting when it is first seen
// rather than on a boundary shared by everybody. Shared boundaries are
// cheaper - the window is an integer division of the clock and expiry is
// an integer compare - but they are also predictable, and a client that
// knows when the window turns over can send its whole budget twice
// across the seam.
type Limiter struct {
	mu     sync.Mutex
	budget int
	window time.Duration

	// One per address currently inside its window. Addresses whose
	// window has passed stay here until a sweep removes them, so this
	// map is a superset of what is actually being limited.
	seen map[string]*window

	sweptAt time.Time
}

type window struct {
	count int
	until time.Time
}

// New returns a limiter allowing budget connections per window per
// address. A budget of zero or less disables it, which is how a listener
// with no configured limit is expressed.
func New(budget int, per time.Duration) *Limiter {
	return &Limiter{
		budget: budget,
		window: per,
		seen:   make(map[string]*window),
	}
}

// off reports a limiter that is not limiting. A nil receiver counts,
// because a listener with no limiter configured calls these methods on
// one.
func (l *Limiter) off() bool {
	return l == nil || l.budget <= 0
}

// Allow reports whether this address is under its budget, and charges it
// for the call.
//
// One method for both because on an SMTP listener the connection IS the
// cost: by the time this is asked, the work worth refusing has already
// been offered.
func (l *Limiter) Allow(ip string) bool {
	if l.off() {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	l.sweepLocked(now)

	live := l.liveLocked(ip, now)
	if live == nil {
		l.seen[ip] = &window{count: 1, until: now.Add(l.window)}

		return true
	}

	if live.count >= l.budget {
		return false
	}

	live.count++

	return true
}

// Exceeded reports whether this address has already spent its budget,
// without charging it.
//
// The two halves are separate because an HTTP gate needs them apart: it
// must refuse a caller that has burnt its budget on failed credentials
// while leaving the honest request it is currently holding uncharged. As
// one call, every successful request would count against the same
// budget and a busy integration would throttle itself.
func (l *Limiter) Exceeded(ip string) bool {
	if l.off() {
		return false
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	live := l.liveLocked(ip, time.Now())

	return live != nil && live.count >= l.budget
}

// liveLocked answers the address's current window, or nil when it has
// none - never seen, or seen long enough ago that the window has passed.
// Both callers need exactly this question and asked it separately.
func (l *Limiter) liveLocked(ip string, now time.Time) *window {
	w, ok := l.seen[ip]
	if !ok || now.After(w.until) {
		return nil
	}

	return w
}

// sweepLocked drops addresses whose window has passed.
//
// At most once per window, because the alternative is walking the whole
// map on every connection - which is the opposite of what a limiter
// under load should do. Entries therefore outlive their window by up to
// one more, which costs a little memory and nothing in correctness:
// liveLocked checks the time, not the presence.
func (l *Limiter) sweepLocked(now time.Time) {
	if now.Sub(l.sweptAt) <= l.window {
		return
	}

	for ip, w := range l.seen {
		if now.After(w.until) {
			delete(l.seen, ip)
		}
	}

	l.sweptAt = now
}
