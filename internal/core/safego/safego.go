// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package safego keeps a panic in one background unit of work from
// taking the process with it.
//
// The HTTP edge already has this: server.safeRecover turns a panic in
// any handler into a 500 and the other requests carry on. Everything
// running outside a request had no equivalent - the delivery worker,
// the campaign runner, webhook dispatch, the audit writer - so a
// single malformed row or a bad MIME part in one inbound message
// killed the whole binary. The blast radius should be one job.
//
// This is a backstop, not error handling. A panic reaching one of
// these guards is a bug, so every recovery logs at error with a stack
// trace. If a guard fires regularly, fix what is panicking.
package safego

import (
	"fmt"
	"log/slog"
	"runtime/debug"
)

// Recover is the deferred guard. Name the unit of work so the log
// line says what died, and pass any identifying attributes:
//
//	defer safego.Recover(log, "queue: deliver", "email_id", job.ID)
func Recover(log *slog.Logger, unit string, attrs ...any) {
	r := recover()
	if r == nil {
		return
	}

	Report(log, unit, r, attrs...)
}

// Report logs an already-recovered panic value. Use it when the call
// site needs the value itself - to turn it into a return value, say
// - and so had to call recover() directly. Recover cannot be reused
// there: recover() only returns non-nil to the deferred function of
// the panicking frame, so a second call inside it yields nil and the
// panic would go unlogged.
func Report(log *slog.Logger, unit string, r any, attrs ...any) {
	if r == nil {
		return
	}

	logPanic(log, unit, r, attrs...)
}

// Do runs fn and converts a panic into an error, for call sites that
// already have an error path to route the failure into. The returned
// error is also logged, because a panic is worth an error line even
// when the caller handles it quietly.
func Do(log *slog.Logger, unit string, fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			logPanic(log, unit, r)
			err = fmt.Errorf("%s: panic: %v", unit, r)
		}
	}()

	return fn()
}

// Go starts fn in a guarded goroutine. Use it instead of a bare `go`
// for anything that outlives a request.
func Go(log *slog.Logger, unit string, fn func()) {
	go func() {
		defer Recover(log, unit)
		fn()
	}()
}

func logPanic(log *slog.Logger, unit string, r any, attrs ...any) {
	if log == nil {
		log = slog.Default()
	}

	args := make([]any, 0, len(attrs)+4)
	args = append(args, "unit", unit, "reason", fmt.Sprintf("%v", r))
	args = append(args, attrs...)
	args = append(args, "stack", string(debug.Stack()))
	log.Error("panic recovered in background work", args...)
}
