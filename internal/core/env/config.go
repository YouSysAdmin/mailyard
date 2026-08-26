// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package env owns Config (Viper-backed YAML loader) and the
// per-process Runtime that handlers receive through the *Runtime
// field on each domain Handler.
package env

import (
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// ConsolePath is where the operator console is served, and the prefix
// every absolute link into it is built from.
//
// /app and not /admin: most of what is behind it is ordinary member
// work. The platform-admin pages sit under /app/admin.
//
// It lives here because three places build links into the console -
// the server mount, the docs gate and the invitation and reset mails -
// and internal/server cannot be imported by the domain packages that
// send those.
const ConsolePath = "/app"

// Config is the whole of what an operator can set, as resolved by
// Load: the YAML file merged with MAILYARD_* environment overrides.
// Validate refuses a boot that would otherwise fail later.
//
// Anything an operator changes while running is a platform setting
// instead - see internal/models/setting. A yaml key earns its place by
// being needed BEFORE there is a database to read settings from.
type Config struct {
	// Source describes where these values came from, for the boot log.
	// Not a setting - mapstructure must not try to fill it.
	Source string `mapstructure:"-"`

	// RemovedKeys are settings the operator wrote that no longer exist.
	// Filled by Load, reported at boot - see removedTLSKeys.
	RemovedKeys []string `mapstructure:"-"`

	Server     ServerConfig     `mapstructure:"server"`
	Database   DatabaseConfig   `mapstructure:"database"`
	Logging    LoggingConfig    `mapstructure:"logging"`
	Auth       AuthConfig       `mapstructure:"auth"`
	Worker     WorkerConfig     `mapstructure:"worker"`
	Sending    SendingConfig    `mapstructure:"sending"`
	Webhook    WebhookConfig    `mapstructure:"webhook"`
	Campaign   CampaignConfig   `mapstructure:"campaign"`
	Submission SubmissionConfig `mapstructure:"submission"`
	Inbound    InboundConfig    `mapstructure:"inbound"`
	Storage    StorageConfig    `mapstructure:"storage"`
	Metrics    MetricsConfig    `mapstructure:"metrics"`
	CORS       CORSConfig       `mapstructure:"cors"`
	ACME       ACMEConfig       `mapstructure:"acme"`

	RelayNodes  RelayNodesConfig  `mapstructure:"relay_nodes"`
	RelayNode   RelayNodeConfig   `mapstructure:"relay_node"`
	RateLimit   RateLimitConfig   `mapstructure:"ratelimit"`
	EmailVerify EmailVerifyConfig `mapstructure:"email_verify"`
}

// RelayNodesConfig governs relay nodes enrolling themselves into the
// shared pool.
//
// A relay node is a machine elsewhere that delivers straight to
// recipient MXs from its own address. Enrolment is how one joins the
// pool without an operator typing its details in.
type RelayNodesConfig struct {
	// Enabled registers the enrolment endpoints at all. Off means the
	// routes do not exist rather than merely refusing, the same
	// reasoning as ses.enabled: an unused public surface is one
	// nobody watches.
	Enabled bool `mapstructure:"enabled"`

	// AutoRegisterToken is the shared secret a platform node presents
	// to enrol. Required when enabled.
	//
	// One token for the whole fleet, because a per-node secret is the
	// thing that rots. A leaked one is survivable because enrolment
	// lands in pending - see relay_nodes_auto_approve, off by
	// default.
	AutoRegisterToken string `mapstructure:"auto_register_token"`

	// CACommonName names the authority in its own certificate. Cosmetic,
	// but it is what an operator sees when inspecting a node.
	CACommonName string `mapstructure:"ca_common_name"`
}

// EmailVerifyConfig tunes address verification. Off by default: the
// MX check makes outbound DNS queries, which an operator on a locked
// down network should opt into rather than discover.
type EmailVerifyConfig struct {
	Enabled bool `mapstructure:"enabled"`

	// CacheTTL is how long an intrinsic verdict is reused.
	CacheTTL time.Duration `mapstructure:"cache_ttl"`

	// MXCacheTTL is how long a per-domain MX answer is reused.
	MXCacheTTL time.Duration `mapstructure:"mx_cache_ttl"`

	// LookupTimeout bounds one DNS resolution.
	LookupTimeout time.Duration `mapstructure:"lookup_timeout"`
}

// ProxyProtocolConfig lets an SMTP listener learn the real client
// address from a load balancer.
//
// SMTP carries no X-Forwarded-For, so behind a TCP balancer every
// session appears to come from the balancer - which collapses the
// per-IP session limiter into one bucket for every sender, computes SPF
// for the wrong address, and stamps our own hop into client_ip. The
// HTTP side has server.trusted_proxies for the same job.
//
// Off by default, and Trusted may not be empty when it is on: a PROXY
// header is an unauthenticated claim about who is calling, so reading
// one from any peer would hand a stranger a forged source address. See
// internal/core/proxylisten for what each peer gets.
type ProxyProtocolConfig struct {
	Enabled bool `mapstructure:"enabled"`

	// Trusted are the balancer addresses or CIDRs whose PROXY header is
	// believed. A bare address is accepted as well as a CIDR.
	Trusted []string `mapstructure:"trusted"`
}

// validate refuses the one combination that cannot be made safe.
//
// Both possible defaults for an empty list are wrong: "trust
// everybody" is a forged-source-address hole, and "trust nobody" is a
// listener that answers 500 to its own balancer while the config
// claims the feature is on. So it is a boot failure and the operator
// picks.
//
// Called from both Validate and ValidateNode - a node never runs
// Validate, so a check that lives only there would silently not apply
// to the one listener on a node whose rate strangers set.
func (p ProxyProtocolConfig) validate(key string) error {
	if p.Enabled && len(p.Trusted) == 0 {
		return fmt.Errorf("%s.proxy_protocol.enabled needs %s.proxy_protocol.trusted: a PROXY header is an "+
			"unauthenticated claim about who is calling, so reading one from any peer would let a stranger "+
			"forge a source address and pass SPF as anybody", key, key)
	}

	return nil
}

// RateLimitConfig caps request rates on the HTTP edge. Fixed windows
// in process memory, so a multi-node deployment multiplies the ceiling
// by the node count - size accordingly or limit at a shared proxy.
// Zero disables one limit.
type RateLimitConfig struct {
	// Enabled is the master switch for every HTTP limiter here.
	Enabled bool `mapstructure:"enabled"`

	// LoginPerMinute caps POST /api/auth/login per client IP. bcrypt
	// cost-12 already makes brute force slow, this caps throughput.
	LoginPerMinute int `mapstructure:"login_per_minute"`

	// OIDCPerMinute caps the OIDC callback per client IP.
	OIDCPerMinute int `mapstructure:"oidc_per_minute"`

	// APIPerMinute caps /api/v1 per API key (per IP for callers
	// presenting no usable token).
	APIPerMinute int `mapstructure:"api_per_minute"`

	// The three below govern endpoints whose rate is set by somebody
	// else's software rather than by a person at a keyboard, which is
	// why they are an order of magnitude higher than the ones above and
	// why they were constants in routes.go until an operator needed to
	// change one. They are here and not in `inbound` or `relay_nodes`
	// because what they cap is the HTTP edge, and one place to look is
	// the point of this section.

	// SESWebhookPerMinute caps the public SES receiver
	// (POST /webhooks/ses).
	//
	// High, because SNS retries hard and for hours - throttling real
	// notifications would lose bounces, which is the one thing that
	// endpoint exists to avoid. A ceiling on abuse, not a quota.
	SESWebhookPerMinute int `mapstructure:"ses_webhook_per_minute"`

	// RelayNodeChatterPerMinute caps the authenticated relay node
	// endpoints - heartbeats, certificate renewal, status.
	//
	// A hundred nodes reporting every two minutes from behind one NAT
	// address is fifty entirely legitimate requests a minute, and the
	// failure mode of setting this too low is the whole fleet dropping
	// out of the pool at once.
	RelayNodeChatterPerMinute int `mapstructure:"relay_node_chatter_per_minute"`

	// RelayNodeInboundPerMinute caps mail a relay node forwards back
	// (POST /api/relay-nodes/inbound), separately from the timer-driven
	// endpoints above.
	//
	// A node's MX takes whatever the internet sends it, so this is the
	// one node endpoint whose rate strangers set. A runaway guard and
	// not a delivery policy: the node's own listener does the per-IP
	// filtering, and mail refused here was already accepted at the SMTP
	// layer - so the node has said 250 and a refusal loses it. Raise it
	// rather than lower it.
	RelayNodeInboundPerMinute int `mapstructure:"relay_node_inbound_per_minute"`
}

// MetricsConfig exposes the Prometheus scrape endpoint. Token, when
// set, must arrive as a bearer token - leave it empty only on
// trusted networks.
type MetricsConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Token   string `mapstructure:"token"`
}

