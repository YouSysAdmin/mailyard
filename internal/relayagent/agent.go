// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relayagent

import (
	"context"
	"crypto/tls"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/certgen"
	"github.com/yousysadmin/mailyard/internal/core/iplimit"
	"github.com/yousysadmin/mailyard/internal/core/mx"
	"github.com/yousysadmin/mailyard/internal/core/relayca"
	"github.com/yousysadmin/mailyard/internal/core/safedial"
	"github.com/yousysadmin/mailyard/internal/core/safego"
)

// agentJSON is how the agent renders JSON - the spool entries it
// keeps, the accept-list cache, and the reports it sends home.
// Invalid UTF-8 is coerced to U+FFFD rather than refused, which is
// what v1 always did and what these strings need: spool metadata
// carries values from received mail and an outcome reason quotes a
// remote SMTP reply, and a bad byte in either must not fail a write
// for a message already accepted, or lose a report.
var agentJSON = json.JoinOptions(jsontext.AllowInvalidUTF8(true))

// Config is everything a node needs. Mirrors env.RelayNodeConfig,
// restated here so this package does not depend on the platform's
// configuration types.
type Config struct {
	ControlURL  string
	EnrollToken string
	ServerGroup string
	// Domains is this node's scope - see env.RelayNodeConfig.Domains.
	// Sent at enrolment and, when the node also receives, applied to
	// RCPT TO on top of the platform's accept list.
	Domains             []string
	Addr                string
	Hostname            string
	SpoolDir            string
	MaxLifetime         time.Duration
	HeartbeatInterval   time.Duration
	DeliveryConcurrency int
	SMTPPort            int
	IPv6                bool
	MaxMessageSize      int64
	Version             string
	// InboundEnabled says this node also runs an MX. Reported on the
	// heartbeat so the console can tell a node that receives from one
	// that does not - an empty forward queue means nothing on its own.
	InboundEnabled bool

	// Mode is "listen" or "pull" - see env.RelayNodeConfig.Mode. Empty
	// is listen.
	Mode string
}

// Pulls reports whether this node claims its mail rather than
// listening for it.
func (c Config) Pulls() bool { return c.Mode == "pull" }

// Agent is a running node.
type Agent struct {
	cfg   Config
	log   *slog.Logger
	spool *Spool
	ctrl  *Control

	nodeID string
	token  string
	peerCN string
	tlsCfg *tls.Config

	// accept is the recipient domain list this node answers RCPT from,
	// refreshed on the heartbeat and cached in the spool.
	accept *acceptList
	// forwardWake nudges the forward loop. Buffered by one and written
	// non-blockingly, so an SMTP session never waits on it.
	forwardWake chan struct{}

	Deliverer *Deliverer
}

// New opens the spool and enrols if this is a first run.
func New(ctx context.Context, cfg Config, log *slog.Logger) (*Agent, error) {
	spool, err := OpenSpool(cfg.SpoolDir)
	if err != nil {
		return nil, err
	}

	a := &Agent{
		cfg:         cfg,
		log:         log,
		spool:       spool,
		ctrl:        &Control{BaseURL: cfg.ControlURL},
		accept:      &acceptList{},
		forwardWake: make(chan struct{}, 1),
	}
	// Restore the accept list before anything can bind port 25. A node
	// restarting while the control plane is unreachable then keeps
	// answering RCPT the way it did, instead of refusing everything
	// until the first heartbeat gets through.
	a.accept.load(spool)
	if err := a.identify(ctx); err != nil {
		_ = spool.Close()

		return nil, err
	}

	if err := a.buildTLS(); err != nil {
		_ = spool.Close()

		return nil, err
	}

	network := "tcp4"
	if cfg.IPv6 {
		network = "tcp"
	}
	a.Deliverer = &Deliverer{
		Spool:        spool,
		Lookup:       mx.New(mx.Config{}),
		Log:          log,
		HELO:         cfg.Hostname,
		SMTPPort:     cfg.SMTPPort,
		Network:      network,
		MaxLifetime:  cfg.MaxLifetime,
		Concurrency:  cfg.DeliveryConcurrency,
		PollInterval: 30 * time.Second,
		Report:       a.recordOutcome,
	}

	return a, nil
}

// Close stops the agent and releases the spool. Anything already
// accepted stays on disk for the next start.
func (a *Agent) Close() error { return a.spool.Close() }

// mode is what this node tells the platform about how to reach it.
func (a *Agent) mode() string {
	if a.cfg.Pulls() {
		return "pull"
	}

	return "listen"
}

