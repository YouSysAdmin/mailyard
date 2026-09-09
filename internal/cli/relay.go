// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package cli

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/emersion/go-smtp"
	"github.com/spf13/cobra"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/proxylisten"
	"github.com/yousysadmin/mailyard/internal/core/safego"
	"github.com/yousysadmin/mailyard/internal/relayagent"
	"github.com/yousysadmin/mailyard/pkg"
)

// newRelayCmd builds the relay node role.
//
// A subcommand like api and worker, and for the same reason: the role
// is a word in argv, which every orchestrator already sets per
// container, rather than a config key that makes one machine's file differ from the rest.
//
// Unlike those two, this role does NOT go through runServe. A node
// has no database, no stores, no crypto service and no HTTP surface -
// sharing that bootstrap would mean threading "except not this, and
// not that" through all of it. It is a different process that happens
// to ship in the same binary.
func newRelayCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "relay",
		Short: "Start a relay node: deliver queued mail straight to recipient mail exchangers",
		Long: "Start a relay node - an egress machine that accepts messages from\n" +
			"Mailyard delivery workers over mutual TLS and delivers them to the\n" +
			"recipient's own mail exchangers from this host's address.\n\n" +
			"It holds no database credentials. On first run it enrols with\n" +
			"relay_node.control_url using relay_node.enroll_token and receives a\n" +
			"certificate; after that the certificate is its identity.\n\n" +
			"Outbound port 25 must be open and this host's PTR record must match\n" +
			"relay_node.hostname, or nothing will be delivered. Both are checked\n" +
			"at startup.",
		RunE: runRelay,
	}
}

func runRelay(cmd *cobra.Command, _ []string) error {
	configPath, _ := cmd.Flags().GetString("config")

	cfg, err := env.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// ValidateNode, not Validate: a node must not be asked for a
	// database DSN or an encryption key it should never hold.
	if err := cfg.ValidateNode(); err != nil {
		return fmt.Errorf("config invalid: %w", err)
	}

	log, err := buildLogger(cfg.Logging)
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	slog.SetDefault(log)
	log.Info("mailyard starting", "role", "relay", "config", cfg.Source,
		"hostname", cfg.RelayNode.Hostname, "spool", cfg.RelayNode.SpoolDir)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	agent, err := relayagent.New(ctx, relayagent.Config{
		ControlURL:          cfg.RelayNode.ControlURL,
		EnrollToken:         cfg.RelayNode.EnrollToken,
		ServerGroup:         cfg.RelayNode.ServerGroup,
		Domains:             cfg.RelayNode.Domains,
		Addr:                cfg.RelayNode.Addr,
		Hostname:            cfg.RelayNode.Hostname,
		SpoolDir:            cfg.RelayNode.SpoolDir,
		MaxLifetime:         cfg.RelayNode.MaxLifetime,
		HeartbeatInterval:   cfg.RelayNode.HeartbeatInterval,
		DeliveryConcurrency: cfg.RelayNode.DeliveryConcurrency,
		SMTPPort:            cfg.RelayNode.SMTPPort,
		IPv6:                cfg.RelayNode.IPv6,
		MaxMessageSize:      cfg.Submission.MaxMessageSize,
		InboundEnabled:      cfg.RelayNode.Inbound.Enabled,
		Version:             pkg.Version,
		Mode:                cfg.RelayNode.Mode,
	}, log)
	if err != nil {
		return fmt.Errorf("relay node: %w", err)
	}
	defer func() { _ = agent.Close() }()

	// The environment checks run in the background: they make network
	// calls, and a node whose DNS is slow should still start.
	safego.Go(log, "relay node: environment checks", func() { agent.CheckEnvironment(ctx) })

	// The delivery listener, unless this node PULLS: then nothing is
	// dialled into it and there is no port to bind, which is the point
	// of the mode.
	var srv *smtp.Server
	var ln net.Listener
	if cfg.RelayNode.Mode == "pull" {
		log.Info("relay node in pull mode, claiming mail over the control channel", "node_id", agent.NodeID())
	} else {
		addr, tlsCfg, backend := agent.Listener()
		srv = relayagent.NewServer(backend, addr, cfg.RelayNode.Hostname, tlsCfg)
		// At debug, the whole SMTP conversation lands in the log. It is
		// the only view of a PROTOCOL refusal - the library answers a
		// malformed command itself and calls no hook - so without it a bad
		// envelope from the platform reads as an empty log here and a
		// mysterious 501 there.
		if strings.EqualFold(cfg.Logging.Level, "debug") {
			srv.Debug = relayagent.ProtocolTrace(log, "worker")
		}

		// Bind before serving, so a port we cannot take is a failure to
		// start rather than one log line under a healthy-looking process.
		// Implicit TLS, so the listener is wrapped rather than upgraded.
		raw, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("relay node listener on %s: %w", addr, err)
		}
		// Implicit TLS is the listener itself being a TLS listener, not a
		// STARTTLS upgrade inside the session. go-smtp has no ServeTLS, so
		// the wrapping happens here - and doing it here rather than
		// offering STARTTLS is the point: there is no cleartext phase to
		// strip on a port whose only client is a delivery worker.
		ln = tls.NewListener(raw, tlsCfg)
		log.Info("relay node listening", "addr", raw.Addr().String(), "node_id", agent.NodeID())
	}

	// The MX, for a node that also receives. Bound synchronously like
	// every other listener in this codebase: a port we cannot take is
	// a failure to start, not a line in a log under a process that
	// looks healthy while the MX record points at nothing.
	mxSrv, mxErr := startReceiveListener(cfg, agent, log)
	if mxErr != nil {
		return mxErr
	}

	agent.Start(ctx)
	if srv != nil {
		safego.Go(log, "relay node: accept loop", func() {
			if serr := srv.Serve(ln); serr != nil && !errors.Is(serr, smtp.ErrServerClosed) {
				log.Error("relay node listener stopped", "err", serr)
			}
		})
	}

	<-ctx.Done()
	log.Info("relay node shutting down")

	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// The MX first. It is the one taking mail from strangers, and
	// every session it accepts after this point is one more message
	// the forward loop will not get to drain.
	if mxSrv != nil {
		if err := mxSrv.Shutdown(shutdown); err != nil {
			log.Warn("relay node inbound listener shutdown", "err", err)
		}
	}

	if srv != nil {
		if err := srv.Shutdown(shutdown); err != nil {
			log.Warn("relay node listener shutdown", "err", err)
		}
	}

	return nil
}

