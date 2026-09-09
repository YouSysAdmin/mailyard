// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relayagent

import (
	"context"
	"crypto/tls"
	"encoding/json/v2"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"

	"github.com/yousysadmin/mailyard/internal/core/dnsname"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/core/iplimit"
	"github.com/yousysadmin/mailyard/internal/core/safego"
)

// Meta keys for the cached accept list. In the spool with the rest of
// the node's identity, so a node that restarts while the control
// plane is unreachable still answers RCPT the way it did before.
const (
	metaAcceptNames = "accept_names"
	metaAcceptETag  = "accept_etag"
)

// acceptList is which recipient domains this node may take mail for.
//
// It is a CACHE of the platform's answer, refreshed on the heartbeat,
// and it exists because a node has no database. Asking the platform
// per RCPT would put a round trip over the unreliable link inside
// every SMTP session - on the link whose unreliability is the reason
// the MX is out here at all.
type acceptList struct {
	mu    sync.RWMutex
	names map[string]bool
	etag  string
	// known is false until a list has actually been received. It is
	// NOT the same as an empty list, and the difference decides
	// whether an unrecognized recipient gets a 451 or a 550.
	known bool
}

// covers reports whether any name in the list covers this address's
// domain, by the same rule the platform applies - dnsname.Covering,
// so a subdomain of a verified name is accepted here exactly as it
// would be there.
func (a *acceptList) covers(addr string) bool {
	_, host, ok := strings.CutLast(addr, "@")
	if !ok || host == "" {
		return false
	}

	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, candidate := range dnsname.Covering(host) {
		if a.names[candidate] {
			return true
		}
	}

	return false
}

// domainScope is the node's OWN restriction, relay_node.domains.
//
// A separate type from acceptList rather than a second mode on it,
// because the two differ exactly where it matters: an empty acceptList
// means "the platform has told me nothing yet", an empty scope means
// "no restriction of my own". One object holding both would answer one
// of those wrongly on any given day.
//
// Fixed at construction and never replaced, so it needs no lock.
type domainScope struct{ names map[string]bool }

// newDomainScope builds the scope. A nil or empty list is unrestricted.
func newDomainScope(domains []string) *domainScope {
	names := make(map[string]bool, len(domains))
	for _, d := range domains {
		if trimmed := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(d), ".")); trimmed != "" {
			names[trimmed] = true
		}
	}

	return &domainScope{names: names}
}

// covers reports whether this node serves the address's domain.
//
// Subdomains ARE covered, by dnsname.Covering, which is the rule the
// platform's own list applies - a node must not refuse what the
// platform would have accepted for a name it was given. The outbound
// half of the same config value is matched EXACTLY instead, because
// there the rule is SPF's rather than ours.
func (d *domainScope) covers(addr string) bool {
	if d == nil || len(d.names) == 0 {
		return true
	}

	_, host, ok := strings.CutLast(addr, "@")
	if !ok || host == "" {
		return false
	}

	for _, candidate := range dnsname.Covering(host) {
		if d.names[candidate] {
			return true
		}
	}

	return false
}

func (a *acceptList) ready() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.known
}

func (a *acceptList) etagOf() string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.etag
}

func (a *acceptList) replace(names []string, etag string) {
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[strings.ToLower(strings.TrimSuffix(strings.TrimSpace(n), "."))] = true
	}
	a.mu.Lock()
	a.names, a.etag, a.known = set, etag, true
	a.mu.Unlock()
}

// load restores what the last run held. A node restarting during an
// outage of the control plane keeps answering RCPT correctly instead
// of temporarily refusing everything.
func (a *acceptList) load(s *Spool) {
	blob, _ := s.Meta(metaAcceptNames)
	etag, _ := s.Meta(metaAcceptETag)
	if blob == "" || etag == "" {
		return
	}
	var names []string
	if err := json.Unmarshal([]byte(blob), &names); err != nil {
		return
	}
	a.replace(names, etag)
}

func (a *acceptList) store(s *Spool, names []string, etag string) error {
	blob, err := json.Marshal(names, agentJSON)
	if err != nil {
		return err
	}

	if err := s.SetMeta(metaAcceptNames, string(blob)); err != nil {
		return err
	}

	return s.SetMeta(metaAcceptETag, etag)
}

