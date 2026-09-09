// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package env

import (
	"fmt"
	"strings"
	"time"
)

// RelayNodeConfig is THIS node's own settings, read only by the relay
// role.
//
// Note the singular: relay_nodes is the control plane's view of the
// fleet, relay_node is what one node knows about itself. A node ships
// neither the DSN nor the encryption key, which is why Config.Validate
// has to know which role it checks.
type RelayNodeConfig struct {
	// ControlURL is where the platform lives. The node only ever
	// connects OUT to it - nothing reaches into a node except the
	// delivery workers, over mutual TLS.
	ControlURL string `mapstructure:"control_url"`
	// EnrollToken is the shared secret from relay_nodes.auto_register_token.
	// Needed for the first run only: after enrolment the node holds a
	// certificate and its own control token, and this can be removed.
	EnrollToken string `mapstructure:"enroll_token"`

	// Addr is where delivery workers connect. Implicit TLS, client
	// certificate required.
	Addr string `mapstructure:"addr"`

	// Mode is how mail reaches this node. "listen" (the default) binds
	// Addr and waits for delivery workers to dial it over mutual TLS.
	// "pull" binds nothing for delivery and claims assigned mail over
	// the control channel - for a node behind NAT or one that may only
	// egress through a proxy. The control channel is plain HTTPS and
	// honours HTTPS_PROXY, so pull mode needs no inbound port at all.
	Mode string `mapstructure:"mode"`
	// Hostname is what this node calls itself: the certificate name
	// workers dial AND the HELO it announces to the internet.
	//
	// One field for both: large receivers check HELO against the
	// connecting address's PTR, so letting the two disagree makes two
	// problems out of one.
	Hostname string `mapstructure:"hostname"`

	// Domains is the SCOPE of this node: the sender domains it may
	// carry outbound, and the recipient domains its MX accepts.
	// Empty is unrestricted, which is what a single-tenant node wants.
	//
	// Read at enrolment, where it becomes allowed_domains on the row
	// this node creates - so a fleet declares its split in the same
	// file the operator sets SPF from, rather than in the console per
	// node. It can only NARROW: enrolment is already authorized by the
	// platform token or a project key, and a node cannot widen what
	// that token already permits.
	//
	// THE TWO DIRECTIONS MATCH DIFFERENTLY and that is deliberate.
	// Outbound is exact, because SPF is written per name and a relay
	// authorized for example.com is not authorized for
	// mail.example.com. Inbound covers subdomains, because that is the
	// rule the platform's own accept list applies and a node must not
	// refuse what the platform would have taken.
	Domains []string `mapstructure:"domains"`

	// ServerGroup is the slug of the project server group this node
	// joins. Empty means the project's default group.
	//
	// Ignored by a PLATFORM node - the shared pool has no groups.
	// Enrolment says so rather than dropping the value quietly.
	ServerGroup string `mapstructure:"server_group"`

	// SpoolDir holds the queue. Metadata in bbolt, message bytes as
	// files beside it - see the spool for why they are not in bbolt.
	SpoolDir string `mapstructure:"spool_dir"`

	// MaxLifetime is how long a message may keep failing before it is
	// given up on. Three days is the ordinary MTA convention.
	MaxLifetime time.Duration `mapstructure:"max_lifetime"`
	// HeartbeatInterval is how often the node reports in. Well under
	// the platform's stale window, which the node is told at
	// enrolment.
	HeartbeatInterval time.Duration `mapstructure:"heartbeat_interval"`
	// DeliveryConcurrency caps simultaneous outbound sessions.
	DeliveryConcurrency int `mapstructure:"delivery_concurrency"`
	// SMTPPort is the destination port for delivery. 25 in production
	// and configurable only so a test can point it somewhere else.
	SMTPPort int `mapstructure:"smtp_port"`
	// IPv6 allows delivery over IPv6.
	//
	// Off by default: a box with a AAAA record will prefer v6, where
	// receivers want a PTR and SPF coverage a v4-only setup does not
	// have, and the refusals look nothing like the cause.
	IPv6 bool `mapstructure:"ipv6"`

	// Inbound turns this node into an MX as well as an egress.
	Inbound RelayNodeInboundConfig `mapstructure:"inbound"`
}

