// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package database

import (
	"context"
	"database/sql"
	"math/rand/v2"
	"sync/atomic"
)

// Base is the scaffolding every domain store repeats: the handles,
// the q() that rewrites `?` placeholders, and the read/write routing.
//
// Embed it rather than redeclaring those things:
//
//	Type Store struct{ database.Base }
//
//	func NewStore(db *sql.DB) *Store {
//	    return &Store{database.NewBase(db)}
//	}
//
// Twenty-eight stores each carried their own identical copy, which is
// twenty-eight chances for one to forget Rebind and send `?` straight
// at a driver that wants $1. The fields stay unexported so the
// embedding store reaches a handle through DB() and cannot acquire a
// second way to build queries. TestStoresDoNotReachPastTheHelpers
// keeps DB() itself down to transactions.
type Base struct {
	db *sql.DB

	// replicas are read-only followers, if any are configured. They
	// are reached only through the Read* helpers, never by default.
	//
	// Opt in, not opt out, and that is the whole safety story. If
	// reads went to a replica unless a query said otherwise, then
	// every query written from now on would silently get replica
	// semantics, and the failure mode is not an error - it is a row
	// that was just written coming back missing. Somebody logs in, the
	// session row lands on the primary, the next request reads a
	// follower that has not caught up, and the console says
	// "authentication required" for reasons nobody can reproduce.
	//
	// With the default the other way round, forgetting to opt in costs
	// a query on the primary that could have been elsewhere. That is a
	// performance note, not an outage.
	replicas []*sql.DB

	// next round-robins across replicas. A plain uint64 touched with
	// atomic.AddUint64 rather than an atomic.Uint64: that type carries
	// a noCopy marker, and Base is returned BY VALUE from NewBase into
	// thirty store constructors, so go vet would flag every one of
	// them. Nothing here needs the extra type safety - one field, one
	// operation.
	next uint64
}

// NewBase builds the scaffolding around a primary handle, optionally
// with read-only followers.
//
// Variadic so the thirty existing call sites - and every store test -
// keep compiling unchanged. A store that never reads from a replica
// simply does not take the argument, which is also the honest signal:
// plumbing replicas into a store that has no Read* query is plumbing
// nobody can tell is dead.
//
// The round-robin offset starts somewhere random so a fleet
// restarting together does not aim every first query at the same follower.
func NewBase(db *sql.DB, replicas ...*sql.DB) Base {
	b := Base{db: db, replicas: replicas}
	if len(replicas) > 1 {
		b.next = rand.Uint64()
	}

	return b
}

// DB is the raw handle - the PRIMARY, always. Reserved for
// transactions, which have to pin one connection and are writes anyway.
func (b *Base) DB() *sql.DB { return b.db }

// Q rewrites the `?` placeholders a query is written with into
// Postgres's $1..$N. Every query a store issues goes through it.
func (b *Base) Q(query string) string { return Rebind(query) }

// Exec runs a statement against the PRIMARY, and Query and QueryRow
// below it do the same for reads. Each is Q plus the matching
// database/sql call, so a store cannot accidentally issue a query that
// skipped Rebind or landed somewhere it should not have.
//
// query is a parameter here, so TestNoDynamicSQL cannot judge it at
// this line - there is no string left to look at. It judges every
// CALL SITE of these instead, which is where the text still exists.
func (b *Base) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	//sqlconst:allow the query is checked at each Exec call site
	return b.db.ExecContext(ctx, b.Q(query), args...)
}

// Query runs a read against the PRIMARY. For a scan that may live with
// replica lag use ReadQuery.
func (b *Base) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	//sqlconst:allow the query is checked at each Query call site
	return b.db.QueryContext(ctx, b.Q(query), args...)
}

// QueryRow runs a single-row read against the PRIMARY.
func (b *Base) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	//sqlconst:allow the query is checked at each QueryRow call site
	return b.db.QueryRowContext(ctx, b.Q(query), args...)
}

// ReadQuery and ReadQueryRow are Query and QueryRow against a read
// replica, falling back to the primary when none is configured.
//
// Only for a SELECT whose caller can live with a follower being
// seconds behind. Not eligible when it writes (including FOR UPDATE),
// when a write-in the same request depends on the answer (a quota
// count, a uniqueness probe, the suppression check), when the caller
// just wrote the row, or when it resolves authentication. A stale
// answer in this is a wrong decision, not a stale display.
//
// What is left is aggregate reporting and browsing retrospective logs,
// which are also the queries that scan the most rows.
//
// TestReadHelpersOnlyServeReads fails on a statement here that is not
// a SELECT.
func (b *Base) ReadQuery(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	//sqlconst:allow the query is checked at each ReadQuery call site
	return b.reader().QueryContext(ctx, b.Q(query), args...)
}

// ReadQueryRow is QueryRow against a read replica, falling back to the
// primary when none is configured. Never for anything the same request
// wrote.
func (b *Base) ReadQueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	//sqlconst:allow the query is checked at each ReadQueryRow call site
	return b.reader().QueryRowContext(ctx, b.Q(query), args...)
}

// reader picks a follower, or the primary when none are configured.
//
// Plain round-robin. Not least-connections or latency-weighted:
// database/sql already queues per handle, so a slow replica drains
// its own pool rather than the process, and a scheduler that tries to
// be cleverer than that needs health signals it would then have to
// keep honest. The starting offset is randomized per process so a
// fleet restarting together does not send every first query to the same follower.
func (b *Base) reader() *sql.DB {
	switch len(b.replicas) {
	case 0:
		return b.db
	case 1:
		return b.replicas[0]
	default:
		i := atomic.AddUint64(&b.next, 1)

		return b.replicas[i%uint64(len(b.replicas))]
	}
}

// HasReplicas reports whether Read* actually reaches a follower.
func (b *Base) HasReplicas() bool { return len(b.replicas) > 0 }