// ReceiveBackend is the MX-facing listener a node runs.
//
// No AUTH: the internet delivers here. This is the same shape as the
// platform's own MX listener, and everything it does NOT do is the
// point - no parsing, no suppression list, no authentication verdict.
// Those need a database. The node's whole job is to take the bytes,
// write them down, and be honest about what it saw.
type ReceiveBackend struct {
	Spool          *Spool
	Accept         *acceptList
	Log            *slog.Logger
	MaxMessageSize int64
	Limiter        *iplimit.Limiter

	// Local is relay_node.domains, this node's own scope. Empty means
	// the platform's list decides alone, which is every single-tenant
	// node.
	//
	// ANDed with the platform's list, never ORed: the node may only
	// narrow what it was told to accept. A node handed a domain it does
	// not serve is a routing mistake somewhere else, and taking the
	// mail anyway would forward it to a platform that has no reason to
	// expect it from here.
	Local *domainScope

	// Wake nudges the forward loop, so a message is not sitting out a
	// poll interval while the link is perfectly healthy.
	Wake func()
}

// NewSession implements smtp.Backend for the MX listener. Its clients
// are strangers, so nothing here is trusted the way the worker-facing
// listener trusts a certificate.
func (b *ReceiveBackend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	remote := ""
	if c != nil && c.Conn() != nil {
		remote = c.Conn().RemoteAddr().String()
	}
	ip := remote
	if host, _, err := net.SplitHostPort(remote); err == nil {
		ip = host
	}

	if !b.Limiter.Allow(ip) {
		b.Log.Warn("relay node: inbound rate limited", "ip", ip)

		return nil, &smtp.SMTPError{Code: 421, EnhancedCode: smtp.EnhancedCode{4, 7, 0}, Message: "rate limit exceeded"}
	}
	helo := ""
	if c != nil {
		helo = c.Hostname()
	}

	return &receiveSession{backend: b, ip: ip, helo: helo}, nil
}

type receiveSession struct {
	backend *ReceiveBackend
	ip      string
	helo    string
	from    string
	to      []string
}

// AuthMechanisms implements smtp.Session and offers none - this is an
// MX, and a sender does not authenticate to one.
func (s *receiveSession) AuthMechanisms() []string { return nil }

// Auth implements smtp.Session and always refuses.
func (s *receiveSession) Auth(string) (sasl.Server, error) { return nil, smtp.ErrAuthUnsupported }

// Reset implements smtp.Session, clearing the envelope between
// messages on one connection.
func (s *receiveSession) Reset() { s.from = ""; s.to = nil }

// Logout implements smtp.Session.
func (s *receiveSession) Logout() error { return nil }

// Mail implements smtp.Session, recording the envelope sender SPF will
// be computed against.
func (s *receiveSession) Mail(from string, _ *smtp.MailOptions) error {
	// An empty MAIL FROM is a null return path, which is what a bounce
	// report carries - and bounces are the reason this listener exists.
	if from != "" {
		if addr, err := mail.ParseAddress(from); err == nil {
			s.from = addr.Address
		} else {
			s.from = from
		}
	}

	return nil
}

// Rcpt is the only gate in front of an open port 25, so what it
// refuses and HOW it refuses both matter.
func (s *receiveSession) Rcpt(to string, _ *smtp.RcptOptions) error {
	addr := to
	if parsed, err := mail.ParseAddress(to); err == nil {
		addr = parsed.Address
	}
	addr = strings.ToLower(strings.TrimSpace(addr))

	if !s.backend.Accept.ready() {
		// Never heard from the platform. TEMPORARY, not 550: "I do not
		// know yet" is not "no such domain", and a hard refusal here
		// would bounce perfectly good mail for the minutes between this
		// node starting and its first heartbeat landing.
		//
		// Logged per refusal, because the two ways to stay in this
		// state for long - an unapproved node, a control url that
		// cannot be reached - both look exactly like this from the
		// sender's side and like nothing at all from the console.
		s.backend.Log.Warn("relay node: recipient deferred, no domain list yet",
			"rcpt", addr, "ip", s.ip)

		return &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 3, 0},
			Message: "this node has not yet received its recipient domain list"}
	}

	// The refusals carry WHICH list said no, in the log only - the
	// SMTP answer is the same 550 either way, because telling a
	// stranger which half refused is an oracle. The operator reading
	// this log is the one debugging an accept list.
	if !s.backend.Accept.covers(addr) {
		s.backend.Log.Info("relay node: recipient refused",
			"rcpt", addr, "ip", s.ip, "reason", "not on the platform's domain list")

		return &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 1, 1}, Message: "relay not permitted"}
	}

	if !s.backend.Local.covers(addr) {
		s.backend.Log.Info("relay node: recipient refused",
			"rcpt", addr, "ip", s.ip, "reason", "outside relay_node.domains")

		return &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 1, 1}, Message: "relay not permitted"}
	}

	if len(s.to) >= maxSessionRecipients {
		return &smtp.SMTPError{Code: 452, EnhancedCode: smtp.EnhancedCode{4, 5, 3}, Message: "too many recipients"}
	}
	s.to = append(s.to, addr)

	return nil
}

