// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// Direct delivery: handing a message to the recipient's own mail
// exchanger rather than to a configured relay.
//
// Everything else in this package sends THROUGH somebody - a tenant's
// postfix, SES, a shared server. Send builds the message from a
// Message and may sign it on the way out. Here neither is true. A
// relay node receives bytes that were already composed and signed
// somewhere else, so the one rule that matters is that those bytes go
// out unchanged: re-rendering them through Build would rewrite headers
// and break a DKIM signature the sender cannot re-apply.
//
// Hence Raw. It is deliberately not a Message.

// Raw is a composed message: an envelope and the exact bytes to
// transmit, byte for byte.
type Raw struct {
	// EnvelopeFrom is the MAIL FROM. Empty is legal and means a null
	// return path, which is what a bounce report itself carries.
	EnvelopeFrom string

	// To are the envelope recipients. For direct delivery they should
	// share a destination domain, because the targets were resolved
	// from one.
	To []string

	// Data is the message, headers and body, exactly as it will be
	// written after DATA.
	Data []byte
}

// TLS outcomes, recorded rather than enforced. See DirectConfig.
const (
	// TLSNone means the session stayed in cleartext, either because
	// the peer offered no STARTTLS or because the handshake failed.
	TLSNone = "none"

	// TLSUnverified means STARTTLS succeeded but the certificate did
	// not verify. This is the ordinary case on the public internet and
	// is still worth far more than cleartext.
	TLSUnverified = "unverified"

	// TLSVerified means STARTTLS succeeded and the chain verified
	// against the host roots for the name we dialed.
	TLSVerified = "verified"
)

// DirectConfig tunes one direct delivery.
type DirectConfig struct {
	// HELO is the name we announce. It has to match the PTR of the
	// address we connect from or large receivers reject before
	// looking at anything else.
	HELO string

	// Port is the destination port. Zero means 25. Tests set it.
	Port int

	// Network restricts dialing. Empty means "tcp4".
	//
	// Not "tcp". A box with a AAAA address will happily prefer IPv6,
	// where receivers demand a PTR and SPF coverage that a v4 setup
	// does not have, and the mail is refused for reasons that look
	// nothing like the cause.
	Network string

	// Timeout bounds one connection attempt.
	Timeout time.Duration

	// DialContext overrides how the connection is made. A test seam,
	// and the hook a future SOCKS or bound-address option needs.
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)

	// RootCAs overrides the roots used when checking a peer
	// certificate. Nil uses the host store. Only affects whether the
	// outcome is recorded as verified - it never refuses a delivery.
	RootCAs *x509.CertPool
}

func (c DirectConfig) port() int {
	if c.Port == 0 {
		return 25
	}

	return c.Port
}

func (c DirectConfig) network() string {
	if c.Network == "" {
		return "tcp4"
	}

	return c.Network
}

func (c DirectConfig) timeout() time.Duration {
	if c.Timeout <= 0 {
		return 5 * time.Minute
	}

	return c.Timeout
}

func (c DirectConfig) dial(ctx context.Context, addr string) (net.Conn, error) {
	if c.DialContext != nil {
		return c.DialContext(ctx, c.network(), addr)
	}

	d := &net.Dialer{Timeout: c.timeout()}

	return d.DialContext(ctx, c.network(), addr)
}

// DirectResult reports what happened, per recipient.
//
// A message to several recipients at one domain routinely has some
// accepted and some refused, and collapsing that into a single error -
// which is what Send does, failing on the first bad RCPT - would make
// the node either bounce the whole batch or deliver it twice.
type DirectResult struct {
	// Host is the target that answered. Empty when none did.
	Host string

	// TLS is one of the TLS* constants above.
	TLS string

	// Accepted are the recipients the peer took.
	Accepted []string

	// Rejected maps a recipient to why it was refused. The values are
	// *SendError, so Permanent() decides bounce against retry.
	Rejected map[string]error
}

// SendDirect delivers msg to the first target that will take it.
//
// Targets are tried in order and a transient failure moves to the
// next, which is what an MX preference list is for. A permanent
// rejection does not: the next exchanger for a domain is the same
// mail system and will say the same thing.
//
// The returned error is non-nil only when no target could be reached
// at all. A peer that answered and refused recipients is a successful
// call with those recipients in Rejected.
func SendDirect(ctx context.Context, cfg DirectConfig, hosts []string, msg *Raw) (*DirectResult, error) {
	if len(hosts) == 0 {
		return nil, errors.New("no mail exchangers to try")
	}

	if len(msg.To) == 0 {
		return nil, errors.New("no recipients")
	}

	var lastErr error
	for _, host := range hosts {
		res, err := sendDirectTo(ctx, cfg, host, msg)
		if err == nil {
			return res, nil
		}

		lastErr = err

		// A permanent refusal from the exchanger itself - not from a
		// recipient - is the domain's answer, not this host's. Trying
		// its siblings asks the same mail system the same question.
		if se, ok := errors.AsType[*SendError](err); ok && se.Permanent() {
			return nil, err
		}

		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}

	return nil, fmt.Errorf("no mail exchanger accepted the message: %w", lastErr)
}