// CORSConfig opens the API to browser clients on other origins.
//
// Off by default and worth leaving off: the console shares an origin
// with the API, so this is only for a separately hosted front end.
type CORSConfig struct {
	Enabled bool `mapstructure:"enabled"`

	// AllowedOrigins is an explicit list, e.g.
	// ["https://app.example.com"]. "*" is accepted but cannot be
	// combined with AllowCredentials - see Validate.
	AllowedOrigins []string `mapstructure:"allowed_origins"`

	// AllowedMethods and AllowedHeaders default to the set the API
	// actually uses, so a minimal config is just origins.
	AllowedMethods []string `mapstructure:"allowed_methods"`
	AllowedHeaders []string `mapstructure:"allowed_headers"`

	// ExposeHeaders lists response headers the browser may read.
	ExposeHeaders []string `mapstructure:"expose_headers"`

	// AllowCredentials lets the browser send the session cookie
	// cross-origin. This is the dangerous switch: it turns every
	// listed origin into somewhere an authenticated request can be
	// made from, so list only origins you control.
	AllowCredentials bool `mapstructure:"allow_credentials"`

	// MaxAge caps how long a browser may cache the preflight, in
	// seconds.
	MaxAge int `mapstructure:"max_age"`
}

// StorageConfig selects where attachment bytes live. The default
// (empty backend) keeps them inline as base64 in the database - fs
// and s3 move them out and store only metadata plus a key.
type StorageConfig struct {
	// Backend is "" (inline), "fs" or "s3".
	Backend string `mapstructure:"backend"`

	// FSPath is the base directory for the fs backend.
	FSPath string `mapstructure:"fs_path"`
	S3     struct {
		Endpoint     string `mapstructure:"endpoint"`
		Region       string `mapstructure:"region"`
		Bucket       string `mapstructure:"bucket"`
		AccessKey    string `mapstructure:"access_key"`
		SecretKey    string `mapstructure:"secret_key"`
		UsePathStyle bool   `mapstructure:"use_path_style"`
	} `mapstructure:"s3"`
}

// InboundConfig tunes the MX-facing SMTP listener. Disabled by
// default. No auth - recipients are gated on verified domains, so
// only mail for claimed domains is ever accepted.
type InboundConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	Addr     string `mapstructure:"addr"`
	Hostname string `mapstructure:"hostname"`

	// MaxMessageSize bounds one received message in bytes.
	MaxMessageSize int64 `mapstructure:"max_message_size"`

	// RatePerMinute caps new sessions per client IP per minute.
	// Zero disables the limiter.
	RatePerMinute int `mapstructure:"rate_per_minute"`

	// ProxyProtocol reads the real client address from a balancer in
	// front of this port. It matters more here than on submission: SPF
	// is computed from the CONNECTING IP, so without it every message
	// is checked against the balancer's address instead of the
	// sender's.
	ProxyProtocol ProxyProtocolConfig `mapstructure:"proxy_protocol"`

	// RejectOnDMARCFail refuses a message when the From domain
	// published p=reject and nothing it vouches for passed.
	//
	// Off by default. The verdict is stored either way - this only
	// decides whether a failure is also a refusal. Forwarded mail
	// breaks SPF routinely, so turn it on after looking at what real
	// traffic scores.
	RejectOnDMARCFail bool `mapstructure:"reject_on_dmarc_fail"`

	// TLS offers STARTTLS. On by default: a sending MX prefers
	// encryption and almost none verify the certificate, so a
	// self-signed pair is a real improvement over cleartext.
	TLS TLSConfig `mapstructure:"tls"`
}

// SubmissionConfig tunes the SMTP submission listener. Disabled by
// default - it opens a network port. Clients authenticate with AUTH
// PLAIN carrying either an SMTP submission credential (username plus
// password) or an API key with scope send as the password.
//
// Not relay_nodes / relay_node, which point the other way: this is
// mail arriving from an application we authenticate.
type SubmissionConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	Addr     string `mapstructure:"addr"`
	Hostname string `mapstructure:"hostname"`

	// MaxMessageSize bounds one submitted message in bytes.
	MaxMessageSize int64 `mapstructure:"max_message_size"`

	// RatePerMinute caps new sessions per client IP per minute.
	// Zero disables the limiter.
	RatePerMinute int `mapstructure:"rate_per_minute"`

	// ProxyProtocol reads the real client address from a balancer in
	// front of this port. Without it every session behind one shares a
	// single rate bucket.
	ProxyProtocol ProxyProtocolConfig `mapstructure:"proxy_protocol"`

	// TLS offers STARTTLS. On by default, because without it AUTH runs
	// over cleartext - acceptable only on a trusted network or behind a
	// proxy that terminated it already.
	TLS TLSConfig `mapstructure:"tls"`
}