// maxSessionRecipients matches the platform's own MX, which is what
// this node stands in for.
const maxSessionRecipients = 100

// Data implements smtp.Session, spooling the message into the received
// bucket before answering 250.
func (s *receiveSession) Data(r io.Reader) (err error) {
	// Same guard as the platform's MX and for the same reason: this
	// listener takes bytes from anyone on the internet, and go-smtp
	// does not guard its session goroutines, so a panic anywhere below
	// would be a remote kill of the node.
	defer func() {
		if rec := recover(); rec != nil {
			safego.Report(s.backend.Log, "relay node: inbound data", rec, "from", s.from, "remote_ip", s.ip)
			err = &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 3, 0},
				Message: "temporary failure processing message"}
		}
	}()
	if len(s.to) == 0 {
		return &smtp.SMTPError{Code: 554, EnhancedCode: smtp.EnhancedCode{5, 5, 1}, Message: "no valid recipients"}
	}

	limit := s.backend.MaxMessageSize
	if limit <= 0 {
		limit = 26214400
	}
	raw, rerr := io.ReadAll(io.LimitReader(r, limit+1))
	if rerr != nil {
		return &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 3, 0}, Message: "read error"}
	}

	if int64(len(raw)) > limit {
		return &smtp.SMTPError{Code: 552, EnhancedCode: smtp.EnhancedCode{5, 3, 4}, Message: "message size exceeds limit"}
	}

	// Written down BEFORE the 250. The sending MTA walks away the
	// moment it hears one, so anything held only in memory at that
	// point is mail nobody will ever send again.
	m := &Received{
		ID:           ids.New(),
		EnvelopeFrom: s.from,
		Recipients:   append([]string(nil), s.to...),
		ClientIP:     s.ip,
		HELO:         s.helo,
		ReceivedAt:   time.Now().UTC(),
		NextAttempt:  time.Now().UTC(),
	}
	if perr := s.backend.Spool.PutReceived(m, raw); perr != nil {
		s.backend.Log.Error("relay node: could not spool received mail", "err", perr)

		return &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 3, 0}, Message: "could not queue message"}
	}
	s.backend.Log.Info("relay node: received",
		"id", m.ID, "from", s.from, "recipients", len(s.to), "bytes", len(raw), "client_ip", s.ip)
	if s.backend.Wake != nil {
		s.backend.Wake()
	}

	return nil
}

// NewReceiveServer builds the MX listener.
//
// STARTTLS when a config is given, cleartext otherwise, and never a
// refusal either way. Inbound mail on the internet uses opportunistic
// TLS: a receiver that insists on it simply does not receive from the
// senders that cannot offer it.
func NewReceiveServer(b *ReceiveBackend, addr, hostname string, tlsCfg *tls.Config) *smtp.Server {
	srv := smtp.NewServer(b)
	srv.Addr = addr
	srv.Domain = hostname
	srv.ReadTimeout = 60 * time.Second
	srv.WriteTimeout = 60 * time.Second
	srv.MaxMessageBytes = b.MaxMessageSize
	srv.MaxRecipients = maxSessionRecipients
	srv.EnableSMTPUTF8 = true
	srv.TLSConfig = tlsCfg
	srv.ErrorLog = NewSMTPLogger(b.Log, "inbound")

	return srv
}

// forwardLoop drains received mail to the control plane.
//
// The same discipline as the outcome reports: send, and delete only
// what was accepted. The order matters more here than anywhere else
// in this process, because the node already answered 250 - the copy
// in the spool is the only copy that exists.
func (a *Agent) forwardLoop(ctx context.Context) {
	tick := time.Tick(30 * time.Second)
	for {
		a.forwardReceived(ctx)
		select {
		case <-ctx.Done():
			return
		case <-a.forwardWake:
		case <-tick:
		}
	}
}

// forwardWakeup nudges the loop without blocking the SMTP session
// that called it. A missed nudge costs one poll interval, where a
// blocked Data call costs the sending MTA a timeout.
func (a *Agent) forwardWakeup() {
	select {
	case a.forwardWake <- struct{}{}:
	default:
	}
}

// forwardBatch bounds one pass. Small, because each item is a whole
// message on the wire and a node behind a slow link should make
// steady progress rather than one enormous attempt that times out.
const forwardBatch = 20