// NodeID is the identity this node enrolled as.
func (a *Agent) NodeID() string { return a.nodeID }

// identify loads the stored identity, or enrols to obtain one.
//
// The identity lives in the spool rather than in a separate file so a
// node is exactly one directory. A node that loses that directory
// enrols again as a NEW node, which is the safe outcome: the
// alternative would be answering to an identity whose key it no
// longer has.
func (a *Agent) identify(ctx context.Context) error {
	nodeID, _ := a.spool.Meta(metaNodeID)
	token, _ := a.spool.Meta(metaToken)
	cert, _ := a.spool.Meta(metaCert)
	key, _ := a.spool.Meta(metaKey)
	ca, _ := a.spool.Meta(metaCA)
	peer, _ := a.spool.Meta(metaPeerCN)

	if nodeID != "" && token != "" && cert != "" && key != "" && ca != "" {
		a.nodeID, a.token, a.peerCN = nodeID, token, peer
		a.log.Info("relay node: using stored identity", "node_id", nodeID)
		if err := a.renewIfDue(ctx, cert); err != nil {
			// Renewal failing is not fatal while the current
			// certificate is still valid - the node keeps working and
			// tries again on the next heartbeat.
			a.log.Warn("relay node: certificate renewal failed", "err", err)
		}

		return nil
	}

	if a.cfg.EnrollToken == "" {
		return errors.New("this node has no identity and relay_node.enroll_token is not set")
	}

	return a.enrol(ctx)
}

func (a *Agent) enrol(ctx context.Context) error {
	csrPEM, keyPEM, err := NewKeyAndCSR(a.cfg.Hostname)
	if err != nil {
		return err
	}
	port := portOf(a.cfg.Addr)

	res, err := a.ctrl.Register(ctx, a.cfg.EnrollToken, csrPEM, a.cfg.Hostname, port,
		a.cfg.Version, a.cfg.ServerGroup, a.cfg.Domains, a.mode())
	if err != nil {
		return fmt.Errorf("enrol: %w", err)
	}

	// The key is stored alongside everything else only after the
	// platform has accepted it. Storing first would leave a node
	// holding a key for a certificate it never got.
	for k, v := range map[string]string{
		metaNodeID:  res.NodeID,
		metaToken:   res.Token,
		metaCert:    res.Certificate,
		metaKey:     keyPEM,
		metaCA:      res.CA,
		metaPeerCN:  res.ClientName,
		metaEnroled: time.Now().UTC().Format(time.RFC3339),
	} {
		if serr := a.spool.SetMeta(k, v); serr != nil {
			return fmt.Errorf("store node identity: %w", serr)
		}
	}
	a.nodeID, a.token, a.peerCN = res.NodeID, res.Token, res.ClientName

	a.log.Info("relay node: enrolled", "node_id", res.NodeID, "status", res.Status)
	if res.Status != "enabled" {
		a.log.Warn("relay node: enrolled but not approved - it will receive no mail until a platform admin approves it",
			"node_id", res.NodeID, "status", res.Status)
	}

	return nil
}

// renewIfDue asks for a fresh certificate when the current one is
// close to expiring. A peer pair that expires takes every handshake
// down at once, so this runs at startup as well as on a timer.
func (a *Agent) renewIfDue(ctx context.Context, certPEM string) error {
	crt, err := certgen.ParseCertificate(certPEM)
	if err != nil {
		return err
	}

	if time.Until(crt.NotAfter) > relayca.RenewBefore {
		return nil
	}

	csrPEM, keyPEM, err := NewKeyAndCSR(a.cfg.Hostname)
	if err != nil {
		return err
	}
	res, err := a.ctrl.Renew(ctx, a.nodeID, a.token, csrPEM)
	if err != nil {
		return err
	}
	for k, v := range map[string]string{
		metaCert: res.Certificate, metaKey: keyPEM,
		metaCA: res.CA, metaPeerCN: res.ClientName,
	} {
		if serr := a.spool.SetMeta(k, v); serr != nil {
			return serr
		}
	}
	a.peerCN = res.ClientName
	a.log.Info("relay node: certificate renewed", "expires", crt.NotAfter.Format(time.RFC3339))

	// Rebuild so the listener serves the new pair without a restart.
	return a.buildTLS()
}

