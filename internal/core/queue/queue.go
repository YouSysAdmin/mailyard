// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package queue is the DB-backed delivery worker pool: a poll loop
// claims due email rows through a Source, a pool of goroutines runs
// them through a Processor, and outcomes route back into the row as
// terminal status or a backoff re-queue.
//
// Claiming is a single locking statement (SELECT ... FOR UPDATE SKIP
// LOCKED, see the email store), so any number of nodes can drain one
// queue and each takes a disjoint batch without contending for the
// head of it. Nothing here is per-node state - a worker that dies
// mid-send leaves its rows in processing and RecoverStuck hands them
// back after ClaimTimeout, on whichever node polls next.
package queue

import (
	"context"
	"time"

	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
)

// OutcomeKind classifies what the processor did with a job.
type OutcomeKind int

const (
	// KindDone - delivered, finalize as sent.
	KindDone OutcomeKind = iota

	// KindRetry - transient failure, re-queue with backoff (or fail
	// when attempts are exhausted).
	KindRetry

	// KindFail - permanent failure, finalize as failed immediately.
	KindFail

	// KindSuppressed - every recipient is suppressed, finalize as
	// suppressed.
	KindSuppressed
)

// Outcome is the processor's verdict on one job.
type Outcome struct {
	Kind OutcomeKind
	Err  error

	// ServerID is the server that actually carried the message, which
	// after a failover walk is not necessarily the one the sender
	// asked for. Set on KindDone only.
	//
	// It has to come back through here because the winner is a local
	// in the failover loop and nothing outside the processor can know
	// it - which is exactly why it went unrecorded until now.
	ServerID string
}

// Done is the terminal success outcome: the message left and nothing
// is retried.
func Done() Outcome { return Outcome{Kind: KindDone} }

// DoneVia is Done plus the server that carried it.
func DoneVia(serverID string) Outcome {
	return Outcome{Kind: KindDone, ServerID: serverID}
}

// Retry asks the worker to try again later, counting an attempt
// against the cap.
func Retry(err error) Outcome { return Outcome{Kind: KindRetry, Err: err} }

// Fail is terminal: err is permanent and no further attempt is made.
func Fail(err error) Outcome { return Outcome{Kind: KindFail, Err: err} }

// Suppressed is terminal and NOT a failure of ours - the recipient is
// on the do-not-send list, so nothing was sent on purpose.
func Suppressed(err error) Outcome { return Outcome{Kind: KindSuppressed, Err: err} }

// Processor delivers one claimed email. Implemented by the email
// domain (domain/email/processor.go).
type Processor interface {
	Process(ctx context.Context, job *emailmodel.Email) Outcome
}

// Source is the queue's persistence: implemented by the email store.
// Methods are not project-scoped - the worker drains every tenant.
type Source interface {
	// ClaimDue atomically claims up to limit due rows (status queued
	// or scheduled, next_attempt_at <= now) and returns them with
	// status processing and attempts already incremented.
	ClaimDue(ctx context.Context, now time.Time, limit int) ([]*emailmodel.Email, error)

	// Requeue returns a transiently-failed row to the queue for a
	// later attempt.
	//
	// createdAt is not decoration. The emails table is partitioned by
	// it, so a statement that names only the id has to visit every
	// live partition - and this one runs once per transient failure.
	// The worker already holds the row it claimed, so the value is
	// free here and turns the update back into a single-partition
	// write.
	Requeue(ctx context.Context, id string, createdAt time.Time, next time.Time, errMsg string) error

	// Finalize writes a terminal status.
	// Finalize writes the terminal state. deliveredVia names the
	// server that carried it and is empty for everything that never
	// left - it is not the pinned smtp_server_id, which means the
	// server the sender ASKED for and must keep meaning that across
	// retries.
	// createdAt prunes to one partition - see Requeue. This is the
	// hottest write in the product, once per message.
	Finalize(ctx context.Context, id string, createdAt time.Time, status, errMsg, deliveredVia string, sentAt *time.Time) error

	// RecoverStuck re-queues processing rows claimed before olderThan
	// (a previous process crashed mid-send). Returns the count.
	RecoverStuck(ctx context.Context, olderThan time.Time) (int, error)
}
