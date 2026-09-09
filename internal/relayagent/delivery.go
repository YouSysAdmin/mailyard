// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relayagent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/mx"
	"github.com/yousysadmin/mailyard/internal/core/safego"
	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
)

// Deliverer drains the spool into the internet.
type Deliverer struct {
	Spool    *Spool
	Lookup   *mx.Lookup
	Log      *slog.Logger
	HELO     string
	SMTPPort int
	Network  string
	// MaxLifetime bounds how long a message may keep failing.
	MaxLifetime time.Duration

	// Concurrency caps simultaneous outbound sessions.
	Concurrency int

	// PollInterval is the floor. Most work arrives via Wake.
	PollInterval time.Duration

	// Report is called with every terminal outcome. Nil during tests
	// that only care about the wire.
	Report func(context.Context, Outcome)

	// Send is a test seam. Nil means smtpclient.SendDirect.
	Send func(context.Context, smtpclient.DirectConfig, []string, *smtpclient.Raw) (*smtpclient.DirectResult, error)

	wake chan struct{}
	once sync.Once

	// inFlight stops two passes working the same message. The loop can
	// start a new pass while a slow delivery is still running, and
	// without this the same message would be attempted twice at once
	// and delivered twice.
	mu       sync.Mutex
	inFlight map[string]bool
}

// Outcome is one terminal result for one recipient.
type Outcome struct {
	EmailID   string
	Recipient string

	// Delivered is true when the destination accepted it.
	Delivered bool

	// Permanent distinguishes a refusal from having run out of time.
	// Both are final, but only one is the recipient's fault.
	Permanent bool
	Reason    string
}

func (d *Deliverer) init() {
	d.once.Do(func() {
		d.wake = make(chan struct{}, 1)
		d.inFlight = map[string]bool{}
	})
}