func (a *Agent) buildTLS() error {
	cert, _ := a.spool.Meta(metaCert)
	key, _ := a.spool.Meta(metaKey)
	ca, _ := a.spool.Meta(metaCA)

	pair, err := tls.X509KeyPair([]byte(cert), []byte(key))
	if err != nil {
		return fmt.Errorf("node certificate and key do not form a pair: %w", err)
	}
	pool, err := PeerPool(ca)
	if err != nil {
		return err
	}
	a.tlsCfg = &tls.Config{
		Certificates: []tls.Certificate{pair},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		// TLS 1.3 on a link whose only participants are our own
		// processes. There is nothing old to be compatible with.
		MinVersion: tls.VersionTLS13,
	}

	return nil
}

// recordOutcome writes a terminal result down before anything tries
// to send it.
//
// Written, not sent: the control plane may be unreachable exactly
// when a node is having delivery trouble, and an outcome held only in
// memory is a bounce the platform never hears about. The reporting
// loop drains these and deletes only what was accepted.
func (a *Agent) recordOutcome(_ context.Context, o Outcome) {
	// Field by field rather than the conversion staticcheck offers.
	// Outcome is what the agent DECIDED, ReportOutcome is what goes on
	// the wire, and the two are allowed to diverge - the same rule the
	// tree states for a stored shape against its wire shape. A
	// conversion compiles only while they stay identical, which turns
	// "allowed to differ" into "must not".
	//nolint:staticcheck // S1016: the two shapes are deliberately independent
	blob, err := json.Marshal(ReportOutcome{
		EmailID: o.EmailID, Recipient: o.Recipient,
		Delivered: o.Delivered, Permanent: o.Permanent, Reason: o.Reason,
	}, agentJSON)
	if err != nil {
		a.log.Error("relay node: could not encode an outcome", "err", err)

		return
	}
	// Keyed by message and recipient, so the same terminal result
	// recorded twice is one row rather than two reports of one bounce.
	key := o.EmailID + "|" + o.Recipient
	if err := a.spool.PutOutcome(key, blob); err != nil {
		a.log.Error("relay node: could not store an outcome", "err", err)
	}
}

// reportLoop drains pending outcomes to the control plane.
func (a *Agent) reportLoop(ctx context.Context) {
	tick := time.Tick(30 * time.Second)
	for {
		a.flushOutcomes(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick:
		}
	}
}

func (a *Agent) flushOutcomes(ctx context.Context) {
	keys, blobs, err := a.spool.Outcomes(200)
	if err != nil {
		a.log.Error("relay node: could not read pending outcomes", "err", err)

		return
	}

	if len(keys) == 0 {
		return
	}
	outcomes := make([]ReportOutcome, 0, len(blobs))
	for _, b := range blobs {
		var o ReportOutcome
		if uerr := json.Unmarshal(b, &o); uerr != nil {
			continue
		}
		outcomes = append(outcomes, o)
	}

	if err := a.ctrl.Report(ctx, a.nodeID, a.token, outcomes); err != nil {
		if ce, ok := errors.AsType[*ControlError](err); ok && ce.Fatal() {
			// The control plane will never take these. Dropping them
			// stops the node retrying the same batch forever, and the
			// log line is the only trace there will be.
			a.log.Error("relay node: outcomes refused permanently, dropping them",
				"count", len(outcomes), "err", err)
			if derr := a.spool.RemoveOutcomes(keys); derr != nil {
				a.log.Error("relay node: could not clear refused outcomes", "err", derr)
			}

			return
		}
		a.log.Warn("relay node: could not report outcomes, will retry",
			"count", len(outcomes), "err", err)

		return
	}

	// Deleted only after the control plane has taken them. The other
	// order loses a bounce whenever the request fails after the write.
	if err := a.spool.RemoveOutcomes(keys); err != nil {
		a.log.Error("relay node: could not clear reported outcomes", "err", err)

		return
	}
	a.log.Info("relay node: reported outcomes", "count", len(outcomes))
}

// Listener builds the SMTP server workers connect to.
func (a *Agent) Listener() (addr string, tlsCfg *tls.Config, backend *Backend) {
	return a.cfg.Addr, a.tlsCfg, &Backend{
		Spool:          a.spool,
		Log:            a.log,
		MaxMessageSize: a.cfg.MaxMessageSize,
		PeerName:       a.peerCN,
		Wake:           a.Deliverer.Wake,
	}
}

