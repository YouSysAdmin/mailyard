// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"time"
)

// lockRetry is how long a waiter sleeps between attempts.
const lockRetry = 20 * time.Millisecond

// Locks are named locks shared by every node, held as Postgres
// session advisory locks on a connection of their own.
//
// A session lock lives on one connection, so this takes a dedicated
// connection from the pool where every store issues statements through
// whichever one Base hands it. Base is embedded for Q alone.
type Locks struct {
	Base
	pool *sql.DB
}

// NewLocks builds the lock service over the primary.
func NewLocks(db *sql.DB) *Locks { return &Locks{Base: NewBase(db), pool: db} }

// Hold waits for the lock named by scope and key and returns the
// function that releases it.
//
// A waiter TRIES and gives its connection back between attempts rather
// than blocking on one. Blocked waiters would each pin a pooled
// connection while the holder needs one more to do the work it holds
// the lock for, and a pool full of waiters is a deadlock until the
// request times out.
func (l *Locks) Hold(ctx context.Context, scope, key string) (func(), error) {
	for {
		conn, err := l.pool.Conn(ctx)
		if err != nil {
			return nil, err
		}

		var got bool
		if err := conn.QueryRowContext(ctx,
			l.Q(`SELECT pg_try_advisory_lock(hashtext(?), hashtext(?))`), scope, key).Scan(&got); err != nil {
			_ = conn.Close()

			return nil, err
		}

		if got {
			return func() { l.release(conn, scope, key) }, nil
		}

		_ = conn.Close()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(lockRetry):
		}
	}
}

// release unlocks and returns the connection. A connection whose
// unlock did not go through is discarded rather than pooled, since the
// lock would otherwise stay held by an idle session forever.
func (l *Locks) release(conn *sql.Conn, scope, key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var unlocked bool
	err := conn.QueryRowContext(ctx,
		l.Q(`SELECT pg_advisory_unlock(hashtext(?), hashtext(?))`), scope, key).Scan(&unlocked)
	if err != nil || !unlocked {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	}

	_ = conn.Close()
}
