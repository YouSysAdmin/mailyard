// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"regexp"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// Wake channels. Lowercase on purpose: pg_notify takes the channel as
// a string and does not case-fold it, while a bare LISTEN identifier
// does, so anything with capitals would listen on a name nobody
// notifies. Sanitize below quotes the identifier, and these names
// then match byte for byte.
const (
	// ChannelEmailQueue says an email row became due somewhere.
	ChannelEmailQueue = "mailyard_email_queue"

	// ChannelCampaign says a campaign started, resumed or was scheduled.
	ChannelCampaign = "mailyard_campaign"

	// ChannelRelayAssign says a message was assigned to a pull relay
	// node, so a long-poll waiting for that node can answer.
	ChannelRelayAssign = "mailyard_relay_assign"
)

const (
	// notifyInterval collapses a burst of sends into one NOTIFY. A
	// wake is level-triggered - it says "look at the queue", not "look
	// at this row" - so a receiver that arrives once for a hundred
	// enqueues has lost nothing, and the alternative is an extra round
	// trip per accepted email.
	notifyInterval = 250 * time.Millisecond

	// reconnectDelay paces retries after the listening connection
	// drops. Polling continues throughout, so a slow reconnect costs
	// latency and never delivery.
	reconnectDelay = 5 * time.Second
)

// safeChannel is what a channel name is allowed to contain. Every
// name is a package constant today, and this makes sure a future one
// cannot smuggle anything into the LISTEN identifier below.
var safeChannel = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

// Listener relays "there is work" nudges between nodes over Postgres
// LISTEN/NOTIFY.
//
// It exists because a wake is process-local. When an API node accepts
// an email, the worker on that node learns immediately and every
// other node waits out its poll interval - so a two-second tick, set
// to keep an idle cluster quiet, became two seconds of latency on
// every send once the roles were split across machines. NOTIFY closes
// that gap without adding a broker: the delivery queue is already in
// this database and the nudge travels the same connection.
//
// Nothing depends on it. Notifications are not durable, a node that
// is reconnecting misses whatever fires meanwhile, and none of that
// matters because the poll loop remains the actual guarantee. This is
// a latency optimization with a permanent fallback underneath it, and
// every failure path here logs and returns rather than escalating.
type Listener struct {
	db  *sql.DB
	dsn string
	log *slog.Logger

	mu      sync.Mutex
	subs    map[string][]func()
	pending map[string]struct{}
	kick    chan struct{}
}

// NewListener builds a relay. db carries the outgoing NOTIFY (it is a
// one-shot statement and the pool serves it fine), dsn opens the
// dedicated connection LISTEN needs - a pooled connection cannot hold
// a subscription, since database/sql may hand it to somebody else or
// reset it between uses.
func NewListener(db *sql.DB, dsn string, log *slog.Logger) *Listener {
	return &Listener{
		db:      db,
		dsn:     dsn,
		log:     log,
		subs:    map[string][]func(){},
		pending: map[string]struct{}{},
		kick:    make(chan struct{}, 1),
	}
}

// Subscribe registers fn to run whenever channel fires, on this node
// or any other. Call it before Start - subscriptions added later are
// not picked up until the connection next drops.
func (l *Listener) Subscribe(channel string, fn func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.subs[channel] = append(l.subs[channel], fn)
}

// Notify tells every node that channel has work. Non-blocking and
// best effort: to send is coalesced and performed by Start's
// goroutine, so a caller on the request path never waits on it and a
// failed NOTIFY costs latency rather than an error.
func (l *Listener) Notify(channel string) {
	l.mu.Lock()
	l.pending[channel] = struct{}{}
	l.mu.Unlock()
	select {
	case l.kick <- struct{}{}:
	default:
	}
}

// Start arms the relay until ctx is cancelled. Returns immediately.
func (l *Listener) Start(ctx context.Context) {
	go l.notifyLoop(ctx)

	l.mu.Lock()
	channels := make([]string, 0, len(l.subs))
	for ch := range l.subs {
		channels = append(channels, ch)
	}

	l.mu.Unlock()
	if len(channels) > 0 {
		go l.listenLoop(ctx, channels)
	}
}

// notifyLoop drains coalesced Notify calls, at most one round per
// notifyInterval.
func (l *Listener) notifyLoop(ctx context.Context) {
	for {
		select {
		case <-l.kick:
		case <-ctx.Done():
			return
		}

		l.mu.Lock()
		due := make([]string, 0, len(l.pending))
		for ch := range l.pending {
			due = append(due, ch)
		}

		clear(l.pending)
		l.mu.Unlock()

		for _, ch := range due {
			// pg_notify rather than a NOTIFY statement precisely so
			// the channel name is a bind parameter and not part of
			// the SQL text.
			if _, err := l.db.ExecContext(ctx, `SELECT pg_notify($1, '')`, ch); err != nil {
				if ctx.Err() != nil {
					return
				}

				l.log.Warn("pg notify failed, peers will pick the work up on their next poll",
					"channel", ch, "err", err)
			}
		}

		select {
		case <-time.After(notifyInterval):
		case <-ctx.Done():
			return
		}
	}
}

// listenLoop keeps a subscription up, reconnecting for as long as ctx lives.
func (l *Listener) listenLoop(ctx context.Context, channels []string) {
	for ctx.Err() == nil {
		err := l.listenOnce(ctx, channels)
		if ctx.Err() != nil {
			return
		}

		l.log.Warn("pg listen: subscription dropped, falling back to polling until it is back",
			"err", err, "retry_in", reconnectDelay.String())
		select {
		case <-time.After(reconnectDelay):
		case <-ctx.Done():
			return
		}
	}
}

// listenOnce holds one connection open and dispatches until it fails.
// It always returns a non-nil error - the only way out is a broken
// connection or a canceled context.
func (l *Listener) listenOnce(ctx context.Context, channels []string) error {
	conn, err := pgx.Connect(ctx, l.dsn)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	defer func() {
		// Not ctx: on cancellation the close would be cancelled too,
		// leaving the backend to time the session out on its own.
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = conn.Close(closeCtx)
	}()

	for _, ch := range channels {
		if !safeChannel.MatchString(ch) {
			return fmt.Errorf("refusing to listen on %q: not a plain identifier", ch)
		}

		// LISTEN takes an identifier, and an identifier cannot be a
		// bind parameter - pg_notify has a string form, LISTEN has no
		// equivalent. Every name is a package constant, matched
		// against safeChannel just above and quoted by Sanitize.
		//sqlconst:allow channel is a package constant, identifier-checked and Sanitize-quoted
		if _, err := conn.Exec(ctx, `LISTEN `+pgx.Identifier{ch}.Sanitize()); err != nil {
			return fmt.Errorf("listen %s: %w", ch, err)
		}
	}

	l.log.Info("pg listen: subscribed", "channels", channels)

	// Fire once on connect. Anything that happened while this node had
	// no subscription - startup, or the gap after a dropped connection
	// - was missed, and the rows behind it are sitting due right now.
	// Waiting for the next tick would make a reconnect the slowest
	// moment in the system rather than the fastest.
	for _, ch := range channels {
		l.fire(ch)
	}

	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return fmt.Errorf("wait: %w", err)
		}

		l.fire(n.Channel)
	}
}

// fire runs the subscribers for one channel. They are non-blocking
// wake nudges, so this stays on the listening goroutine.
func (l *Listener) fire(channel string) {
	l.mu.Lock()
	fns := l.subs[channel]
	l.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}