func sendDirectTo(ctx context.Context, cfg DirectConfig, host string, msg *Raw) (*DirectResult, error) {
	res := &DirectResult{Host: host, TLS: TLSNone, Rejected: map[string]error{}}

	client, state, err := dialDirect(ctx, cfg, host)
	if err != nil {
		return nil, err
	}

	defer func() { _ = client.Close() }()
	res.TLS = state

	if err := client.Mail(EnvelopeAddress(msg.EnvelopeFrom)); err != nil {
		return nil, wrapSendError("MAIL FROM", msg.EnvelopeFrom, fmt.Errorf("smtp mail from failed: %w", err))
	}

	for _, addr := range msg.To {
		rcpt := EnvelopeAddress(addr)
		if err := client.Rcpt(rcpt); err != nil {
			// Per recipient, not fatal. The rest of the batch may
			// still be deliverable on this same connection.
			res.Rejected[rcpt] = wrapSendError("RCPT TO", rcpt, fmt.Errorf("smtp rcpt to failed: %w", err))
			continue
		}

		res.Accepted = append(res.Accepted, rcpt)
	}

	if len(res.Accepted) == 0 {
		// Nobody was accepted, so there is nothing to send. Report the
		// result rather than an error: every recipient already carries
		// its own reason, and a caller that sees an error here would
		// retry a batch that was permanently refused.
		_ = client.Quit()

		return res, nil
	}

	w, err := client.Data()
	if err != nil {
		return nil, wrapSendError("DATA", "", fmt.Errorf("smtp data failed: %w", err))
	}

	if _, err := w.Write(msg.Data); err != nil {
		return nil, fmt.Errorf("smtp write failed: %w", err)
	}

	// The end-of-data reply is the one that means delivered. Anything
	// before it was the peer agreeing to listen.
	if err := w.Close(); err != nil {
		return nil, wrapSendError("DATA", "", fmt.Errorf("smtp close failed: %w", err))
	}

	_ = client.Quit()

	return res, nil
}

// dialDirect opens a session and upgrades it opportunistically.
//
// Opportunistic means exactly what RFC 7435 says: prefer TLS, accept
// what you get, never refuse to deliver over it. Verification failure
// is recorded, not enforced - most of the internet's mail exchangers
// present certificates that do not verify for the name their MX record
// gives, and a sender that insists loses mail that every other sender
// delivers.
func dialDirect(ctx context.Context, cfg DirectConfig, host string) (*smtp.Client, string, error) {
	client, err := helloTo(ctx, cfg, host)
	if err != nil {
		return nil, TLSNone, err
	}

	if ok, _ := client.Extension("STARTTLS"); !ok {
		return client, TLSNone, nil
	}

	verified := false
	tlsCfg := &tls.Config{
		ServerName: host,
		// Verification happens in the callback so a failure can be
		// recorded instead of ending the delivery. Leaving this false
		// would make an unverifiable peer a hard error.
		InsecureSkipVerify: true, //nolint:gosec // see VerifyConnection
		VerifyConnection: func(cs tls.ConnectionState) error {
			verified = verifyChain(cs, host, cfg.RootCAs)

			return nil
		},
		MinVersion: tls.VersionTLS12,
	}
	if err := client.StartTLS(tlsCfg); err != nil {
		// The connection is spent - a failed handshake leaves nothing
		// to talk on. Redial in the clear rather than lose the message
		// to a peer whose TLS is broken.
		_ = client.Close()
		plain, perr := helloTo(ctx, cfg, host)
		if perr != nil {
			return nil, TLSNone, perr
		}

		return plain, TLSNone, nil
	}

	if verified {
		return client, TLSVerified, nil
	}

	return client, TLSUnverified, nil
}

func helloTo(ctx context.Context, cfg DirectConfig, host string) (*smtp.Client, error) {
	addr := net.JoinHostPort(host, fmt.Sprint(cfg.port()))
	conn, err := cfg.dial(ctx, addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}

	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()

		return nil, fmt.Errorf("smtp greeting from %s: %w", addr, err)
	}

	helo := cfg.HELO
	if helo == "" {
		helo = "localhost"
	}

	if err := client.Hello(helo); err != nil {
		_ = client.Close()

		return nil, wrapSendError("EHLO", "", fmt.Errorf("smtp ehlo to %s: %w", addr, err))
	}

	return client, nil
}

// verifyChain reports whether the peer's chain would have verified,
// mirroring what crypto/tls does by default.
func verifyChain(cs tls.ConnectionState, host string, roots *x509.CertPool) bool {
	if len(cs.PeerCertificates) == 0 {
		return false
	}

	inter := x509.NewCertPool()
	for _, c := range cs.PeerCertificates[1:] {
		inter.AddCert(c)
	}

	_, err := cs.PeerCertificates[0].Verify(x509.VerifyOptions{
		DNSName:       strings.TrimSuffix(host, "."),
		Roots:         roots,
		Intermediates: inter,
	})

	return err == nil
}
