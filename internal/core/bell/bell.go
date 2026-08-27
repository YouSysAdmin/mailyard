// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package bell is a broadcast wake-up: any number of waiters block on
// it, one Ring releases all of them. It is what a long-poll handler
// waits on so a request parked for thirty seconds returns the moment
// there is something to return, without polling the database to find
// out.
package bell

import (
	"context"
	"sync"
	"time"
)

// Bell is a level-triggered broadcast. The zero value is ready to use.
type Bell struct {
	mu sync.Mutex
	ch chan struct{}
}

func (b *Bell) current() chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ch == nil {
		b.ch = make(chan struct{})
	}

	return b.ch
}

// Ring releases every current waiter. A ring with nobody waiting is
// not remembered: a waiter that arrives afterwards checks its own
// condition first, which is the shape every long-poll here has.
func (b *Bell) Ring() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ch != nil {
		close(b.ch)
		b.ch = nil
	}
}

// Wait blocks until the bell rings, d elapses, or ctx ends. Reports
// true when it was the bell.
func (b *Bell) Wait(ctx context.Context, d time.Duration) bool {
	if b == nil {
		select {
		case <-time.After(d):
		case <-ctx.Done():
		}

		return false
	}

	ch := b.current()
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ch:
		return true
	case <-t.C:
		return false
	case <-ctx.Done():
		return false
	}
}
