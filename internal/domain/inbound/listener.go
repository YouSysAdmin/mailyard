// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package inbound

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/mail"
	"strings"
	"time"

	"github.com/emersion/go-smtp"

	"github.com/yousysadmin/mailyard/internal/core/safego"
	dmodel "github.com/yousysadmin/mailyard/internal/models/domain"

	"github.com/yousysadmin/mailyard/internal/core/iplimit"
)

// Backend is the MX-facing smtp.Backend: no AUTH (the internet
// delivers here), recipients gated on verified domains at RCPT time.
type Backend struct {
	Service        *Service
	MaxMessageSize int64
	Limiter        *iplimit.Limiter
}

// NewSession builds a Session.
func (b *Backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	remote := ""
	if c != nil && c.Conn() != nil {
		remote = c.Conn().RemoteAddr().String()
	}

	ip := remote
	if host, _, err := net.SplitHostPort(remote); err == nil {
		ip = host
	}

	if !b.Limiter.Allow(ip) {
		b.Service.Log.Warn("inbound: rate limited", "ip", ip)

		return nil, &smtp.SMTPError{Code: 421, EnhancedCode: smtp.EnhancedCode{4, 7, 0}, Message: "rate limit exceeded"}
	}

	// go-smtp fills c.helo before calling NewSession, so the announced
	// name is available here and stays fixed for the session.
	helo := ""
	if c != nil {
		helo = c.Hostname()
	}

	return &session{backend: b, ip: ip, helo: helo}, nil
}

type session struct {
	backend *Backend
	ip      string

	// helo is what the client announced. SPF falls back to it when the
	// envelope sender is null, which is how bounces arrive.
	helo string
	from string
	to   []string

	// domain is resolved from the first accepted recipient. Later
	// recipients must route to the same project domain - standard
	// MTAs retry rejected recipients in a separate transaction.
	domain *dmodel.Domain
}

// Reset implements smtp.Session, clearing the envelope between
// messages on one connection.
func (s *session) Reset() {
	s.from = ""
	s.to = nil
	s.domain = nil
}

// Logout implements smtp.Session.
func (s *session) Logout() error { return nil }

// Mail implements smtp.Session, recording the envelope sender.
func (s *session) Mail(from string, _ *smtp.MailOptions) error {
	if from != "" {
		if addr, err := mail.ParseAddress(from); err == nil {
			s.from = addr.Address
		} else {
			s.from = from
		}
	}

	return nil
}

// Rcpt refuses recipients whose domain no project has verified,
// failing before DATA so unwanted mail costs no bandwidth.
func (s *session) Rcpt(to string, _ *smtp.RcptOptions) error {
	addr := to
	if parsed, err := mail.ParseAddress(to); err == nil {
		addr = parsed.Address
	}

	addr = strings.ToLower(strings.TrimSpace(addr))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	d, err := s.backend.Service.ResolveDomain(ctx, addr)
	if err != nil {
		s.backend.Service.Log.Error("inbound: domain lookup failed", "err", err)

		return &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 3, 0}, Message: "temporary failure"}
	}

	if d == nil {
		return &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 1, 1}, Message: "relay not permitted"}
	}

	if s.domain != nil && s.domain.ID != d.ID {
		return &smtp.SMTPError{Code: 452, EnhancedCode: smtp.EnhancedCode{4, 5, 3}, Message: "recipients span domains, retry separately"}
	}

	s.domain = d
	s.to = append(s.to, addr)

	return nil
}

// Data implements smtp.Session, reading and ingesting one message.
func (s *session) Data(r io.Reader) (err error) {
	// This listener takes mail from anyone on the internet and hands
	// it to a MIME parser, so it is the likeliest place in the process
	// to meet input nobody anticipated. go-smtp does not guard its
	// session goroutines, which would make a parser panic on one
	// hostile message a remote kill of the whole binary. Answer 451
	// instead: the sender retries, and the operator gets a stack trace
	// naming the message.
	defer func() {
		if rec := recover(); rec != nil {
			safego.Report(s.backend.Service.Log, "inbound: data",
				rec, "from", s.from, "remote_ip", s.ip)
			err = &smtp.SMTPError{
				Code:         451,
				EnhancedCode: smtp.EnhancedCode{4, 3, 0},
				Message:      "temporary failure processing message",
			}
		}
	}()
	if s.domain == nil || len(s.to) == 0 {
		return &smtp.SMTPError{Code: 554, EnhancedCode: smtp.EnhancedCode{5, 5, 1}, Message: "no valid recipients"}
	}

	limit := s.backend.MaxMessageSize
	if limit <= 0 {
		limit = 26214400
	}

	lr := &io.LimitedReader{R: r, N: limit + 1}
	raw, err := io.ReadAll(lr)
	if err != nil {
		return &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 3, 0}, Message: "read error"}
	}

	if int64(len(raw)) > limit {
		return &smtp.SMTPError{Code: 552, EnhancedCode: smtp.EnhancedCode{5, 3, 4}, Message: "message size exceeds limit"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, ierr := s.backend.Service.Ingest(ctx, s.domain, s.from,
		append([]string(nil), s.to...), raw, Conn{IP: s.ip, HELO: s.helo})
	switch {
	case ierr == nil:
		return nil
	case errors.Is(ierr, ErrDuplicate):
		// Idempotent success: the upstream MTA is retrying a message
		// that already landed.
		return nil
	case errors.Is(ierr, ErrSenderSuppressed):
		return &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 7, 1}, Message: "sender suppressed"}
	case errors.Is(ierr, ErrDMARCFail):
		return &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 7, 1}, Message: "dmarc policy rejects this message"}
	default:
		s.backend.Service.Log.Error("inbound: ingest failed", "ip", s.ip, "err", ierr)

		return &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 3, 0}, Message: "temporary failure"}
	}
}

// NewServer builds the MX listener. STARTTLS is offered when tlsCfg
// is non-nil - inbound MTAs use opportunistic TLS, so no config means
// cleartext transport, never a refusal.
func NewServer(b *Backend, addr, hostname string, tlsCfg *tls.Config) *smtp.Server {
	srv := smtp.NewServer(b)
	srv.Addr = addr
	srv.Domain = hostname
	srv.ReadTimeout = 60 * time.Second
	srv.WriteTimeout = 60 * time.Second
	srv.MaxMessageBytes = b.MaxMessageSize
	srv.MaxRecipients = 100
	srv.EnableSMTPUTF8 = true
	srv.TLSConfig = tlsCfg

	return srv
}