// WebhookConfig tunes outgoing event webhook delivery.
type WebhookConfig struct {
	Timeout     time.Duration `mapstructure:"timeout"`
	MaxAttempts int           `mapstructure:"max_attempts"`
	RetryDelay  time.Duration `mapstructure:"retry_delay"`

	// AllowPrivateTargets lets webhooks reach loopback, RFC 1918 and
	// other reserved addresses. Off by default: URLs are chosen by
	// project members, and without the guard one can aim a delivery at
	// the cloud metadata service and use this process as a proxy into
	// the private network. Turn it on only for receivers that really
	// are on this network, and only knowing it reopens that path to
	// anyone who can create a webhook.
	AllowPrivateTargets bool `mapstructure:"allow_private_targets"`
}

// CampaignConfig tunes the campaign runner.
type CampaignConfig struct {
	// BatchSize caps how many recipients one batch renders and
	// queues. The campaign's send_rate throttles between batches.
	BatchSize    int           `mapstructure:"batch_size"`
	PollInterval time.Duration `mapstructure:"poll_interval"`
}

// WorkerConfig sizes the delivery worker pool and its retry policy.
// Durations accept Go syntax ("2s", "1h") in YAML and env alike.
type WorkerConfig struct {
	// Concurrency is the number of parallel delivery goroutines.
	Concurrency int `mapstructure:"concurrency"`

	// PollInterval is how often the queue is checked for due work
	// (sends also wake the worker immediately).
	PollInterval time.Duration `mapstructure:"poll_interval"`

	// MaxAttempts bounds delivery attempts per email before it is
	// marked failed.
	MaxAttempts int `mapstructure:"max_attempts"`

	// RetryBaseDelay seeds the exponential backoff:
	// base * 2^(attempt-1), capped at RetryMaxDelay.
	RetryBaseDelay time.Duration `mapstructure:"retry_base_delay"`
	RetryMaxDelay  time.Duration `mapstructure:"retry_max_delay"`

	// ClaimTimeout re-queues processing rows older than this (crash
	// recovery). Keep it comfortably above the slowest SMTP delivery.
	ClaimTimeout time.Duration `mapstructure:"claim_timeout"`
}

// SendingConfig bounds what a single send may carry.
type SendingConfig struct {
	MaxRecipients          int   `mapstructure:"max_recipients"`
	MaxAttachmentSize      int64 `mapstructure:"max_attachment_size"`
	MaxTotalAttachmentSize int64 `mapstructure:"max_total_attachment_size"`

	// AutoSuppressOnReject adds a suppression when an SMTP server
	// permanently rejects a recipient (5xx at RCPT TO). Default true.
	AutoSuppressOnReject bool `mapstructure:"auto_suppress_on_reject"`

	// AllowPrivateSMTPTargets lets a PROJECT's smtp server point at
	// loopback, RFC 1918 and other reserved addresses. Off by default,
	// for the reason webhook.allow_private_targets is: host and port
	// are a project member's choice, and the connection test returns
	// the peer's banner, which makes it a scanner of this network.
	// The shared pool and relay nodes are not subject to the guard at
	// all - an operator placed those, and they often are private.
	AllowPrivateSMTPTargets bool `mapstructure:"allow_private_smtp_targets"`

	// SPFInclude is the host tenants name in their SPF record, e.g.
	// "_spf.mail.example.com". Only this installation knows it, so the
	// default is empty and the console asks for it rather than
	// printing a placeholder somebody would publish verbatim.
	SPFInclude string `mapstructure:"spf_include"`

	// BounceAddress is the return path for mail leaving through the
	// shared platform pool, and only that: SPF is checked for the return
	// path against the connecting IP, so a platform domain is correct
	// exactly where the platform owns the IP. Applied to tenant relays it
	// would put an SPF failure on this domain.
	//
	// A project on its own server sets projects.bounce_address instead.
	// Empty leaves MAIL FROM as the From address.
	//
	// Which message a report concerns is answered separately, by
	// smtpclient.HeaderEmailID.
	BounceAddress string `mapstructure:"bounce_address"`
}

// CryptoConfig keys the at-rest encryption of secrets stored in the
// database: SMTP passwords, TOTP secrets, DKIM private keys, OAuth
// client secrets, certificate private halves.
//
// Required, with no fallback. A base64 fallback behind a startup warning
// means an install that missed the warning stores all of those
// reversibly in columns documented as encrypted, so refusing to boot is
// the honest version of the same warning.
//
// Changing it orphans encrypted rows - treat it like a database
// credential.
type CryptoConfig struct {
	// EncryptionKey is the secret the AES-256 key is derived from, via
	// HKDF-SHA256 with a purpose label (see crypto.KeyAtRest).
	//
	// At least 32 characters, and random:
	//
	//	openssl rand -hex 32
	//
	// The floor matters: HKDF stretches anything to 32 bytes, so a
	// short passphrase yields a key that looks like every other one
	// and carries only the entropy typed. Same floor as
	// auth.jwt_secret. Set it through the env var to keep it out of
	// YAML.
	EncryptionKey string `mapstructure:"encryption_key"`
}

// AuthConfig gates the operator-console auth surface. When enabled,
// every protected /api/* route requires a session cookie.
type AuthConfig struct {
	Disabled bool `mapstructure:"disabled"`

	// JWTSecret signs HS256 session JWTs. Required when !Disabled.
	// Generate with `openssl rand -hex 32`.
	JWTSecret string `mapstructure:"jwt_secret"`

	// SessionTTL is the cookie lifetime. Empty -> 12h.
	SessionTTL string `mapstructure:"session_ttl"`

	// RegistrationEnabled opens public self-signup. Off by default -
	// this is an operator console, and open signup on an
	// internet-facing install means strangers with accounts. New
	// accounts are always plain users.
	RegistrationEnabled bool `mapstructure:"registration_enabled"`

	// PasskeysEnabled offers WebAuthn passkeys on LOCAL accounts. On
	// by default, since nothing happens until somebody enrols one.
	//
	// Local-only on purpose: a passkey on an SSO account is a way in
	// the IdP knows nothing about, so disabling the person there would
	// no longer lock them out.
	PasskeysEnabled bool `mapstructure:"passkeys_enabled"`

	Local AuthLocalConfig `mapstructure:"local"`
}

// AuthLocalConfig governs password sign-in. Disabling it is how an
// installation becomes SSO-only, installation wide - there is no
// per-project equivalent, because one session records one provider.
type AuthLocalConfig struct {
	Enabled bool `mapstructure:"enabled"`

	// Email is the bootstrap user's address. On first run with an
	// empty users table, the tool inserts a user with this email and
	// a generated password (logged to stderr once).
	Email string `mapstructure:"email"`
}

// LoggingConfig builds the process logger, in buildLogger. Level and
// Format are checked by Validate, since the alternative is discovering
// a typo in the one facility that would have reported it.
//
// Color applies to text output on a terminal only - see buildLogger.
type LoggingConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
	Output string `mapstructure:"output"`
	Color  bool   `mapstructure:"color"`
}

