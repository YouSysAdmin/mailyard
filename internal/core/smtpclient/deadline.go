// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpclient

import (
	"context"
	"net"
	"sync"
	"time"
)

// IdleTimeout is how long a conversation may go with no byte moving
// in either direction before the connection is cut.
//
// net/smtp has no timeouts of its own. Two minutes is past the
// slowest honest reply, and a large message on a slow link keeps
// moving bytes and is not idle.
const IdleTimeout = 2 * time.Minute

// bound wraps conn so every read and write carries a fresh deadline,
// the nearer of now plus IdleTimeout and ctx's own deadline, and the
// connection is closed when ctx is cancelled.
//
// Wrapped BELOW the TLS layer, so the handshake and everything
// STARTTLS carries afterwards are bounded the same way.
func bound(ctx context.Context, conn net.Conn) net.Conn {
	limit, _ := ctx.Deadline()
	b := &boundConn{Conn: conn, limit: limit, stop: make(chan struct{})}
	if ctx.Done() != nil {
		go func() {
			select {
			case <-ctx.Done():
				_ = b.Conn.Close()
			case <-b.stop:
			}
		}()
	}

	return b
}

type boundConn struct {
	net.Conn
	limit time.Time
	stop  chan struct{}
	once  sync.Once
}

func (c *boundConn) deadline() time.Time {
	d := time.Now().Add(IdleTimeout)
	if !c.limit.IsZero() && c.limit.Before(d) {
		return c.limit
	}

	return d
}

func (c *boundConn) Read(p []byte) (int, error) {
	_ = c.SetReadDeadline(c.deadline())

	return c.Conn.Read(p)
}

func (c *boundConn) Write(p []byte) (int, error) {
	_ = c.SetWriteDeadline(c.deadline())

	return c.Conn.Write(p)
}

func (c *boundConn) Close() error {
	c.once.Do(func() { close(c.stop) })

	return c.Conn.Close()
}
