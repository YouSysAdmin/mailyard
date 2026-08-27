package env

import (
	"context"
	"crypto/tls"
	"github.com/yousysadmin/mailyard/internal/core/bell"
	"log/slog"

	"github.com/yousysadmin/mailyard/internal/core/alertmail"
	"github.com/yousysadmin/mailyard/internal/core/audit"
	"github.com/yousysadmin/mailyard/internal/core/blob"
	"github.com/yousysadmin/mailyard/internal/core/cron"
	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/dispatch"
	"github.com/yousysadmin/mailyard/internal/core/emailverify"
	"github.com/yousysadmin/mailyard/internal/core/eventbus"
	"github.com/yousysadmin/mailyard/internal/core/notify"
	coreoidc "github.com/yousysadmin/mailyard/internal/core/oidc"
	"github.com/yousysadmin/mailyard/internal/core/queue"
	"github.com/yousysadmin/mailyard/internal/core/sessioncache"
	"github.com/yousysadmin/mailyard/internal/core/sestopics"
	"github.com/yousysadmin/mailyard/internal/core/settings"
	"github.com/yousysadmin/mailyard/internal/core/systemmail"
	"github.com/yousysadmin/mailyard/internal/core/tlsbuild"
	"github.com/yousysadmin/mailyard/internal/core/tracking"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/domain/store"
)

// Runtime is the server-scoped bag of dependencies. Built once in
// cli/serve.go and handed to every domain Handler so it can reach
// config, the logger, the DB, the aggregated Store, and the OIDC
// client.
type Runtime struct {
	Config *Config
	Log    *slog.Logger
	DB     database.Database
	Store  *store.Store

	// OAuth builds and caches an SSO flow per configured identity
	// provider. Providers live in the database and are editable at
	// runtime, so this is always set and may legitimately serve zero
	// providers.
	OAuth  *coreoidc.Registry
	Crypto *crypto.Service // always set, keyless = base64 fallback
	Queue  *queue.Worker   // the delivery worker, set in serve.go

	// Dispatch fans email lifecycle events out to subscribed
	// webhooks. Always set in serve.go.
	Dispatch *dispatch.Dispatcher

	// CampaignWake nudges the campaign runner (a plain func so env
	// does not import the campaign domain). Nil until serve.go wires
	// it.
	CampaignWake func()

	// RelayNodeTLS builds the transport for dialling one relay node:
	// our client certificate, the relay authority as the root, and
	// ServerName set to the host. A plain func for the same reason as
	// CampaignWake above - env cannot import the email domain, which
	// imports env.
	//
	// Nil where relay nodes are not configured, which is every
	// community installation. A caller holding a node row and no
	// builder must REFUSE rather than dial with the default config:
	// a node's certificate is signed by our own authority and can
	// never verify against the system roots.
	RelayNodeTLS func(ctx context.Context, host string) (*tls.Config, error)

	// RelayBell wakes the long-poll a pull relay node parks on the
	// claim route when a message is assigned to it. Always set. Rung
	// from the LISTEN/NOTIFY relay so an assignment made on a worker
	// node reaches a poll parked on an api node.
	RelayBell *bell.Bell

	// Tracking signs the public /t/ URLs. Always set in serve.go,
	// disabled (Enabled() false) when server.public_url is empty.
	Tracking *tracking.Signer

	// Blob is the attachment object store. Nil means inline storage
	// (base64 in the database).
	Blob blob.Store

	// SystemMail sends the platform's own mail (invitations, password
	// resets). Always set in serve.go, but Enabled() reports false
	// unless the operator configured system_mail - every caller must
	// keep working without it.
	SystemMail *systemmail.Sender

	// Settings serves platform settings from memory. Always set in
	// serve.go, seeded with registry defaults before the first load.
	Settings *settings.Service

	// SESTopics caches which SNS topics are configured on a server, so
	// the public SES receiver does not query the database per request
	// and an edit in the console takes effect at once.
	SESTopics *sestopics.Allowlist

	// TLS is the certificate builder this process used. Held so the
	// admin surface can report what ACME holds and force a renewal -
	// nothing else may build a TLS config, and a second one would mean
	// a second challenge listener.
	//
	// nil on a process that built no TLS at all, which every caller
	// has to tolerate.
	TLS *tlsbuild.Builder

	// Cron runs scheduled maintenance. Always set in serve.go.
	Cron *cron.Manager

	// Audit records the operational and security trails. Always set
	// in serve.go. Writes are async and best effort - a nil Recorder
	// is a valid no-op.
	Audit *audit.Recorder

	// Events fans project activity out to server-sent event
	// subscribers. In process, so on a multi-node deployment a live
	// stream only shows events raised by the node serving it - the
	// durable tables remain the authority. Always set in serve.go.
	Events *eventbus.Bus

	// Alerts mails somebody when access changes or a project has a
	// problem. Always set, and a no-op until platform mail is configured
	// and security_alerts_enabled is on - so every caller can hand it an
	// event without asking.
	Alerts *alertmail.Notifier

	// Notify files in-app notifications and pushes them to the live
	// stream. Always set in serve.go, and every call is best effort -
	// a notification that cannot be filed must not fail its caller.
	Notify *notify.Raiser

	// Sessions caches session revocation checks so the auth path does
	// not read a row per request. Always set in serve.go.
	Sessions *sessioncache.Cache

	// Verifier judges recipient addresses. Nil when
	// email_verify.enabled is false, and the endpoint then reports
	// the feature as off rather than pretending.
	Verifier *emailverify.Verifier
}