// TLSConfig says whether a listener terminates TLS in this process. It
// says nothing about which certificate - that is chosen in the console
// and stored in the database.
//
// It was a mode per listener (none / manual / self / acme) plus a
// file pair and an fqdn. Uploading in the console already did manual,
// self is the fallback when nothing is assigned, acme is one block
// below, and fqdn came from server.public_url. What was left
// duplicated the assignment and disagreed with it in both directions -
// mode none made an assignment inert, an assignment overrode the file.
// A boolean cannot disagree with anything.
type TLSConfig struct {
	// Enabled terminates TLS here.
	//
	// False for HTTP and true for the two SMTP listeners, which are
	// the two honest answers. On 25 and 587 a sender prefers
	// encryption and almost none verify, so a self-signed pair beats
	// cleartext. In front of the console usually sits a proxy that
	// terminates TLS - and relay nodes verify our certificate with
	// their system trust store, so a self-signed one on the API stops
	// every node enrolling.
	Enabled bool `mapstructure:"enabled"`
}

// ACMEConfig is what is left of Let's Encrypt in the config file: one
// port, and only for the deployment that needs it.
//
// Everything else - whether ACME is on, which hosts, the contact, the
// directory - is a platform setting now. A yaml key earns its place by
// binding a port, and tls-alpn-01 needs none: the CA validates against
// the TLS listener that is already up.
type ACMEConfig struct {
	// ChallengeAddr binds an HTTP-01 challenge listener.
	//
	// Empty by default, meaning tls-alpn-01 only, which needs nothing.
	// Set it when a proxy TERMINATES TLS and answers the handshake
	// itself, so ALPN validation never reaches us. A TCP-passthrough
	// proxy preserves ALPN and needs nothing.
	ChallengeAddr string `mapstructure:"challenge_addr"`
}

// ServerConfig is the HTTP listener: where it binds, and the URL it is
// reached at from outside.
//
// PublicURL is not cosmetic. It is what mails and OIDC redirects are
// built from, and the self-signed certificate hostname is derived from
// it - so getting it wrong is a link nobody can follow, not a wrong
// label.
type ServerConfig struct {
	Addr      string    `mapstructure:"addr"`
	PublicURL string    `mapstructure:"public_url"`
	TLS       TLSConfig `mapstructure:"tls"`

	// TrustedProxies are the proxy IPs or CIDRs in front of this node.
	// Empty means the TCP peer is the caller, which is right for direct
	// exposure. Behind a proxy, set it or the rate limiter, the audit
	// log and the access log all record the proxy.
	//
	// LIST EVERY HOP, not just the nearest one. The caller's address is
	// resolved by walking X-Forwarded-For from the right and stopping at
	// the first entry that is not in this list (internal/core/clientip),
	// so a hop left out of it becomes the answer.
	TrustedProxies []string `mapstructure:"trusted_proxies"`

	// MaxConcurrentRequests caps connections being served at once. 0
	// leaves fasthttp's own default, which is 262144.
	//
	// It is a memory bound and not a performance setting. fasthttp reads
	// a whole request body into memory BEFORE the handler chain runs, so
	// before any auth or rate limit can refuse it, and the cap on one
	// body is sending.max_total_attachment_size inflated for base64 -
	// around 34 MB at the default. The default connection ceiling makes
	// the product of those two numbers unbounded in practice.
	MaxConcurrentRequests int `mapstructure:"max_concurrent_requests"`
}