// Wake asks the loop to look now. Coalesced: a wake is
// level-triggered, so several arrivals during one pass are one nudge.
func (d *Deliverer) Wake() {
	d.init()
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// Start runs until ctx is done.
func (d *Deliverer) Start(ctx context.Context) {
	d.init()
	interval := d.PollInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	tick := time.Tick(interval)

	for {
		d.pass(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick:
		case <-d.wake:
		}
	}
}

func (d *Deliverer) pass(ctx context.Context) {
	// init here as well as in Start. The in-flight map is what stops
	// one message being delivered twice, and a guard that depends on
	// some other method having run first is not a guard.
	d.init()

	concurrency := d.Concurrency
	if concurrency < 1 {
		concurrency = 4
	}
	due, err := d.Spool.Due(time.Now(), concurrency*4)
	if err != nil {
		d.Log.Error("relay node: could not read the queue", "err", err)

		return
	}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for _, m := range due {
		if !d.claim(m.ID) {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		safego.Go(d.Log, "relay node: deliver", func() {
			defer func() {
				<-sem
				d.release(m.ID)
				wg.Done()
			}()
			d.attempt(ctx, m)
		})
	}
	wg.Wait()
}

func (d *Deliverer) claim(id string) bool {
	d.init()
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.inFlight[id] {
		return false
	}
	d.inFlight[id] = true

	return true
}

func (d *Deliverer) release(id string) {
	d.init()
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.inFlight, id)
}

// attempt makes one delivery attempt for one message.
func (d *Deliverer) attempt(ctx context.Context, m *Message) {
	m.Attempts++

	// Out of time before anything else. A message past its lifetime
	// must not make another connection, however transient its last
	// failure looked.
	if d.expired(m) {
		d.giveUp(ctx, m, "gave up after "+d.MaxLifetime.String())

		return
	}

	body, err := d.Spool.Body(m.ID)
	if err != nil {
		// The bytes are gone and no retry brings them back. Report it
		// as a failure rather than leaving a message that can never
		// succeed cycling through the queue forever.
		d.Log.Error("relay node: message body is unreadable", "id", m.ID, "err", err)
		d.giveUp(ctx, m, "message body was lost on the node")

		return
	}

	targets, err := d.Lookup.Resolve(ctx, m.Domain)
	if err != nil {
		if mxErr, ok := errors.AsType[*mx.Error](err); ok && mxErr.Permanent() {
			d.giveUp(ctx, m, mxErr.Error())

			return
		}
		d.defer_(m, err.Error())

		return
	}

	hosts := make([]string, 0, len(targets))
	for _, t := range targets {
		hosts = append(hosts, t.Host)
	}

	network := d.Network
	if network == "" {
		network = "tcp4"
	}
	cfg := smtpclient.DirectConfig{
		HELO:    d.HELO,
		Port:    d.SMTPPort,
		Network: network,
	}
	send := d.Send
	if send == nil {
		send = smtpclient.SendDirect
	}

	res, err := send(ctx, cfg, hosts, &smtpclient.Raw{
		EnvelopeFrom: m.EnvelopeFrom,
		To:           m.Recipients,
		Data:         body,
	})
	if err != nil {
		if se, ok := errors.AsType[*smtpclient.SendError](err); ok && se.Permanent() {
			// The destination refused the message itself, not one
			// recipient. That is the domain's answer and retrying
			// asks the same question again.
			d.giveUp(ctx, m, se.Error())

			return
		}
		d.defer_(m, err.Error())

		return
	}

	// Accepted recipients are done. Report each and drop it from the
	// message, so a retry for the rest cannot deliver to them twice.
	for _, rcpt := range res.Accepted {
		d.report(ctx, Outcome{
			EmailID: m.EmailID, Recipient: rcpt, Delivered: true,
			Reason: "delivered via " + res.Host,
		})
	}

	var retry []string
	for rcpt, rerr := range res.Rejected {
		if se, ok := errors.AsType[*smtpclient.SendError](rerr); ok && se.Permanent() {
			d.report(ctx, Outcome{
				EmailID: m.EmailID, Recipient: rcpt, Permanent: true,
				Reason: se.Error(),
			})
			continue
		}
		retry = append(retry, rcpt)
	}

	if len(retry) == 0 {
		// Done with this message either way, but say which. A message
		// every recipient refused is not "delivered", and an operator
		// reading that line would go looking for mail that was never
		// sent.
		msg := "relay node: delivered"
		if len(res.Accepted) == 0 {
			msg = "relay node: refused by the destination"
		}
		d.Log.Info(msg,
			"id", m.ID, "domain", m.Domain, "host", res.Host, "tls", res.TLS,
			"accepted", len(res.Accepted), "refused", len(res.Rejected))
		if err := d.Spool.Remove(m.ID); err != nil {
			d.Log.Error("relay node: could not remove a delivered message", "id", m.ID, "err", err)
		}

		return
	}

	m.Recipients = retry
	d.defer_(m, "some recipients deferred")
}

func (d *Deliverer) expired(m *Message) bool {
	if d.MaxLifetime <= 0 {
		return false
	}

	return time.Since(m.AcceptedAt) > d.MaxLifetime
}

// defer_ schedules the next attempt.
//
// Exponential with a ceiling, which is what every MTA does and for a
// reason worth stating: a receiver that is throttling wants fewer
// connections, and a fixed short retry is indistinguishable from
// hammering.
func (d *Deliverer) defer_(m *Message, reason string) {
	m.LastError = reason
	m.NextAttempt = time.Now().Add(backoff(m.Attempts))
	if err := d.Spool.Update(m); err != nil {
		d.Log.Error("relay node: could not update a queued message", "id", m.ID, "err", err)
	}
	d.Log.Warn("relay node: deferred",
		"id", m.ID, "domain", m.Domain, "attempt", m.Attempts,
		"next", m.NextAttempt.Format(time.RFC3339), "reason", reason)
}

// giveUp reports every remaining recipient as failed and drops the
// message. Reported as NOT permanent when we simply ran out of time -
// the address may be perfectly good and the platform should not
// suppress it.
func (d *Deliverer) giveUp(ctx context.Context, m *Message, reason string) {
	permanent := !d.expired(m)
	for _, rcpt := range m.Recipients {
		d.report(ctx, Outcome{
			EmailID: m.EmailID, Recipient: rcpt,
			Permanent: permanent, Reason: reason,
		})
	}
	d.Log.Warn("relay node: gave up",
		"id", m.ID, "domain", m.Domain, "attempts", m.Attempts, "reason", reason)
	if err := d.Spool.Remove(m.ID); err != nil {
		d.Log.Error("relay node: could not remove an abandoned message", "id", m.ID, "err", err)
	}
}

func (d *Deliverer) report(ctx context.Context, o Outcome) {
	if d.Report == nil {
		return
	}

	if o.EmailID == "" {
		// Nothing to attribute it to. Logged rather than silently
		// dropped, because a message with no id means the sending side
		// stopped stamping the header.
		d.Log.Warn("relay node: outcome has no sending id",
			"recipient", o.Recipient, "delivered", o.Delivered)

		return
	}
	d.Report(ctx, o)
}

// backoff grows the gap between attempts: about a minute, then five,
// fifteen, an hour, capped at four.
func backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	const base = time.Minute
	const ceiling = 4 * time.Hour
	d := time.Duration(math.Pow(3, float64(attempt-1))) * base
	if d > ceiling || d <= 0 {
		return ceiling
	}

	return d
}

// SweepExpired drops messages past their lifetime even if nothing has
// tried them lately, so a node that was offline for a week does not
// wake up and deliver a stack of stale mail.
func (d *Deliverer) SweepExpired(ctx context.Context) error {
	all, err := d.Spool.All()
	if err != nil {
		return err
	}
	for _, m := range all {
		if d.expired(m) {
			d.giveUp(ctx, m, fmt.Sprintf("gave up after %s", d.MaxLifetime))
		}
	}

	return nil
}