func (a *Agent) forwardReceived(ctx context.Context) {
	now := time.Now().UTC()
	due, err := a.spool.ReceivedDue(now, forwardBatch)
	if err != nil {
		a.log.Error("relay node: could not read received mail", "err", err)

		return
	}
	for _, m := range due {
		if ctx.Err() != nil {
			return
		}
		a.forwardOne(ctx, m)
	}
}

func (a *Agent) forwardOne(ctx context.Context, m *Received) {
	body, err := a.spool.ReceivedBody(m.ID)
	if err != nil {
		// The bytes are gone and no attempt can bring them back.
		// Dropping the entry stops the node retrying a message it
		// cannot read for the rest of its life.
		a.log.Error("relay node: received message body is missing, dropping it", "id", m.ID, "err", err)
		if rerr := a.spool.RemoveReceived(m.ID); rerr != nil {
			a.log.Error("relay node: could not drop a bodyless entry", "id", m.ID, "err", rerr)
		}

		return
	}

	res, err := a.ctrl.Inbound(ctx, a.nodeID, a.token, InboundMessage{
		EnvelopeFrom: m.EnvelopeFrom,
		EnvelopeTo:   m.Recipients,
		ClientIP:     m.ClientIP,
		HELO:         m.HELO,
		Raw:          body,
	})
	switch err {
	case nil:
		// accepted, duplicate or refused - all three are the platform
		// having DECIDED, so the node is done with it either way.
		if res.Status == inboundStatusRefused {
			a.log.Info("relay node: the platform refused forwarded mail",
				"id", m.ID, "reason", res.Reason, "recipients", len(m.Recipients))
		}

		if rerr := a.spool.RemoveReceived(m.ID); rerr != nil {
			a.log.Error("relay node: could not clear forwarded mail", "id", m.ID, "err", rerr)
		}

		return

	default:
		if ce, ok := errors.AsType[*ControlError](err); ok && ce.Fatal() {
			// Refused in a way retrying cannot change. Said loudly,
			// because this is the one path in the node that discards a
			// message a stranger's MTA was told we had accepted.
			a.log.Error("relay node: forwarded mail refused permanently, dropping it",
				"id", m.ID, "recipients", len(m.Recipients), "err", err)
			if rerr := a.spool.RemoveReceived(m.ID); rerr != nil {
				a.log.Error("relay node: could not drop refused mail", "id", m.ID, "err", rerr)
			}

			return
		}
		a.retryForward(m, err)
	}
}

// retryForward schedules the next attempt, or gives up.
//
// Giving up on a message that was ACCEPTED at the SMTP layer is a
// loss, so max_lifetime is the only thing that causes it and it is
// the same window the outbound queue uses. The alternative - keeping
// it forever - fills a disk on a node nobody is watching, and hides
// the fact that the link has been down for a week.
func (a *Agent) retryForward(m *Received, cause error) {
	m.Attempts++
	m.LastError = cause.Error()

	if time.Since(m.ReceivedAt) > a.cfg.MaxLifetime {
		a.log.Error("relay node: giving up on forwarding a received message",
			"id", m.ID, "age", time.Since(m.ReceivedAt).String(),
			"attempts", m.Attempts, "err", cause)
		if rerr := a.spool.RemoveReceived(m.ID); rerr != nil {
			a.log.Error("relay node: could not drop an expired message", "id", m.ID, "err", rerr)
		}

		return
	}

	m.NextAttempt = time.Now().UTC().Add(forwardBackoff(m.Attempts))
	if uerr := a.spool.UpdateReceived(m); uerr != nil {
		a.log.Error("relay node: could not reschedule a forward", "id", m.ID, "err", uerr)
	}
	a.log.Warn("relay node: could not forward received mail, will retry",
		"id", m.ID, "attempts", m.Attempts, "next", m.NextAttempt.Format(time.RFC3339), "err", cause)
}

// forwardBackoff doubles to a ten minute ceiling.
//
// Lower than the outbound queue's four hours on purpose. That one
// backs off against a stranger's mail exchanger, which may be
// throttling us deliberately, and pushing harder makes it worse. This
// one is our own control plane, where a long backoff only adds delay
// to mail that is already sitting on a disk we do not administer.
func forwardBackoff(attempts int) time.Duration {
	d := 30 * time.Second
	for i := 1; i < attempts && d < 10*time.Minute; i++ {
		d *= 2
	}
	if d > 10*time.Minute {
		d = 10 * time.Minute
	}

	return d
}
