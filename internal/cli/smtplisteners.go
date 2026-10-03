// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/emersion/go-smtp"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/iplimit"
	"github.com/yousysadmin/mailyard/internal/core/proxylisten"
	"github.com/yousysadmin/mailyard/internal/core/safego"
	"github.com/yousysadmin/mailyard/internal/core/tlsbuild"
	"github.com/yousysadmin/mailyard/internal/domain/certificate"
	"github.com/yousysadmin/mailyard/internal/domain/email"
	"github.com/yousysadmin/mailyard/internal/domain/inbound"
	"github.com/yousysadmin/mailyard/internal/domain/sandbox"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	"github.com/yousysadmin/mailyard/internal/domain/submission"
	smodel "github.com/yousysadmin/mailyard/internal/models/setting"
)

// smtpListeners are the SMTP servers a node accepts mail on. Either is
// nil when its listener is off or the role does not run it.
type smtpListeners struct {
	log        *slog.Logger
	submission *smtp.Server
	inbound    *smtp.Server
}

// startSMTP builds and binds the listeners this role runs.
//
// The SMTP listeners are ingress, so they follow the api role rather
// than the worker one: they accept messages and queue them, exactly
// like POST /api/v1/emails/send does.
func startSMTP(r role, cfg *env.Config, rt *env.Runtime, st *store.Store, tb *tlsbuild.Builder, log *slog.Logger) (*smtpListeners, error) {
	l := &smtpListeners{log: log}

	// SMTP submission: optional listener that feeds the same send
	// pipeline. Authenticated by submission credentials or API keys,
	// so it needs nothing beyond the store and the email service.
	if r.api && cfg.Submission.Enabled {
		backend := &submission.Backend{
			Credentials:    st.SMTPCredential,
			Keys:           st.APIKey,
			Projects:       st.Project,
			Sender:         email.NewService(rt),
			Sandbox:        &sandbox.Service{Store: st.Sandbox, Settings: rt.Settings, Log: log, All: st},
			Log:            log,
			MaxMessageSize: cfg.Submission.MaxMessageSize,
			Limiter:        iplimit.New(cfg.Submission.RatePerMinute, time.Minute),
			Maintenance:    func() bool { return rt.Settings.Bool(smodel.KeyMaintenanceMode) },
		}

		submissionTLS, terr := tb.Build(certificate.ListenerSubmission, cfg.Submission.TLS.Enabled)
		if terr != nil {
			return nil, fmt.Errorf("submission tls: %w", terr)
		}

		l.submission = submission.NewServer(backend, cfg.Submission.Addr, cfg.Submission.Hostname, submissionTLS)
		if lerr := l.serve(l.submission, "smtp submission", cfg.Submission.Addr, submissionTLS != nil,
			cfg.Submission.ProxyProtocol); lerr != nil {
			return nil, lerr
		}
	}

	// Inbound MX listener: receives mail for verified domains and
	// stores it per project, emitting inbound.received webhooks.
	if r.api && cfg.Inbound.Enabled {
		// The same pipeline the relay-node forwarding endpoint builds.
		// One constructor, because the two transports differ in how
		// bytes arrive and in nothing else.
		svc := inbound.NewService(rt)
		backend := &inbound.Backend{
			Service:        svc,
			MaxMessageSize: cfg.Inbound.MaxMessageSize,
			Limiter:        iplimit.New(cfg.Inbound.RatePerMinute, time.Minute),
		}

		inboundTLS, terr := tb.Build(certificate.ListenerInbound, cfg.Inbound.TLS.Enabled)
		if terr != nil {
			return nil, fmt.Errorf("inbound tls: %w", terr)
		}

		l.inbound = inbound.NewServer(backend, cfg.Inbound.Addr, cfg.Inbound.Hostname, inboundTLS)
		if lerr := l.serve(l.inbound, "inbound smtp", cfg.Inbound.Addr, inboundTLS != nil,
			cfg.Inbound.ProxyProtocol); lerr != nil {
			return nil, lerr
		}
	}

	return l, nil
}

// serve binds the port before returning and only then hands the
// listener to a goroutine.
//
// Binding inside the goroutine surfaced a refused bind as one log
// line while boot carried on reporting success - and the defaults
// are privileged ports, so "permission denied" is ordinary. A port
// we cannot take is a failure to start, not a warning.
func (l *smtpListeners) serve(srv *smtp.Server, kind, addr string, secure bool, proxy env.ProxyProtocolConfig) error {
	ln, lerr := net.Listen("tcp", addr)
	if lerr != nil {
		return fmt.Errorf("%s listener on %s: %w", kind, addr, lerr)
	}

	bound := ln.Addr().String()

	// The PROXY wrap goes outside, because the header is the first
	// thing on the wire - before EHLO, and before STARTTLS upgrades
	// the same connection. A wrap on the inside would be reading it
	// out of the middle of a session.
	//
	// A malformed trusted list fails here, at the bind, which is
	// where a port we cannot take fails too. Validate has already
	// refused an empty list.
	if proxy.Enabled {
		if ln, lerr = proxylisten.Wrap(ln, proxy.Trusted); lerr != nil {
			return fmt.Errorf("%s proxy protocol: %w", kind, lerr)
		}
	}

	l.log.Info(kind+" listening", "addr", bound, "starttls", secure, "proxy_protocol", proxy.Enabled)

	safego.Go(l.log, kind+": accept loop", func() {
		if serr := srv.Serve(ln); serr != nil && !errors.Is(serr, smtp.ErrServerClosed) {
			l.log.Error(kind+" stopped", "err", serr)
		}
	})

	return nil
}

// stop shuts both listeners down, each bounded by timeout.
func (l *smtpListeners) stop(timeout time.Duration) {
	stopSMTP(l.submission, timeout)
	stopSMTP(l.inbound, timeout)
}

// stopSMTP drains one listener and closes it when the drain runs out
// of time. A nil server is a listener this node never started.
func stopSMTP(srv *smtp.Server, timeout time.Duration) {
	if srv == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if serr := srv.Shutdown(ctx); serr != nil {
		_ = srv.Close()
	}
}