// startReceiveListener binds the MX, when this node runs one.
//
// Returns (nil, nil) when it does not, which is the default: an open
// port 25 is a decision, and a node that only delivers has no reason
// to hold one.
func startReceiveListener(cfg *env.Config, agent *relayagent.Agent, log *slog.Logger) (*smtp.Server, error) {
	in := cfg.RelayNode.Inbound
	if !in.Enabled {
		return nil, nil
	}

	tlsCfg, err := agent.ReceiveTLS(in.TLS.Cert, in.TLS.Key)
	if err != nil {
		return nil, fmt.Errorf("relay node inbound tls: %w", err)
	}
	backend := agent.ReceiveListener(in.MaxMessageSize, in.RatePerMinute)
	srv := relayagent.NewReceiveServer(backend, in.Addr, cfg.RelayNode.Hostname, tlsCfg)
	if strings.EqualFold(cfg.Logging.Level, "debug") {
		srv.Debug = relayagent.ProtocolTrace(log, "inbound")
	}

	ln, err := net.Listen("tcp", in.Addr)
	if err != nil {
		return nil, fmt.Errorf("relay node inbound listener on %s: %w", in.Addr, err)
	}
	bound := ln.Addr().String()
	// The PROXY wrap goes OUTSIDE, because the header is the first
	// thing on the wire - before EHLO and before the STARTTLS upgrade.
	//
	// This node ASSERTS client_ip to the platform and SPF is computed
	// from it, so behind a balancer without this the node reports our
	// own hop as the sender of every message it receives.
	//
	// Only this listener. The other one in this process takes
	// connections from our own delivery workers over mutual TLS, where
	// there is no balancer and the client certificate is the identity.
	if in.ProxyProtocol.Enabled {
		if ln, err = proxylisten.Wrap(ln, in.ProxyProtocol.Trusted); err != nil {
			return nil, fmt.Errorf("relay node inbound proxy protocol: %w", err)
		}
	}
	// STARTTLS, not implicit TLS: this is an MX, and a sender on the
	// internet opens a cleartext session and upgrades. The other
	// listener in this process is the opposite, because its only
	// client is one of our own workers.
	log.Info("relay node inbound listening", "addr", bound,
		"hostname", cfg.RelayNode.Hostname, "starttls", tlsCfg != nil,
		"proxy_protocol", in.ProxyProtocol.Enabled)
	safego.Go(log, "relay node: inbound accept loop", func() {
		if serr := srv.Serve(ln); serr != nil && !errors.Is(serr, smtp.ErrServerClosed) {
			log.Error("relay node inbound listener stopped", "err", serr)
		}
	})

	return srv, nil
}
