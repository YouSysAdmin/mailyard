// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relayagent

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/mail"
	"slices"
	"strings"
	"time"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
)

// Backend accepts messages from delivery workers.
//
// There is no AUTH here at all, and that is not an omission. The
// listener runs implicit TLS with RequireAndVerifyClientCert against
// our own authority, so a connection that reaches this code has
// already proved it holds a certificate we issued. A password would
// add a second, weaker answer to a question already settled.
type Backend struct {
	Spool          *Spool
	Log            *slog.Logger
	MaxMessageSize int64

	// PeerName is the certificate name an inbound peer must carry.
	// Verified again here, per session, because the TLS layer proves
	// the certificate is OURS and this proves it is a worker's - a
	// node certificate is also ours and must not be able to connect
	// into another node.
	PeerName string

	// Wake nudges the delivery loop when something arrives, so a
	// message is not waiting out a poll interval for no reason.
	Wake func()
}

// NewSession implements smtp.Backend for the worker-facing listener.
// The client certificate is the identity, so a connection without one
// is refused here rather than at MAIL.
func (b *Backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	state, ok := c.TLSConnectionState()
	if !ok {
		// Cannot happen behind an implicit-TLS listener, and is
		// refused rather than trusted if it ever does.
		return nil, &smtp.SMTPError{Code: 530, Message: "TLS required"}
	}

	if err := b.checkPeer(state); err != nil {
		b.Log.Warn("relay node: refused a peer", "err", err, "remote", c.Conn().RemoteAddr().String())

		return nil, &smtp.SMTPError{Code: 530, Message: "client certificate is not accepted"}
	}

	return &session{backend: b, remote: c.Conn().RemoteAddr().String()}, nil
}

func (b *Backend) checkPeer(state tls.ConnectionState) error {
	if len(state.PeerCertificates) == 0 {
		return errors.New("no client certificate")
	}

	if b.PeerName == "" {
		// Refuse rather than accept anything the CA signed. An empty
		// expected name is a misconfigured node, and the failure mode
		// of guessing here is admitting another node.
		return errors.New("node has no expected peer name configured")
	}
	leaf := state.PeerCertificates[0]
	if leaf.Subject.CommonName == b.PeerName {
		return nil
	}

	if slices.Contains(leaf.DNSNames, b.PeerName) {
		return nil
	}

	// A node certificate carries ServerAuth only, so this is belt and
	// braces - but it is the check that stops one node connecting
	// into another if the key usages are ever loosened.
	return fmt.Errorf("peer certificate %q is not %q", leaf.Subject.CommonName, b.PeerName)
}

type session struct {
	backend *Backend
	remote  string
	from    string
	to      []string

	// accepted counts messages spooled on this session, so Logout can
	// say when a worker connected and handed over NOTHING. A
	// protocol-level refusal - a malformed MAIL FROM, say - is
	// answered inside the library, no hook of ours runs, and without
	// this the node's log stays empty while the platform's blames the
	// node.
	accepted int
}

// AuthMechanisms implements smtp.Session and offers none - mutual TLS
// already established who is calling.
func (s *session) AuthMechanisms() []string { return nil }

// Auth implements smtp.Session and always refuses, since this listener
// authenticates by certificate.
func (s *session) Auth(string) (sasl.Server, error) {
	return nil, smtp.ErrAuthUnsupported
}

// Mail implements smtp.Session, recording the envelope sender for the
// message being handed over.
func (s *session) Mail(from string, _ *smtp.MailOptions) error {
	// An empty MAIL FROM is legal and means a null return path, which
	// is what a bounce report itself carries.
	s.from = from
	s.to = nil

	return nil
}

// Rcpt implements smtp.Session, adding one recipient this node is
// being asked to deliver to.
func (s *session) Rcpt(to string, _ *smtp.RcptOptions) error {
	addr := smtpclient.EnvelopeAddress(to)
	if _, err := mail.ParseAddress(addr); err != nil {
		s.backend.Log.Warn("relay node: refused a recipient",
			"rcpt", to, "remote", s.remote, "reason", "not a valid address")

		return &smtp.SMTPError{Code: 501, Message: "recipient is not a valid address"}
	}
	s.to = append(s.to, addr)

	return nil
}