// ReceiveListener builds the MX-facing server, for a node that also
// receives.
//
// A different listener from the one above in every respect that
// matters: that one is a private transport between our own processes,
// with implicit TLS and a client certificate. This one takes mail
// from strangers with no authentication at all, so its only gate is
// the recipient domain list.
func (a *Agent) ReceiveListener(maxSize int64, ratePerMinute int) *ReceiveBackend {
	return &ReceiveBackend{
		Spool:  a.spool,
		Accept: a.accept,
		// The node's own scope, on top of the pushed list. Built here
		// rather than taken as an argument because it is the same
		// config value the enrolment sent, and two callers reading it
		// separately is how the two halves come to disagree.
		Local:          newDomainScope(a.cfg.Domains),
		Log:            a.log,
		MaxMessageSize: maxSize,
		Limiter:        iplimit.New(ratePerMinute, time.Minute),
		Wake:           a.forwardWakeup,
	}
}

// Start runs the delivery loop, the heartbeat and the sweeps.
func (a *Agent) Start(ctx context.Context) {
	safego.Go(a.log, "relay node: delivery loop", func() { a.Deliverer.Start(ctx) })
	safego.Go(a.log, "relay node: heartbeat", func() { a.heartbeatLoop(ctx) })
	safego.Go(a.log, "relay node: sweeps", func() { a.sweepLoop(ctx) })
	safego.Go(a.log, "relay node: outcome reports", func() { a.reportLoop(ctx) })
	if a.cfg.Pulls() {
		safego.Go(a.log, "relay node: claim loop", func() { a.pullLoop(ctx) })
	}
	// Started whether or not this node runs an MX. A node that had one
	// and no longer does still holds mail it accepted, and the loop is
	// what drains it.
	safego.Go(a.log, "relay node: inbound forwarding", func() { a.forwardLoop(ctx) })
}

