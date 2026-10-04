// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package safego

import (
	"log/slog"
	"sync"
	"time"
)

// Group is work started after a request has answered that shutdown
// waits for before the database closes: a reset link, platform mail,
// an alert. Each unit carries its own timeout, so Wait is bounded by
// the slowest of them as well as by its own argument.
//
// A nil Group starts the work untracked, the same as Go.
type Group struct {
	wg sync.WaitGroup
}

// Go starts fn guarded and tracked.
func (g *Group) Go(log *slog.Logger, unit string, fn func()) {
	if g == nil {
		Go(log, unit, fn)

		return
	}

	g.wg.Go(func() {
		defer Recover(log, unit)
		fn()
	})
}

// Wait blocks until the tracked work finishes or timeout passes, and
// reports whether it finished.
func (g *Group) Wait(timeout time.Duration) bool {
	if g == nil {
		return true
	}

	done := make(chan struct{})
	go func() {
		g.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}