// DatabaseConfig is the PostgreSQL connection and the key that seals
// secrets inside it. DSN is the only required setting in the whole
// file.
type DatabaseConfig struct {
	// Crypto keys the at-rest encryption of secrets in this database,
	// which is all it does - every consumer is a store sealing a
	// column. It sits here and not at the top level because
	// auth.jwt_secret keys something else entirely: sessions, OIDC
	// state and tracking HMACs all derive from that one.
	Crypto CryptoConfig `mapstructure:"crypto"`

	// DSN is the PostgreSQL connection string for the PRIMARY, e.g.
	// postgres://user:pass@host:5432/mailyard?sslmode=require.
	// Set it via MAILYARD_DATABASE_DSN to keep the password out of YAML.
	DSN string `mapstructure:"dsn"`

	// ReplicaDSNs are read-only followers. Empty means every query
	// goes to the primary, which is the default and is always correct.
	//
	// Adding one does not redistribute load on its own: reads reach a
	// follower only where a store asked through ReadQuery.
	//
	// Several are round-robined. No primary failover: promoting a
	// follower unattended is how two nodes both come to believe they are
	// the primary.
	//
	// A follower cannot serve LISTEN/NOTIFY - notifications do not travel
	// in the WAL, so a LISTEN there is accepted and never fires. The
	// listener takes the primary DSN.
	ReplicaDSNs []string `mapstructure:"replica_dsns"`

	// ReplicaReads picks which groups of reads actually use the
	// followers. Ignored entirely when ReplicaDSNs is empty.
	ReplicaReads ReplicaReadsConfig `mapstructure:"replica_reads"`

	// MaxOpenConns caps this node's connections to the primary, and
	// separately to each replica. 0 removes the cap, which is the
	// driver's own default - and is how one busy node exhausts the
	// server's max_connections for every node sharing the database,
	// including the delivery workers.
	MaxOpenConns int `mapstructure:"max_open_conns"`

	// MaxIdleConns is how many of those stay warm between requests.
	// The driver default of 2 makes a steady load reconnect
	// constantly. 0 here means keep none, so the default is set to
	// match MaxOpenConns rather than to 0.
	MaxIdleConns int `mapstructure:"max_idle_conns"`

	// ConnMaxLifetime recycles a connection after this long regardless
	// of use, so a fleet drifts back onto a rebalanced or failed-over
	// database. 0 keeps connections forever.
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`

	// ConnMaxIdleTime closes a warm connection that has sat unused
	// this long. 0 keeps it until ConnMaxLifetime.
	ConnMaxIdleTime time.Duration `mapstructure:"conn_max_idle_time"`
}

// ReplicaReadsConfig turns follower reads on or off per group.
//
// The second of two gates. Which queries MAY use a follower is decided
// in code, per query, by calling ReadQuery; this decides which of those
// groups an installation wants routed away, which depends on its
// replication lag and how its console is used.
//
// A group defaults off where a page that reads it also writes it and
// immediately re-reads. Only suppressions does.
//
// Never eligible, whatever this says: the queue claim, quota counts
// before a send, session and TOTP resolution, suppression filtering at
// send time, inbound dedup, uniqueness probes, and anything read back
// that the same request wrote. Those are wrong on a follower, not slow.
type ReplicaReadsConfig struct {
	// Analytics moves the dashboard aggregation - the summary, the
	// daily trend and the status breakdown. Default ON.
	//
	// The safest and the biggest win: these count across the whole
	// emails table, and no page writes a row then asks for an
	// aggregate over days that includes it.
	Analytics bool `mapstructure:"analytics"`

	// EmailLog moves browsing the delivery log and its status counts,
	// including the Prometheus gauge. Default ON.
	//
	// The console never writes and re-reads this list: sending
	// navigates to the detail page, which reads by id on the primary,
	// and Retry patches the row in place. Not the quota count before a
	// send, which is a decision rather than a display.
	EmailLog bool `mapstructure:"email_log"`

	// InboundLog moves browsing received mail and its status counts.
	// Default ON.
	//
	// Rows arrive from the MX listener, never from the console, and a
	// delete drops the row locally. Not the Message-ID and
	// content-hash lookups at ingest: those decide whether to insert,
	// and a stale answer stores the message twice.
	InboundLog bool `mapstructure:"inbound_log"`

	// Sandbox moves the captured-message list and its count.
	// Default ON.
	//
	// Looks risky and is not: the console does not write these rows,
	// an application under test does, and nobody watches the page
	// while a suite runs.
	Sandbox bool `mapstructure:"sandbox"`

	// Suppressions moves the console list and search.
	//
	// Default off, alone among these: adding and removing a block both
	// reload the list at once, so on a lagging follower the address
	// you just blocked is missing - and "is this address blocked" is
	// the question the page exists to answer.
	//
	// Worth turning on where the list is large and the lag is known to
	// be small. It governs the LIST only: the send-time filter is
	// never moved, because a stale answer there delivers to an address
	// that was just blocked.
	Suppressions bool `mapstructure:"suppressions"`

	// Bounces moves the bounce list. Default ON: the page is read-only
	// and the rows are written by bounce intake, never by a person.
	// Not the has-this-address-bounced lookup, which is a single index
	// seek and gains nothing.
	Bounces bool `mapstructure:"bounces"`

	// Contacts moves the contact list, its search and its count.
	// Default ON. Written only by the delivery worker as messages
	// finish, and the API is read-only by design - the store has no
	// Put - so nobody here reads back what they just wrote.
	Contacts bool `mapstructure:"contacts"`

	// WebhookDeliveries moves the delivery history. Default ON: the
	// rows come from the dispatcher, and editing a webhook reloads the
	// definitions instead, which are not moved.
	WebhookDeliveries bool `mapstructure:"webhook_deliveries"`

	// AuditLog moves both trails. Default ON: the writes already go
	// through an async queue and may be dropped under load, so a
	// follower adds nothing new to reason about.
	AuditLog bool `mapstructure:"audit_log"`
}

// Any reports whether any group is enabled. Used to tell an operator
// who configured followers and then switched everything off - which
// leaves connections open that serve nothing.
func (r ReplicaReadsConfig) Any() bool {
	return r.Analytics || r.EmailLog || r.InboundLog || r.Sandbox ||
		r.Suppressions || r.Bounces || r.Contacts || r.WebhookDeliveries ||
		r.AuditLog
}

// Load reads the YAML file at path (or ./mailyard.yaml when path is
// empty), merges in MAILYARD_*-prefixed environment overrides, and
// returns the resolved Config. A missing file is not an error when
// env vars supply the required values.
func Load(path string) (*Config, error) {
	v := viper.New()
	if path != "" {
		v.SetConfigFile(path)
	} else {
		// SetConfigFile and not SetConfigName: the name form also tries
		// the bare name, so a binary called `mailyard` in the working
		// directory was read as YAML and the operator got "invalid
		// trailing UTF-8 octet" from their own executable.
		v.SetConfigFile("mailyard.yaml")
	}

	v.SetEnvPrefix("MAILYARD")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// AutomaticEnv only resolves keys viper already knows about (from
	// defaults or the config file), and Unmarshal walks that known-key
	// list - so a value supplied only via env (e.g.
	// MAILYARD_AUTH_JWT_SECRET with no auth: block in YAML) would be
	// silently dropped without an explicit BindEnv per key.
	bindEnvKeys(v, reflect.TypeFor[Config](), "")

	v.SetDefault("server.addr", ":3000")
	// Far above what any single node of this shape serves, far below
	// fasthttp's 262144 - the point is that the worst case is a number
	// rather than whatever the kernel allows.
	v.SetDefault("server.max_concurrent_requests", 4096)
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "json")
	v.SetDefault("logging.output", "stdout")
	v.SetDefault("logging.color", true)
	v.SetDefault("auth.passkeys_enabled", true)

	// Replica read groups. They do nothing at all until
	// database.replica_dsns is non-empty, so these cost an
	// installation with no follower exactly nothing.
	//
	// On where the console never writes and re-reads the same list,
	// off where it does. See ReplicaReadsConfig for how each was
	// decided.
	v.SetDefault("database.replica_reads.analytics", true)
	v.SetDefault("database.replica_reads.email_log", true)
	v.SetDefault("database.replica_reads.inbound_log", true)
	v.SetDefault("database.replica_reads.sandbox", true)
	// The one default that is off. The suppressions page adds and
	// removes rows and reloads the list on the spot, so lag shows up
	// as the block you just made being absent from the answer.
	v.SetDefault("database.replica_reads.suppressions", false)
	v.SetDefault("database.replica_reads.bounces", true)
	v.SetDefault("database.replica_reads.contacts", true)
	v.SetDefault("database.replica_reads.webhook_deliveries", true)
	v.SetDefault("database.replica_reads.audit_log", true)

	// The pool. Unbounded (the driver default) let one node's burst
	// take every slot of the server's max_connections, and 2 idle made
	// steady load a reconnect churn. 25 fits several nodes inside the
	// stock 100 with room for one-shot commands and an operator's psql.
	v.SetDefault("database.max_open_conns", 25)
	v.SetDefault("database.max_idle_conns", 25)
	v.SetDefault("database.conn_max_lifetime", "30m")
	v.SetDefault("database.conn_max_idle_time", "5m")

	v.SetDefault("relay_nodes.enabled", false)
	v.SetDefault("relay_nodes.ca_common_name", "Mailyard Relay CA")
	relayNodeDefaults(v)
	v.SetDefault("worker.concurrency", 4)
	v.SetDefault("worker.poll_interval", "2s")
	v.SetDefault("worker.max_attempts", 5)
	v.SetDefault("worker.retry_base_delay", "30s")
	v.SetDefault("worker.retry_max_delay", "1h")
	v.SetDefault("worker.claim_timeout", "5m")
	v.SetDefault("sending.max_recipients", 50)
	v.SetDefault("sending.max_attachment_size", 10*1024*1024)
	v.SetDefault("sending.max_total_attachment_size", 25*1024*1024)
	v.SetDefault("sending.auto_suppress_on_reject", true)
	v.SetDefault("sending.allow_private_smtp_targets", false)
	v.SetDefault("sending.spf_include", "")
	v.SetDefault("sending.bounce_address", "")
	v.SetDefault("webhook.timeout", "10s")
	v.SetDefault("webhook.max_attempts", 3)
	v.SetDefault("webhook.retry_delay", "10s")
	v.SetDefault("webhook.allow_private_targets", false)
	v.SetDefault("campaign.batch_size", 100)
	v.SetDefault("campaign.poll_interval", "5s")
	v.SetDefault("submission.enabled", false)
	// 587 is the submission port (RFC 6409) and 25 is where an MX is
	// expected. Both are privileged, so an unprivileged process needs
	// CAP_NET_BIND_SERVICE or a port mapping - but both listeners are
	// off by default, so this only affects a deployment that turned
	// one on, and a deployment that wants a real MX has to be on 25
	// anyway. Override with submission.addr / inbound.addr to go back to
	// the unprivileged 2525 / 2526 behind a proxy.
	v.SetDefault("submission.addr", ":587")
	v.SetDefault("submission.hostname", "mailyard")
	v.SetDefault("submission.max_message_size", 25*1024*1024)
	v.SetDefault("submission.rate_per_minute", 60)
	// STARTTLS on for the two mail listeners and off for HTTP. See
	// TLSConfig.Enabled for why those are different answers rather than
	// an oversight.
	v.SetDefault("server.tls.enabled", false)
	v.SetDefault("submission.tls.enabled", true)
	v.SetDefault("inbound.tls.enabled", true)
	// Empty, not ":80". tls-alpn-01 needs no port at all, so binding one
	// by default would take a privileged port from every installation
	// for a challenge type most of them never use.
	v.SetDefault("acme.challenge_addr", "")
	v.SetDefault("inbound.enabled", false)
	v.SetDefault("inbound.addr", ":25")
	v.SetDefault("inbound.hostname", "mailyard")
	v.SetDefault("inbound.max_message_size", 25*1024*1024)
	v.SetDefault("inbound.rate_per_minute", 120)
	// Off, and off is the only safe default: a PROXY header is an
	// unauthenticated claim about who is calling, so a listener that
	// reads one without a trusted list hands a stranger a forged source
	// address. Turning it on REQUIRES naming the balancer.
	v.SetDefault("submission.proxy_protocol.enabled", false)
	v.SetDefault("inbound.proxy_protocol.enabled", false)
	v.SetDefault("inbound.reject_on_dmarc_fail", false)
	v.SetDefault("email_verify.enabled", false)
	v.SetDefault("email_verify.cache_ttl", "24h")
	v.SetDefault("email_verify.mx_cache_ttl", "1h")
	v.SetDefault("email_verify.lookup_timeout", "5s")
	v.SetDefault("ratelimit.enabled", true)
	v.SetDefault("ratelimit.login_per_minute", 10)
	v.SetDefault("ratelimit.oidc_per_minute", 30)
	v.SetDefault("ratelimit.api_per_minute", 120)
	v.SetDefault("ratelimit.ses_webhook_per_minute", 600)
	v.SetDefault("ratelimit.relay_node_chatter_per_minute", 600)
	v.SetDefault("ratelimit.relay_node_inbound_per_minute", 1200)
	v.SetDefault("metrics.enabled", false)
	v.SetDefault("cors.enabled", false)
	// The methods and headers the API actually uses, so a working
	// config is just cors.enabled plus cors.allowed_origins.
	v.SetDefault("cors.allowed_methods", []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"})
	v.SetDefault("cors.allowed_headers", []string{"Content-Type", "Authorization", "X-Mailyard-Project-Id"})
	v.SetDefault("cors.max_age", 300)
	v.SetDefault("storage.backend", "")
	v.SetDefault("storage.fs_path", "data/attachments")

	source := "environment only"
	if err := v.ReadInConfig(); err == nil {
		source = v.ConfigFileUsed()
	} else {
		// A missing config is only tolerated when we chose the path
		// ourselves - an operator who passed --config expects to be
		// told the file is not there.
		//
		// Two error shapes mean the same thing here: viper reports
		// ConfigFileNotFoundError when it searched for a name, and a
		// bare os.ErrNotExist when it was handed an explicit path,
		// which is what the default arm now uses.
		missing := os.IsNotExist(err)
		if _, ok := errors.AsType[viper.ConfigFileNotFoundError](err); ok {
			missing = true
		}

		if !missing || path != "" {
			return nil, fmt.Errorf("read config: %w", err)
		}

		// No file, and that is allowed. Say so, and name any YAML
		// sitting in the working directory: a config under a name
		// nothing reads looks exactly like a config that is being
		// ignored, and the symptom shows up far away - a renamed file
		// once cost an afternoon of chasing missing tracking pixels.
		if near := nearbyYAML(); len(near) > 0 {
			source = "environment only (ignoring " + strings.Join(near, ", ") + ", expected mailyard.yaml)"
		}
	}

	var c Config
	if err := v.Unmarshal(&c); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	c.Source = source
	// Captured here and nowhere else, because this is the only place the
	// raw values are still visible: these keys no longer have a field to
	// land in, so nothing about the parsed Config can show they were set.
	c.RemovedKeys = removedKeysIn(v)
	trimStringSlices(reflect.ValueOf(&c))

	return &c, nil
}

// trimStringSlices strips surrounding whitespace from every entry of
// every []string in the config.
//
// EVERY list here can arrive from an environment variable, and viper
// splits one on commas WITHOUT trimming - so the natural
// "a.example.com, b.example.com" yields a second entry that begins with
// a space. What that costs depends on who reads it and none of the
// answers are good: server.trusted_proxies fails the BOOT, because
// netip.ParsePrefix refuses " 10.0.0.0/8" and proxylisten.ParseTrusted
// reports a value that looks correct in the file. A relay node's
// allowed domain simply never matches, and the symptom is a send that
// resolves no candidates. cors.allowed_origins silently stops matching
// the origin it names.
//
// Done ONCE, here, rather than in each consumer: there are seven such
// fields today and the next one would arrive without the trim, since
// nothing about writing a config field tells you this is a problem.
// Trimming is safe for all of them - not one takes a value where
// leading or trailing space is meaningful.
func trimStringSlices(v reflect.Value) {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return
		}

		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return
	}

	for _, f := range v.Fields() {
		if f.Kind() == reflect.Struct || f.Kind() == reflect.Pointer {
			trimStringSlices(f)

			continue
		}

		if f.Kind() != reflect.Slice || f.Type().Elem().Kind() != reflect.String || !f.CanSet() {
			continue
		}

		for j := range f.Len() {
			e := f.Index(j)
			e.SetString(strings.TrimSpace(e.String()))
		}
	}
}

// removedKeysIn names every removed TLS key the operator actually set.
func removedKeysIn(v *viper.Viper) []string {
	var out []string
	for _, key := range removedTLSKeys {
		if v.IsSet(key) {
			out = append(out, key)
		}
	}

	return out
}

// nearbyYAML lists YAML files in the working directory, so a config
// that was never read can be named rather than merely absent.
func nearbyYAML() []string {
	entries, err := os.ReadDir(".")
	if err != nil {
		return nil
	}

	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || name == "mailyard.yaml" {
			continue
		}

		if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
			out = append(out, name)
		}

		if len(out) == 4 {
			break
		}
	}

	return out
}

// bindEnvKeys walks the config struct and calls v.BindEnv for every
// leaf key so env-only values survive Unmarshal (see the comment at
// the call site in Load). Key names come from the mapstructure tag,
// falling back to the lowercased field name - the same resolution
// mapstructure itself uses for untagged fields (e.g. tlsutils.Config).
func bindEnvKeys(v *viper.Viper, t reflect.Type, prefix string) {
	for f := range t.Fields() {
		if !f.IsExported() {
			continue
		}

		name := f.Tag.Get("mapstructure")
		if name == "-" {
			continue
		}

		if name == "" {
			name = strings.ToLower(f.Name)
		}

		key := name
		if prefix != "" {
			key = prefix + "." + name
		}

		ft := f.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}

		if ft.Kind() == reflect.Struct {
			bindEnvKeys(v, ft, key)
			continue
		}

		_ = v.BindEnv(key)
	}
}

// Validate checks required fields. Defaults handle the rest.
func (c *Config) Validate() error {
	if c.Database.DSN == "" {
		return fmt.Errorf("database.dsn is required (e.g. postgres://user:pass@host:5432/mailyard)")
	}

	for i, r := range c.Database.ReplicaDSNs {
		if strings.TrimSpace(r) == "" {
			return fmt.Errorf("database.replica_dsns[%d] is empty", i)
		}

		// Pasting the primary in here is the mistake worth catching:
		// it looks like it works, spreads nothing, and hides that the
		// replica an operator believes they configured is not there.
		if r == c.Database.DSN {
			return fmt.Errorf("database.replica_dsns[%d] is the primary dsn - a replica has to be a different server", i)
		}
	}

	if c.Database.MaxOpenConns < 0 || c.Database.MaxIdleConns < 0 ||
		c.Database.ConnMaxLifetime < 0 || c.Database.ConnMaxIdleTime < 0 {
		return fmt.Errorf("database pool settings (max_open_conns, max_idle_conns, conn_max_lifetime, conn_max_idle_time) cannot be negative - 0 means the driver default")
	}

	if c.Server.MaxConcurrentRequests < 0 {
		return fmt.Errorf("server.max_concurrent_requests cannot be negative - 0 means the fasthttp default")
	}

	if c.Worker.Concurrency < 1 {
		return fmt.Errorf("worker.concurrency must be at least 1")
	}

	if c.Worker.MaxAttempts < 1 {
		return fmt.Errorf("worker.max_attempts must be at least 1")
	}

	if c.Worker.PollInterval <= 0 || c.Worker.RetryBaseDelay <= 0 ||
		c.Worker.RetryMaxDelay <= 0 || c.Worker.ClaimTimeout <= 0 {
		return fmt.Errorf("worker durations (poll_interval, retry_base_delay, retry_max_delay, claim_timeout) must be positive")
	}

	if c.Sending.MaxRecipients < 1 {
		return fmt.Errorf("sending.max_recipients must be at least 1")
	}

	if c.Webhook.Timeout <= 0 || c.Webhook.RetryDelay <= 0 || c.Webhook.MaxAttempts < 1 {
		return fmt.Errorf("webhook settings (timeout, retry_delay, max_attempts) must be positive")
	}

	if c.Campaign.BatchSize < 1 || c.Campaign.PollInterval <= 0 {
		return fmt.Errorf("campaign settings (batch_size, poll_interval) must be positive")
	}

	if c.Submission.Enabled {
		if c.Submission.Addr == "" {
			return fmt.Errorf("submission.addr required when the submission listener is enabled")
		}

		if c.Submission.MaxMessageSize < 1 {
			return fmt.Errorf("submission.max_message_size must be positive")
		}
	}

	switch c.Storage.Backend {
	case "", "fs", "s3":
	default:
		return fmt.Errorf("storage.backend %q invalid: want %q, %q or empty for inline", c.Storage.Backend, "fs", "s3")
	}

	if c.Storage.Backend == "s3" && c.Storage.S3.Bucket == "" {
		return fmt.Errorf("storage.s3.bucket required for the s3 backend")
	}

	if c.Inbound.Enabled {
		if c.Inbound.Addr == "" {
			return fmt.Errorf("inbound.addr required when the inbound listener is enabled")
		}

		if c.Inbound.MaxMessageSize < 1 {
			return fmt.Errorf("inbound.max_message_size must be positive")
		}
	}

	if c.Sending.MaxAttachmentSize < 0 || c.Sending.MaxTotalAttachmentSize < 0 {
		return fmt.Errorf("sending attachment size limits must not be negative")
	}

	// The HTTP body limit is derived from the total (base64 inflates
	// attachments by 4/3), so an oversized value here would ask
	// fasthttp to buffer that much per request.
	const maxTotalAttachmentSize = 256 * 1024 * 1024
	if c.Sending.MaxTotalAttachmentSize > maxTotalAttachmentSize {
		return fmt.Errorf("sending.max_total_attachment_size must not exceed %d bytes", maxTotalAttachmentSize)
	}

	// A per-attachment cap above the total is not reachable, and
	// reads as a promise the total silently overrides.
	if c.Sending.MaxTotalAttachmentSize > 0 && c.Sending.MaxAttachmentSize > c.Sending.MaxTotalAttachmentSize {
		return fmt.Errorf("sending.max_attachment_size must not exceed sending.max_total_attachment_size")
	}

	if c.EmailVerify.Enabled {
		if c.EmailVerify.CacheTTL < 0 || c.EmailVerify.MXCacheTTL < 0 || c.EmailVerify.LookupTimeout <= 0 {
			return fmt.Errorf("email_verify durations must be positive (lookup_timeout) or non-negative (cache ttls)")
		}
	}

	// An unrecognized format is not a harmless typo: the logger falls
	// back to human-readable text, so "jsn" quietly produces output no
	// shipper can parse and nothing says why.
	switch c.Logging.Format {
	case "", "text", "json":
	default:
		return fmt.Errorf("logging.format %q invalid: want %q or %q", c.Logging.Format, "text", "json")
	}

	switch c.Logging.Level {
	case "", "trace", "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("logging.level %q invalid: want trace, debug, info, warn or error", c.Logging.Level)
	}

	if err := c.Submission.ProxyProtocol.validate("submission"); err != nil {
		return err
	}

	if err := c.Inbound.ProxyProtocol.validate("inbound"); err != nil {
		return err
	}

	if c.RateLimit.LoginPerMinute < 0 || c.RateLimit.OIDCPerMinute < 0 || c.RateLimit.APIPerMinute < 0 ||
		c.RateLimit.SESWebhookPerMinute < 0 || c.RateLimit.RelayNodeChatterPerMinute < 0 ||
		c.RateLimit.RelayNodeInboundPerMinute < 0 {
		return fmt.Errorf("ratelimit values must not be negative (use 0 to disable an individual limit)")
	}

	// Not gated on auth: the secrets this key protects (SMTP
	// passwords, DKIM private keys) exist whether or not the console
	// has accounts.
	if addr := strings.TrimSpace(c.Sending.BounceAddress); addr != "" {
		if _, err := mail.ParseAddress(addr); err != nil {
			return fmt.Errorf("sending.bounce_address %q is not a valid email address", addr)
		}
	}

	if err := c.validateRelayEdition(); err != nil {
		return err
	}

	if c.RelayNodes.Enabled && c.RelayNodes.AutoRegisterToken == "" {
		return fmt.Errorf("relay_nodes.auto_register_token required when relay_nodes.enabled is set, otherwise any caller can enrol a node (generate with `openssl rand -hex 32`)")
	}

	if c.RelayNodes.Enabled && len(c.RelayNodes.AutoRegisterToken) < 32 {
		return fmt.Errorf("relay_nodes.auto_register_token must be at least 32 characters, it is the only thing standing between a stranger and a machine in your sending pool")
	}

	if c.Database.Crypto.EncryptionKey == "" {
		// Named in full, because this MOVED: it was crypto.encryption_key
		// at the top level, and an operator upgrading meets this message
		// rather than a key that is quietly ignored.
		return fmt.Errorf("database.crypto.encryption_key required, secrets at rest are not stored without it (generate with `openssl rand -hex 32`) - this was crypto.encryption_key, and MAILYARD_CRYPTO_ENCRYPTION_KEY is now MAILYARD_DATABASE_CRYPTO_ENCRYPTION_KEY")
	}

	// The same floor auth.jwt_secret has, for the same reason: HKDF
	// stretches a short secret to 32 bytes but cannot invent the
	// entropy it did not have, so a six-character key is a
	// six-character key however long the derived one looks.
	if len(c.Database.Crypto.EncryptionKey) < 32 {
		return fmt.Errorf("database.crypto.encryption_key must be at least 32 characters (generate with `openssl rand -hex 32`)")
	}

	// The tracking signer keys on jwt_secret too, and its URLs are the
	// one surface that is public by design - so the secret is required
	// by whichever of the two features wants it, auth or a public_url,
	// and the floor is the same either way: HKDF stretches a short
	// secret but cannot invent entropy.
	if c.Auth.Disabled && c.Auth.JWTSecret == "" && c.Server.PublicURL != "" {
		return fmt.Errorf("auth.jwt_secret required when server.public_url is set: it signs the public tracking and unsubscribe links, even with auth disabled (generate with `openssl rand -hex 32`)")
	}

	if c.Auth.JWTSecret != "" && len(c.Auth.JWTSecret) < 32 {
		return fmt.Errorf("auth.jwt_secret must be at least 32 characters (generate with `openssl rand -hex 32`)")
	}

	if !c.Auth.Disabled {
		if c.Auth.JWTSecret == "" {
			return fmt.Errorf("auth.jwt_secret required when auth is enabled (set auth.disabled: true to skip)")
		}

		if c.Auth.SessionTTL != "" {
			d, err := time.ParseDuration(c.Auth.SessionTTL)
			if err != nil || d <= 0 {
				return fmt.Errorf("auth.session_ttl %q invalid: want a Go duration like 12h or 30m", c.Auth.SessionTTL)
			}
		}

		// Local login is the only YAML-configured method now that
		// identity providers live in the database. It is also the
		// bootstrap path: an operator needs an account before there is
		// an admin API to configure SSO with, and a break-glass way back
		// in if a provider breaks.
		if !c.Auth.Local.Enabled {
			return fmt.Errorf("auth enabled but no method configured: enable auth.local (identity providers are configured at runtime under /api/oauth-providers)")
		}

		if c.Auth.Local.Email == "" {
			return fmt.Errorf("auth.local.email required (bootstrap user email)")
		}

		c.Auth.Local.Email = strings.TrimSpace(strings.ToLower(c.Auth.Local.Email))
	}

	if c.CORS.Enabled {
		if len(c.CORS.AllowedOrigins) == 0 {
			return fmt.Errorf("cors.enabled is true but cors.allowed_origins is empty (list the origins, or turn cors off)")
		}

		for _, o := range c.CORS.AllowedOrigins {
			o = strings.TrimSpace(o)
			if o == "" {
				return fmt.Errorf("cors.allowed_origins contains an empty entry")
			}

			// A wildcard with credentials is refused by every browser
			// anyway, and configuring it suggests the operator expects
			// cookies to work cross-origin. Fail loudly at startup
			// rather than let them debug a silent CORS rejection.
			if o == "*" && c.CORS.AllowCredentials {
				return fmt.Errorf("cors.allowed_origins cannot be \"*\" when cors.allow_credentials is true - list the exact origins instead")
			}

			if o != "*" && !strings.Contains(o, "://") {
				return fmt.Errorf("cors.allowed_origins entry %q must be a full origin including the scheme, e.g. https://app.example.com", o)
			}
		}

		if c.CORS.MaxAge < 0 {
			return fmt.Errorf("cors.max_age must not be negative")
		}
	}

	return nil
}

// TLSHost is the name a generated self-signed pair carries.
//
// Empty falls back to localhost in tlsbuild, which is the right answer
// for a scratch instance nobody gave a public URL.
func (c *Config) TLSHost() string { return publicURLHost(c.Server.PublicURL) }

// TLSEnabled reports whether any listener terminates TLS here, which is
// what decides whether a certificate has to be built at boot at all.
func (c *Config) TLSEnabled() bool {
	return c.Server.TLS.Enabled ||
		(c.Submission.Enabled && c.Submission.TLS.Enabled) ||
		(c.Inbound.Enabled && c.Inbound.TLS.Enabled)
}

// publicURLHost is the hostname out of server.public_url, without the
// port. Empty for anything unparseable, which leaves the fqdn as the
// operator wrote it and lets the ordinary validation complain.
func publicURLHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return ""
	}

	return u.Hostname()
}

// removedTLSKeys are the keys that used to configure a certificate and
// no longer do anything at all.
//
// Reported at boot, not refused. viper parses these without complaint,
// and silence would let an operator believe a mode they wrote is in
// force - while failing the boot over a redundant line is out of
// proportion.
//
// Listed leaf by leaf rather than by prefix, because viper.IsSet
// answers about a key and the operator wants to read the name they
// typed.
var removedTLSKeys = func() []string {
	var out []string
	for _, block := range []string{"server.tls", "submission.tls", "inbound.tls"} {
		for _, leaf := range []string{
			"mode", "cert", "key", "fqdn", "alg", "cachedir",
			"acme.hosts", "acme.email", "acme.http_addr", "acme.cachedir",
		} {
			out = append(out, block+"."+leaf)
		}
	}

	// The top-level acme block held these before they became platform
	// settings. Named too, or the file reads fine and the install
	// issues nothing.
	out = append(out, "acme.enabled", "acme.hosts", "acme.email")

	return out
}()