func (a *Agent) heartbeatLoop(ctx context.Context) {
	interval := a.cfg.HeartbeatInterval
	if interval <= 0 {
		interval = 2 * time.Minute
	}
	tick := time.Tick(interval)

	lastStatus := ""
	for {
		res, err := a.ctrl.Heartbeat(ctx, a.nodeID, a.token, heartbeatReq{
			Version:        a.cfg.Version,
			AcceptETag:     a.accept.etagOf(),
			InboundEnabled: a.cfg.InboundEnabled,
			InboundQueued:  a.receivedCount(),
			Mode:           a.mode(),
		})
		switch err {
		case nil:
			if res.Status != lastStatus {
				// Say it once per change, not every two minutes. A
				// node waiting for approval is otherwise silent, and a
				// node that just got approved should say so.
				a.log.Info("relay node: status", "status", res.Status)
				lastStatus = res.Status
			}
			a.refreshAccept(res)
		default:
			if ce, ok := errors.AsType[*ControlError](err); ok && ce.Fatal() {
				// The control plane has forgotten us, or the token is
				// wrong. Retrying cannot fix either, and a node that
				// keeps trying buries the reason in a log.
				a.log.Error("relay node: the control plane no longer accepts this node - it will deliver what it has and take nothing new",
					"err", err)

				return
			}
			a.log.Warn("relay node: heartbeat failed, will retry", "err", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-tick:
		}
	}
}

// receivedCount is the forward queue depth for the heartbeat.
//
// Zero on error rather than a failed beat: liveness is what keeps
// this node in the delivery pool, and dropping out of it because a
// count could not be read would take the SENDING half down over a
// number on a console page.
func (a *Agent) receivedCount() int {
	n, err := a.spool.ReceivedCount()
	if err != nil {
		a.log.Warn("relay node: could not count received mail", "err", err)

		return 0
	}

	return n
}

// refreshAccept takes a new recipient domain list off a heartbeat.
//
// Keyed on the ETAG, never on whether the list came with it. An empty
// list is a real answer - an installation may have no verified
// domains, or have just removed its last one - and treating absence
// as "unchanged" would leave this node accepting mail for a name
// nobody owns any more.
func (a *Agent) refreshAccept(res *Heartbeat) {
	if res.AcceptETag == "" || res.AcceptETag == a.accept.etagOf() {
		return
	}
	a.accept.replace(res.AcceptDomains, res.AcceptETag)
	if err := a.accept.store(a.spool, res.AcceptDomains, res.AcceptETag); err != nil {
		// Not fatal: the list is live in memory either way. It only
		// means a restart before the next beat starts with the older
		// one, or with none.
		a.log.Warn("relay node: could not cache the recipient domain list", "err", err)
	}
	a.log.Info("relay node: recipient domain list updated", "domains", len(res.AcceptDomains))
}

func (a *Agent) sweepLoop(ctx context.Context) {
	tick := time.Tick(time.Hour)
	for {
		if n, err := a.spool.SweepOrphans(); err != nil {
			a.log.Warn("relay node: orphan sweep failed", "err", err)
		} else if n > 0 {
			a.log.Info("relay node: removed orphaned message bodies", "count", n)
		}

		if err := a.Deliverer.SweepExpired(ctx); err != nil {
			a.log.Warn("relay node: expiry sweep failed", "err", err)
		}

		if cert, _ := a.spool.Meta(metaCert); cert != "" {
			if err := a.renewIfDue(ctx, cert); err != nil {
				a.log.Warn("relay node: certificate renewal failed", "err", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick:
		}
	}
}

// CheckEnvironment reports the two things that decide whether a node
// can deliver at all, neither of which is fixable in code.
//
// Warnings, not errors. An operator mid-setup should still be able to
// bring the node up and watch it complain.
func (a *Agent) CheckEnvironment(ctx context.Context) {
	a.checkOutboundSMTP(ctx)
	a.checkReverseDNS(ctx)
}

// checkOutboundSMTP is the single most common reason a node delivers
// nothing. AWS, GCP, Azure, DigitalOcean and Hetzner all block
// outbound 25 by default, and the symptom is every connection timing
// out - which looks like a slow internet rather than a closed port.
func (a *Agent) checkOutboundSMTP(ctx context.Context) {
	if a.cfg.SMTPPort != 25 {
		return
	}
	// gmail-smtp-in.l.google.com is about as reliably reachable as
	// port 25 gets. A refusal is informative, a timeout is the block.
	probe, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(probe, "tcp4", "gmail-smtp-in.l.google.com:25")
	if err != nil {
		a.log.Warn("relay node: could not open an outbound connection on port 25 - most hosting providers block it by default and unblocking is a support request. This node will not deliver anything until that is resolved",
			"err", err)

		return
	}
	_ = conn.Close()
	a.log.Info("relay node: outbound port 25 is open")
}

// checkReverseDNS compares our address's PTR with the name we
// announce. Gmail and Outlook reject on a mismatch before looking at
// the message, and the node cannot fix its own PTR - that is the
// hosting provider's control panel.
func (a *Agent) checkReverseDNS(ctx context.Context) {
	probe, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	addr, err := publicAddress(probe)
	if err != nil {
		a.log.Warn("relay node: could not determine this node's outbound address, so its PTR was not checked", "err", err)

		return
	}

	// Behind NAT - a container, a home router - the socket's source
	// address is a private one, and its "PTR" is whatever the local
	// resolver invents (Docker answers the container id). Comparing
	// that against relay_node.hostname is a false alarm on every
	// start, so say what is actually known instead: the check cannot
	// run from here, and the PTR that matters is the public address's.
	if ip, perr := netip.ParseAddr(addr); perr == nil && !safedial.AddrAllowed(ip) {
		a.log.Info("relay node: outbound address is private (NAT), so its PTR cannot be checked from here - verify that the PUBLIC address's PTR matches relay_node.hostname, e.g. `dig -x <public ip>`",
			"local_address", addr, "expected", a.cfg.Hostname)

		return
	}

	names, err := net.DefaultResolver.LookupAddr(probe, addr)
	if err != nil || len(names) == 0 {
		a.log.Warn("relay node: this address has no PTR record. Large receivers reject mail from an address with no reverse DNS, whatever else is configured",
			"address", addr, "expected", a.cfg.Hostname)

		return
	}
	want := strings.TrimSuffix(strings.ToLower(a.cfg.Hostname), ".")
	for _, n := range names {
		if strings.TrimSuffix(strings.ToLower(n), ".") == want {
			a.log.Info("relay node: reverse DNS matches the announced name", "address", addr, "ptr", n)

			return
		}
	}
	a.log.Warn("relay node: the PTR record does not match relay_node.hostname. Receivers compare the two and reject on a mismatch",
		"address", addr, "ptr", strings.Join(names, ","), "expected", a.cfg.Hostname)
}

// publicAddress finds the address this node connects out from,
// without sending anything: a UDP socket to a public address picks a
// route and a source address, which is what a receiver will see.
func publicAddress(ctx context.Context) (string, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp4", "8.8.8.8:53")
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close() }()
	host, _, err := net.SplitHostPort(conn.LocalAddr().String())

	return host, err
}

func portOf(addr string) int {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 2587
	}
	var n int
	if _, err := fmt.Sscanf(port, "%d", &n); err != nil || n == 0 {
		return 2587
	}

	return n
}