// Data implements smtp.Session, spooling the message before it is
// acknowledged - written down first, so a 250 is never a promise the
// disk did not keep.
func (s *session) Data(r io.Reader) error {
	if len(s.to) == 0 {
		s.backend.Log.Warn("relay node: refused a message", "remote", s.remote, "reason", "no recipients")

		return &smtp.SMTPError{Code: 503, Message: "no recipients"}
	}
	limit := s.backend.MaxMessageSize
	if limit <= 0 {
		limit = 50 << 20
	}
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return &smtp.SMTPError{Code: 451, Message: "could not read message"}
	}

	if int64(len(body)) > limit {
		s.backend.Log.Warn("relay node: refused a message",
			"remote", s.remote, "reason", "too large", "limit", limit)

		return &smtp.SMTPError{Code: 552, Message: "message too large"}
	}

	// Group by recipient domain. Mail exchangers are resolved per
	// domain, so a message to two domains is two deliveries with the
	// same bytes - and splitting here rather than at delivery keeps
	// the retry schedule per destination, which is what a receiver
	// throttling us actually applies to.
	byDomain := map[string][]string{}
	for _, rcpt := range s.to {
		_, host, ok := strings.CutLast(rcpt, "@")
		if !ok {
			continue
		}

		d := strings.ToLower(host)
		byDomain[d] = append(byDomain[d], rcpt)
	}
	if len(byDomain) == 0 {
		s.backend.Log.Warn("relay node: refused a message", "remote", s.remote, "reason", "no usable recipients")

		return &smtp.SMTPError{Code: 501, Message: "no usable recipients"}
	}

	emailID := headerValue(body, smtpclient.HeaderEmailID)
	now := time.Now().UTC()

	for domain, rcpts := range byDomain {
		m := &Message{
			ID:           ids.New(),
			EmailID:      emailID,
			EnvelopeFrom: s.from,
			Domain:       domain,
			Recipients:   rcpts,
			AcceptedAt:   now,
			NextAttempt:  now,
		}
		if err := s.backend.Spool.Put(m, body); err != nil {
			s.backend.Log.Error("relay node: could not spool a message", "err", err)

			// 451 rather than 550: the worker should try again, and
			// with another node if there is one. Accepting a message
			// we could not write down would lose it outright.
			return &smtp.SMTPError{Code: 451, Message: "could not queue message"}
		}
	}

	s.accepted++
	s.backend.Log.Info("relay node: accepted",
		"email_id", emailID, "recipients", len(s.to), "domains", len(byDomain), "bytes", len(body))
	if s.backend.Wake != nil {
		s.backend.Wake()
	}

	return nil
}

// Reset implements smtp.Session, clearing the envelope between
// messages on one connection.
func (s *session) Reset() { s.from = ""; s.to = nil }

// Logout implements smtp.Session.
//
// A session that handed over nothing is worth a line: the only clients
// here are our own workers and the console's Test button, and a worker
// never connects idly - so zero accepted messages means either a
// deliberate dial-and-quit test, or a command the library refused
// before any hook of ours could see it. mail_seen narrows it: false
// means the session died before or AT the MAIL command, which is where
// a malformed envelope sender lands.
func (s *session) Logout() error {
	if s.accepted == 0 {
		s.backend.Log.Info("relay node: session ended without a message",
			"remote", s.remote, "mail_seen", s.from != "" || len(s.to) > 0)
	}

	return nil
}

// headerValue pulls one header out of raw message bytes.
//
// A deliberate minimum: the node must not parse and re-render the
// message, because those bytes are DKIM-signed by whoever built them
// and this process cannot re-sign. Reading a header is safe, rewriting one is not.
func headerValue(body []byte, name string) string {
	needle := strings.ToLower(name) + ":"
	// Headers end at the first blank line. Scanning past it could
	// find the same string in the body.
	for line := range strings.SplitSeq(string(headerBlock(body)), "\n") {
		trimmed := strings.TrimRight(line, "\r")
		if strings.HasPrefix(strings.ToLower(trimmed), needle) {
			return strings.TrimSpace(trimmed[len(needle):])
		}
	}

	return ""
}

// headerBlock is the message up to its first blank line, or all of it
// when there is none.
func headerBlock(body []byte) []byte {
	if head, _, ok := bytes.Cut(body, []byte("\r\n\r\n")); ok {
		return head
	}

	if head, _, ok := bytes.Cut(body, []byte("\n\n")); ok {
		return head
	}

	return body
}

// NewServer builds the listener. Implicit TLS: the whole session is
// encrypted from the first byte, so there is no cleartext phase to
// downgrade and no STARTTLS to strip.
func NewServer(b *Backend, addr, hostname string, tlsCfg *tls.Config) *smtp.Server {
	srv := smtp.NewServer(b)
	srv.Addr = addr
	srv.Domain = hostname
	srv.ReadTimeout = 5 * time.Minute
	srv.WriteTimeout = 5 * time.Minute
	srv.MaxMessageBytes = b.MaxMessageSize
	srv.AllowInsecureAuth = false
	srv.TLSConfig = tlsCfg
	// Into OUR log, not the library's stderr default: a failed
	// handshake on this listener is a worker with the wrong
	// certificate, which is exactly the thing somebody will be
	// looking for here.
	srv.ErrorLog = NewSMTPLogger(b.Log, "worker")

	return srv
}

// PeerPool builds the trust root a node verifies workers against.
func PeerPool(caPEM string) (*x509.CertPool, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(caPEM)) {
		return nil, errors.New("relay ca bundle did not parse")
	}

	return pool, nil
}
