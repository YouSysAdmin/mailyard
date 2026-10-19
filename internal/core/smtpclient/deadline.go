// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpclient

import (
	"context"
	"errors"
	"fmt"
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

// maxReplySize caps the bytes read between two writes, which in SMTP
// is one reply. The peer is chosen by a tenant or by a recipient's
// MX, and net/smtp accumulates continuation lines without a limit.
// Counted below TLS, so it covers a handshake flight as well.
const maxReplySize = 64 << 10

// maxConversationRead caps every byte read on one connection. A TLS
// layer writes on the peer's request (a KeyUpdate), which resets the
// per-reply count without a command having been sent.
const maxConversationRead = 4 << 20

// ErrReplyTooLarge reports a peer that sent more than a reply may hold.
// It is a transient failure of the attempt, never a server fault.
var ErrReplyTooLarge = errors.New("smtp reply too large")

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

	// reply counts bytes read since the last write, total since the dial.
	reply int
	total int

	// overflow is set once a cap is passed and answers every later read.
	overflow error
}

func (c *boundConn) deadline() time.Time {
	d := time.Now().Add(IdleTimeout)
	if !c.limit.IsZero() && c.limit.Before(d) {
		return c.limit
	}

	return d
}

func (c *boundConn) Read(p []byte) (int, error) {
	if c.overflow != nil {
		return 0, c.overflow
	}

	_ = c.SetReadDeadline(c.deadline())
	n, err := c.Conn.Read(p)
	c.reply += n
	c.total += n
	switch {
	case c.reply > maxReplySize:
		c.overflow = fmt.Errorf("%w: more than %d bytes in one reply", ErrReplyTooLarge, maxReplySize)
	case c.total > maxConversationRead:
		c.overflow = fmt.Errorf("%w: more than %d bytes in one conversation", ErrReplyTooLarge, maxConversationRead)
	}

	if c.overflow != nil {
		_ = c.Close()

		return 0, c.overflow
	}

	return n, err
}

// Write starts a new reply, since SMTP answers each command in turn.
func (c *boundConn) Write(p []byte) (int, error) {
	c.reply = 0
	_ = c.SetWriteDeadline(c.deadline())

	return c.Conn.Write(p)
}

func (c *boundConn) Close() error {
	c.once.Do(func() { close(c.stop) })

	return c.Conn.Close()
}