// RelayNodeInboundConfig is a node that also RECEIVES.
//
// For a network the platform cannot be reached from: mail sent into a
// region behind a firewall, whose bounces cannot cross back. The MX
// points at a node inside that region, which takes the mail on 25 and
// forwards it over the HTTP control channel it already has.
//
// Off by default - an open port 25 is a decision.
type RelayNodeInboundConfig struct {
	Enabled bool `mapstructure:"enabled"`
	// Addr is where the internet connects. An MX record carries no
	// port, so remote senders always try 25 - bind :2526 and map it
	// when the process cannot take a privileged port.
	Addr string `mapstructure:"addr"`
	// MaxMessageSize caps one received message. Above the platform's
	// inbound.max_message_size the node accepts mail the platform then
	// refuses, paying the bandwidth twice and bouncing nothing.
	MaxMessageSize int64 `mapstructure:"max_message_size"`
	// RatePerMinute is the per-IP session budget. This is the one
	// listener on a node whose rate is set by strangers.
	RatePerMinute int `mapstructure:"rate_per_minute"`
	// ProxyProtocol reads the real client address from a balancer in
	// front of this port. The node ASSERTS client_ip to the platform
	// and SPF is computed from it, so without this a node behind a
	// balancer reports our own hop as the sender for every message.
	ProxyProtocol ProxyProtocolConfig `mapstructure:"proxy_protocol"`
	// TLS offers STARTTLS. Empty means a self-signed pair generated
	// on this node and kept in the spool.
	//
	// Self-signed is not a compromise here: inbound mail is
	// opportunistic TLS (RFC 7435), so senders prefer it and almost
	// none verify. ACME would need port 80 open as well.
	TLS RelayNodeTLSConfig `mapstructure:"tls"`
}

// RelayNodeTLSConfig points at a certificate on the node's own disk.
type RelayNodeTLSConfig struct {
	Cert string `mapstructure:"cert"`
	Key  string `mapstructure:"key"`
}

// ValidateNode checks the config of a relay node.
//
// Separate from Validate, and not a subset of it, because a node is a
// genuinely different kind of process: it has no database, no
// encryption key, no session secret and no stores. Running the full
// check would demand three secrets a node must never hold, and
// skipping the check entirely would let a node boot with no idea
// where its control plane is and fail at the first message instead of
// at startup.
func (c *Config) ValidateNode() error {
	if c.RelayNode.ControlURL == "" {
		return fmt.Errorf("relay_node.control_url is required (the Mailyard address this node enrols with)")
	}

	if !strings.HasPrefix(c.RelayNode.ControlURL, "http://") &&
		!strings.HasPrefix(c.RelayNode.ControlURL, "https://") {
		return fmt.Errorf("relay_node.control_url must start with http:// or https://")
	}

	if c.RelayNode.Hostname == "" {
		return fmt.Errorf("relay_node.hostname is required (it is both the certificate name workers dial and the HELO this node announces)")
	}

	if c.RelayNode.SpoolDir == "" {
		return fmt.Errorf("relay_node.spool_dir is required (the queue has to survive a restart)")
	}

	switch c.RelayNode.Mode {
	case "", "listen":
		if c.RelayNode.Addr == "" {
			return fmt.Errorf("relay_node.addr is required (where delivery workers connect)")
		}
	case "pull":
	default:
		return fmt.Errorf("relay_node.mode %q invalid: want listen or pull", c.RelayNode.Mode)
	}

	if c.RelayNode.MaxLifetime <= 0 {
		return fmt.Errorf("relay_node.max_lifetime must be positive, otherwise nothing is ever given up on")
	}

	if c.RelayNode.HeartbeatInterval <= 0 {
		return fmt.Errorf("relay_node.heartbeat_interval must be positive, otherwise the pool will consider this node dead")
	}

	if c.RelayNode.DeliveryConcurrency < 1 {
		return fmt.Errorf("relay_node.delivery_concurrency must be at least 1")
	}

	if c.RelayNode.SMTPPort < 1 || c.RelayNode.SMTPPort > 65535 {
		return fmt.Errorf("relay_node.smtp_port must be a valid port")
	}

	if in := c.RelayNode.Inbound; in.Enabled {
		if in.Addr == "" {
			return fmt.Errorf("relay_node.inbound.addr is required when relay_node.inbound.enabled is set (where the internet delivers)")
		}

		if in.MaxMessageSize <= 0 {
			return fmt.Errorf("relay_node.inbound.max_message_size must be positive")
		}

		// One half of a pair is a pair nobody can build, and the
		// failure would be a listener that starts and then fails every
		// STARTTLS. Empty BOTH is the ordinary case - the node
		// generates its own.
		if (in.TLS.Cert == "") != (in.TLS.Key == "") {
			return fmt.Errorf("relay_node.inbound.tls needs both cert and key, or neither (neither generates a self-signed pair on the node)")
		}

		if err := in.ProxyProtocol.validate("relay_node.inbound"); err != nil {
			return err
		}
	}

	return nil
}
