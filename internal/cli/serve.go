// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/yousysadmin/mailyard/internal/server"

	"github.com/yousysadmin/mailyard/internal/core/alertmail"
	"github.com/yousysadmin/mailyard/internal/core/attachcache"
	coreaudit "github.com/yousysadmin/mailyard/internal/core/audit"
	"github.com/yousysadmin/mailyard/internal/core/authenticator"
	"github.com/yousysadmin/mailyard/internal/core/bell"
	"github.com/yousysadmin/mailyard/internal/core/blob"
	"github.com/yousysadmin/mailyard/internal/core/cron"
	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/dispatch"
	"github.com/yousysadmin/mailyard/internal/core/emailverify"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/eventbus"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/core/metrics"
	"github.com/yousysadmin/mailyard/internal/core/notify"
	coreoidc "github.com/yousysadmin/mailyard/internal/core/oidc"
	"github.com/yousysadmin/mailyard/internal/core/partition"
	"github.com/yousysadmin/mailyard/internal/core/queue"
	"github.com/yousysadmin/mailyard/internal/core/safego"
	"github.com/yousysadmin/mailyard/internal/core/sessioncache"
	"github.com/yousysadmin/mailyard/internal/core/sestopics"
	"github.com/yousysadmin/mailyard/internal/core/settings"
	"github.com/yousysadmin/mailyard/internal/core/systemmail"

	"github.com/yousysadmin/mailyard/internal/core/tlsbuild"
	coretracking "github.com/yousysadmin/mailyard/internal/core/tracking"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/postgres"
	"github.com/yousysadmin/mailyard/internal/domain/campaign"
	"github.com/yousysadmin/mailyard/internal/domain/certificate"
	"github.com/yousysadmin/mailyard/internal/domain/email"
	"github.com/yousysadmin/mailyard/internal/domain/relaynode"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	"github.com/yousysadmin/mailyard/internal/domain/webhook"
	campaignmodel "github.com/yousysadmin/mailyard/internal/models/campaign"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
	smodel "github.com/yousysadmin/mailyard/internal/models/setting"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
	webhookmodel "github.com/yousysadmin/mailyard/internal/models/webhook"
)

// statsRollupDays is the window the trend rollup recomputes.
//
// Fifteen: the chart offers fourteen days, and the extra one keeps the
// oldest bar correct through a timezone's worth of edge. Wider costs a
// longer scan every ten minutes and buys nothing the chart can draw.
const statsRollupDays = 15

// shutdownTimeout bounds the wait for in-flight HTTP requests once
// the queue, the SMTP listeners and the event streams have already
// been stopped. Anything still running at that point is not going to
// finish, and a signal that does not stop the process is worse than a
// dropped request.
const shutdownTimeout = 15 * time.Second

// drainTimeouts bounds each stage of a shutdown.
type drainTimeouts struct {
	server, smtp, runner, worker, cron, dispatch, audit time.Duration
}

// role selects which halves of the process a node runs.
//
// A subcommand rather than a config key, deliberately. The whole
// point of splitting roles is to put more machines behind one queue,
// and a config key would mean each of those machines needs its own
// config file differing in a single line - the exact thing that rots
// in a fleet. Here every node ships the same mailyard.yaml (or the
// same MAILYARD_* environment) and the role is a word in argv, which
// is already per-container in every orchestrator there is.
type role struct {
	// api serves HTTP - console, API, tracking - and owns the SMTP
	// submission and inbound listeners, which are ingress like the API is.
	api bool

	// worker drains the delivery queue, runs campaigns and performs
	// the scheduled maintenance sweeps.
	worker bool
}

// String renders role for a log line.
func (r role) String() string {
	switch {
	case r.api && r.worker:
		return "api+worker"
	case r.api:
		return "api"
	default:
		return "worker"
	}
}

// initFlag is how a fleet names the one node that applies the schema.
//
// Two processes starting together against an empty database race each
// other, and it comes out as a missing goose_db_version on one plus a
// duplicate key from Postgres' catalogue. So one node runs with --init
// and the rest without.
//
// A single node needs it too. Off by default because on would restore
// the race for anyone who does not know the flag exists, where off
// fails loudly on an unmigrated database.
func withInit(c *cobra.Command) *cobra.Command {
	c.Args = noArgs
	c.Flags().Bool("init", false,
		"apply pending database migrations before starting - exactly one node in a fleet should")

	return c
}

func newServeCmd() *cobra.Command {
	return withInit(&cobra.Command{
		Use:   "serve",
		Short: "Start everything: API server and delivery worker",
		Long: "Start a full node - HTTP API and console, SMTP listeners, delivery\n" +
			"queue, campaigns and maintenance jobs. This is the single-binary\n" +
			"default. Several serve nodes can run against one database.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd, role{api: true, worker: true})
		},
	})
}

// The api and worker commands live in roles.go. Every role runs this
// one bootstrap and branches on r - a second copy of eight hundred
// lines per role would be a second copy to keep in step.
func runServe(cmd *cobra.Command, r role) error {
	configPath, _ := cmd.Flags().GetString("config")

	cfg, err := env.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("config invalid: %w", err)
	}

	log, err := buildLogger(cfg.Logging)
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}

	// Make the configured logger the package-level default too.
	//
	// About fifty call sites log through the package functions rather
	// than an injected logger. Without this they went to slog's own
	// handler - a third format, on stderr, ignoring logging.format,
	// output and level alike, so an operator shipping JSON to a file
	// got half the log as text on the terminal.
	slog.SetDefault(log)
	initDB, _ := cmd.Flags().GetBool("init")
	log.Info("mailyard starting", "role", r.String(), "init", initDB, "config", cfg.Source)

	// Keys that still parse and no longer do anything. Silence here would
	// let an operator believe a mode they wrote is in force - see
	// removedTLSKeys.
	if removed := cfg.RemovedKeys; len(removed) > 0 {
		log.Warn("these settings no longer exist and are ignored - a listener now only chooses whether to terminate TLS, and which certificate it serves is assigned in the console",
			"keys", removed)
	}

	// The two settings that make this sit on different lines and
	// neither mentions the other - see MetricsExposedWithoutToken.
	if cfg.MetricsExposedWithoutToken() {
		log.Warn("metrics are reachable beyond this host and no metrics.token gates them - a scrape reports sending volume, queue depth and failure rates to anyone who asks: set metrics.token, or bind metrics.addr to loopback",
			"addr", cfg.Metrics.Addr)
	}

	// Wire the JSON-body validator + custom rules before any handler
	// can run. BindAndValidate panics if Init hasn't run, so this
	// must precede server.New.
	validation.Init()

	// The crypto service is built before the stores because stores
	// with secret columns (smtp servers) encrypt through it.
	// No keyless branch to warn about: Config.Validate requires the
	// key, so this service always has one.
	cryptoSvc := crypto.New(cfg.Database.Crypto.EncryptionKey)

	db, st, err := openDatabase(&cfg.Database, cryptoSvc, initDB)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}

	defer func() { _ = db.Close() }()

	// A serving node's code and schema have to agree. Without --init the
	// open above only checks that SOME schema is there, so an upgraded
	// binary would start happily on the previous version's schema - see
	// postgres.RequireCurrentSchema for what that looked like.
	if !initDB {
		if err := postgres.RequireCurrentSchema(db.DB()); err != nil {
			return err
		}
	}

	warnBounceAddressUnreachable(cmd.Context(), cfg, st, log)

	// Identity providers are configured at runtime and live in the
	// database, so there is nothing to discover at startup - the
	// registry builds and caches a flow the first time each provider
	// is used. It needs the public URL to derive redirect URIs,
	// because the IdP has to reach us by our external name.
	oauthRegistry := coreoidc.NewRegistry(cfg.Server.PublicURL,
		coreoidc.NewHTTPClient(cfg.Auth.OIDC.AllowPrivateTargets))
	if cfg.Server.PublicURL == "" {
		log.Warn("auth: server.public_url is empty, so SSO redirect URIs cannot be built - set it before configuring an identity provider")
	}

	rt := &env.Runtime{
		Config: cfg,
		Log:    log,
		DB:     db,
		Store:  st,
		OAuth:  oauthRegistry,
		Crypto: cryptoSvc,
		Events: eventbus.New(),
	}

	// Attachment blob store: nil keeps attachments inline (base64 in
	// the database), fs or s3 offloads the bytes.
	rt.Blob, err = blob.New(blob.Config{
		Backend:        cfg.Storage.Backend,
		FSPath:         cfg.Storage.FSPath,
		S3Endpoint:     cfg.Storage.S3.Endpoint,
		S3Region:       cfg.Storage.S3.Region,
		S3Bucket:       cfg.Storage.S3.Bucket,
		S3AccessKey:    cfg.Storage.S3.AccessKey,
		S3SecretKey:    cfg.Storage.S3.SecretKey,
		S3UsePathStyle: cfg.Storage.S3.UsePathStyle,
	})
	if err != nil {
		return fmt.Errorf("blob store init: %w", err)
	}

	if rt.Blob != nil {
		log.Info("attachment storage", "backend", cfg.Storage.Backend)
	}

	rt.Attachments = attachcache.New(cfg.Worker.AttachmentCacheBytes, attachcache.DefaultTTL)

	rt.Sessions = sessioncache.New()

	// Which SNS topics belong to a server. Cached because the SES
	// receiver is public and unauthenticated - the topic check is the
	// first thing a request reaches, and a query per call would let
	// anyone who found the URL generate database load at will.
	rt.SESTopics = sestopics.NewAllowlist(db.DB())

	// Address verification. Off by default because the MX check makes
	// outbound DNS queries.
	if cfg.EmailVerify.Enabled {
		rt.Verifier = emailverify.New(emailverify.Config{
			CacheTTL:      cfg.EmailVerify.CacheTTL,
			MXCacheTTL:    cfg.EmailVerify.MXCacheTTL,
			LookupTimeout: cfg.EmailVerify.LookupTimeout,
		})
		log.Info("email verification enabled")
	}

	// Audit trail. Started before anything that records to it, and
	// drained during shutdown before the database closes.
	rt.Audit = coreaudit.New(st.Audit, log)

	// Platform settings: registry defaults, then any stored
	// overrides. Loaded before anything reads a setting.
	rt.Settings = settings.New(st.Setting)
	if err := rt.Settings.Reload(context.Background()); err != nil {
		return fmt.Errorf("load settings: %w", err)
	}

	// The relay identity a worker presents to a relay node. The
	// delivery processor takes it directly, the console's Test button
	// reads it off the Runtime.
	relayClient := relayClientSource(cfg, st)
	if relayClient != nil {
		rt.RelayNodeTLS = relayClient.WorkerTLS
	}

	// Platform mail (invitations, password resets, signup
	// confirmations, alerts). Always present, Enabled() false until an
	// admin sets an address and a project - every call site degrades
	// to copyable links.
	//
	// It is a message of the project platform_mail_project names and
	// goes out through that project's servers, so there is nothing to
	// configure twice and the mail is signed with the project's DKIM
	// key. The queue it goes into is handed over below, once the
	// worker exists.
	rt.SystemMail = systemmail.New(func() systemmail.Address {
		return systemmail.Address{
			From:     rt.Settings.String(smodel.KeyPlatformMailFrom),
			FromName: rt.Settings.String(smodel.KeyPlatformMailFromName),
			Project:  rt.Settings.String(smodel.KeyPlatformMailProject),
		}
	}, log)
	if rt.SystemMail.Enabled() {
		log.Info("platform mail enabled", "from", rt.SystemMail.From(), "project_id", rt.SystemMail.Project())
	} else {
		log.Warn("platform mail disabled, invitations return copyable links and password reset is unavailable - set platform_mail_from and platform_mail_project")
	}

	// Alert mail, off the audit stream. One watcher, because both
	// trails already meet in the recorder's writer goroutine.
	//
	// On every role: an api node is where a key is created, a worker
	// node is where the bounce alert is raised.
	rt.Alerts = &alertmail.Notifier{
		Mail:       rt.SystemMail,
		Recipients: st.AlertRecipients,
		Enabled:    func() bool { return rt.Settings.Bool(smodel.KeySecurityAlertsEnabled) },
		ConsoleURL: strings.TrimRight(cfg.Server.PublicURL, "/") + env.ConsolePath,
		Log:        log,
	}
	if cfg.Server.PublicURL == "" {
		// The mail still goes, it just carries no link. Said out loud
		// because "open the log" is the whole point of the nudge.
		rt.Alerts.ConsoleURL = ""
	}

	rt.Audit.Watch(rt.Alerts.OnAudit)

	// Auth bootstrap: with local login on and no user yet, mint one
	// and print the password to stderr once.
	//
	// api role only - a worker node's stderr is the one nobody
	// watches, and minting there burns the only chance to read it.
	if r.api && !cfg.Auth.Disabled && cfg.Auth.Local.Enabled {
		if err := bootstrapUser(context.Background(), rt); err != nil {
			return fmt.Errorf("auth bootstrap: %w", err)
		}
	}

	// SSO-only mints nobody. With no account at all nobody could ever
	// configure a provider, so that refuses the boot.
	if r.api && !cfg.Auth.Disabled && !cfg.Auth.Local.Enabled {
		if err := requireAnAccount(context.Background(), rt); err != nil {
			return err
		}

		log.Info("local sign-in is off, accounts sign in through identity providers only. " +
			"To get back in without one, set auth.local.enabled true and use set-password")
	}

	// Tenancy bootstrap: the auth-disabled mode has no users, so
	// tenant-scoped routes fall back to a shared default project.
	// Mint it up front so the first request does not race to create it.
	if cfg.Auth.Disabled {
		// Said out loud, at warn, once per boot. auth.disabled makes
		// every request an owner of the project it names, with the full
		// permission catalogue and no credential - and an installation
		// left in it by a copied config or a stray env var otherwise
		// looks exactly like a working one.
		//
		// Warn rather than refuse: it is a supported mode for a
		// single-tenant box behind somebody else's gateway.
		log.Warn("AUTHENTICATION IS DISABLED - every request is treated as a project owner " +
			"with every permission and no credential is required. Set auth.disabled: false " +
			"(or unset MAILYARD_AUTH_DISABLED) unless this instance sits behind a gateway " +
			"that authenticates for it")

		if _, err := rt.Store.Project.EnsureDefault(context.Background()); err != nil {
			return fmt.Errorf("project bootstrap: %w", err)
		}
	}

	// Webhook dispatcher: fans email lifecycle events out to
	// subscribed endpoints. Built before the worker so the worker's
	// finalize hook can emit through it.
	dispatcher := dispatch.New(&webhook.DispatchSink{
		Store: st.Webhook, Audit: rt.Audit,
		Notify: func() *notify.Raiser { return rt.Notify },
	}, dispatch.Config{
		Timeout:             cfg.Webhook.Timeout,
		MaxAttempts:         cfg.Webhook.MaxAttempts,
		RetryDelay:          cfg.Webhook.RetryDelay,
		AllowPrivateTargets: cfg.Webhook.AllowPrivateTargets,
	}, log)
	rt.Dispatch = dispatcher

	// Delivery worker: the email store is its queue source, the email
	// processor its delivery leg. Started before the HTTP server so
	// rows left queued by a previous run drain immediately. relayClient
	// is resolved beside SystemMail above - one identity for the three
	// places a node is dialled from: the worker, the console's Test
	// button (see smtpserver.testTransport) and platform mail.
	processor := &email.Processor{
		Store:            st,
		Log:              log,
		AutoSuppress:     cfg.Sending.AutoSuppressOnReject,
		AllowPrivateSMTP: cfg.Sending.AllowPrivateSMTPTargets,
		PlatformProject:  func() string { return rt.Settings.String(smodel.KeyPlatformMailProject) },
		Blob:             rt.Blob,
		Attachments:      rt.Attachments,
		BounceAddress:    strings.TrimSpace(cfg.Sending.BounceAddress),
		RelayClient:      relayClient,
		Notify:           func() *notify.Raiser { return rt.Notify },
	}
	worker := queue.NewWorker(st.Email, processor, queue.Config{
		Concurrency:    cfg.Worker.Concurrency,
		PollInterval:   cfg.Worker.PollInterval,
		MaxAttempts:    cfg.Worker.MaxAttempts,
		RetryBaseDelay: cfg.Worker.RetryBaseDelay,
		RetryMaxDelay:  cfg.Worker.RetryMaxDelay,
		ClaimTimeout:   cfg.Worker.ClaimTimeout,
		AttemptTimeout: cfg.Worker.AttemptTimeout,
	}, log)

	worker.OnFinal = finalHook(rt, st, dispatcher, log)

	rt.Queue = worker
	// WithCancelCause so the long-lived loops can report WHY they
	// stopped. Three paths cancel this and they mean different things -
	// a deferred cleanup, a signal, and a listener that failed - and
	// ctx.Err() is context.Canceled for all three.
	workerCtx, stopWorker := context.WithCancelCause(context.Background())
	defer stopWorker(nil)

	// Tracking signer: mints the public /tracking/ URLs. Needs the
	// public base URL to build absolute links - without it campaigns
	// send untracked.
	//
	// Keyed on the encryption key, which is not rotated casually,
	// because unsubscribe links never expire. The session secret stays
	// as a verify-only previous key for links minted under it.
	rt.Tracking = coretracking.NewSigner(cfg.Server.PublicURL,
		crypto.DeriveKey(cfg.Database.Crypto.EncryptionKey, crypto.KeyTracking),
		crypto.DeriveKey(cfg.Auth.JWTSecret, crypto.KeyTracking))

	// The keys a rekey retired keep delivered unsubscribe links working.
	// Not fatal: without them only those old links fail.
	if retired, err := st.TrackingKey.Retired(cmd.Context()); err != nil {
		log.Warn("tracking: retired keys could not be read, unsubscribe links minted before a rekey will be refused", "err", err)
	} else {
		rt.Tracking.Retire(retired...)
	}

	if !rt.Tracking.Enabled() {
		log.Warn("tracking: disabled, set server.public_url to enable open/click tracking and hosted unsubscribe pages")
	}

	// Campaign runner: drains sending campaigns into the email queue
	// in throttled batches. Built after the queue worker so its email
	// service can wake it.
	runner := campaign.NewRunner(st, email.NewService(rt), log,
		dispatcher.Emit, rt.Tracking, cfg.Campaign.BatchSize, cfg.Campaign.PollInterval)
	runner.Notify = func() *notify.Raiser { return rt.Notify }
	rt.CampaignWake = runner.Wake
	rt.CampaignEmailCancelled = func(ctx context.Context, projID, emailID string) {
		if err := campaign.Settle(ctx, st, dispatcher.Emit, rt.Notify,
			projID, emailID, campaignmodel.MsgSkipped, "email cancelled"); err != nil {
			log.Error("campaign: settle cancelled message", "email_id", emailID, "err", err)
		}
	}

	// Platform mail goes in through the same door as a tenant's, which
	// is why this waits for the queue: a service built before rt.Queue
	// cannot wake the worker.
	rt.SystemMail.UseProject(email.NewService(rt))

	// Cross-node wake. Once the roles are split the node that accepts
	// a send is routinely not the one that delivers it, so without
	// this every send waits out a poll interval tuned for keeping an
	// idle cluster quiet.
	//
	// Broadcast is set on every role, the subscriptions only on a
	// worker. The listener subscribes with WakeLocal, never Wake - see
	// the note on Worker.WakeLocal.
	listener := postgres.NewListener(db.DB(), cfg.Database.DSN, log)
	worker.Broadcast = func() { listener.Notify(postgres.ChannelEmailQueue) }
	runner.Broadcast = func() { listener.Notify(postgres.ChannelCampaign) }
	if r.worker {
		listener.Subscribe(postgres.ChannelEmailQueue, worker.WakeLocal)
		listener.Subscribe(postgres.ChannelCampaign, runner.WakeLocal)
	}

	// The relay bell is rung from the same relay, on every role: the
	// claim long-poll parks on an api node and the assignment is made
	// on a worker node.
	rt.RelayBell = &bell.Bell{}
	listener.Subscribe(postgres.ChannelRelayAssign, rt.RelayBell.Ring)

	// A revoke on one node clears every node's cache. Subscribed with
	// the LOCAL clear so the broadcast does not rebroadcast itself.
	rt.Sessions.Broadcast = func() { listener.Notify(postgres.ChannelSessions) }
	listener.Subscribe(postgres.ChannelSessions, rt.Sessions.InvalidateAllLocal)

	// The pull-node seam: a candidate that is a node in pull mode is
	// assigned to rather than dialled, and the assignment rings every
	// claim long-poll through the same LISTEN/NOTIFY relay the queue
	// wake uses.
	processor.Pull = &relaynode.Pull{
		Store:  rt.Store,
		TTL:    rt.Config.RelayNodes.AssignmentTTL,
		Notify: func() { listener.Notify(postgres.ChannelRelayAssign) },
	}

	listener.Start(workerCtx)

	// TLS: one builder for every listener so identical blocks share a
	// single certificate (and a single ACME challenge listener)
	// instead of each mode being materialized three times over.
	//
	// Store is what puts a MINTED certificate in the database rather
	// than in a directory on this node. The ACME cache and the
	// self-signed pair are both installation-wide, and a per-node copy
	// of either is what made several nodes order several certificates
	// and serve several different self-signed ones.
	tlsBuilder := tlsbuild.Builder{
		Store: st.Certificate,
		Log:   log,

		// The name a generated self-signed pair carries. One derivation
		// from server.public_url, which is already the name this
		// installation calls itself.
		Host: cfg.TLSHost(),

		// Read fresh on every handshake and every order, because all of
		// it is platform settings - an administrator turns ACME on and
		// names a host without restarting anything.
		ACME: func() tlsbuild.ACME {
			return tlsbuild.ACME{
				Enabled:      rt.Settings.Bool(smodel.KeyACMEEnabled),
				Hosts:        smodel.StringList(rt.Settings.String(smodel.KeyACMEHosts)),
				Email:        rt.Settings.String(smodel.KeyACMEEmail),
				DirectoryURL: rt.Settings.String(smodel.KeyACMEDirectoryURL),
			}
		},
		ChallengeAddr: cfg.ACME.ChallengeAddr,

		// Which managed certificate each listener serves, BY LISTENER.
		// Keying on the TLS block instead and inferring the listener by
		// comparing values cannot work: the ordinary configuration is
		// three identical blocks, so the HTTP listener matches the
		// submission one and reads a setting nobody wrote.
		Assigned: func(listener string) string {
			return rt.Settings.String(certificate.SettingFor(listener))
		},
	}
	rt.TLS = &tlsBuilder
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if terr := tlsBuilder.Shutdown(ctx); terr != nil {
			log.Warn("tls shutdown", "err", terr)
		}
	}()

	// Scheduled maintenance. Every job is idempotent and safe to run
	// concurrently on several nodes, so no leader election is needed.
	rt.Cron = cron.New(log)

	// Alerts: an in-app notification that means something is WRONG also
	// goes out as mail. The raiser calls it rather than alertmail
	// watching the event bus, because a notification is not an audit
	// event - nobody made a request, a job noticed something.
	rt.Notify = &notify.Raiser{Store: st, Bus: rt.Events, Log: log, Alerts: rt.Alerts}

	registerJobs(cmd.Context(), r, rt, st, db, log)

	// The worker and the runner are CONSTRUCTED on every role, and only
	// STARTED on a worker one. An api node still hands rt.Queue to the
	// email service, whose Wake broadcasts - so accepting a send on an
	// api node is what tells the worker nodes to look. Leaving rt.Queue
	// nil there would drop that signal and put every send back on the
	// poll interval.
	//
	// Started here, after everything they read is wired: Broadcast,
	// processor.Pull and rt.Notify are plain fields, read from the loop
	// goroutines without a lock.
	if r.worker {
		// safego.Go on the long-lived loops: each already guards its
		// own unit of work, this catches a panic in the loop
		// machinery itself rather than letting it end the process.
		safego.Go(log, "queue: worker loop", func() { worker.Start(workerCtx) })
		safego.Go(log, "campaign: runner loop", func() { runner.Start(workerCtx) })
	}

	safego.Go(log, "cron: scheduler loop", func() { rt.Cron.Start(workerCtx) })

	listeners, err := startSMTP(r, cfg, rt, st, &tlsBuilder, log)
	if err != nil {
		return err
	}

	if cfg.Metrics.Enabled {
		metrics.RegisterQueueCollector(st.Email.CountAllByStatus)

		// Registered on EVERY role, unlike the ceiling alert, which is
		// a worker job. The count is a property of the database rather
		// than of the node, so whichever node is scraped answers it -
		// and an api-only deployment has no worker to ask.
		partsMetric := &partition.Maintainer{DB: db.DB(), Log: log}
		metrics.RegisterPartitionCollector(func(ctx context.Context) (int, int, error) {
			h, err := partsMetric.Count(ctx)

			return h.Partitions, h.Ceiling, err
		})
	}

	serverTLS, err := tlsBuilder.Build(certificate.ListenerServer, cfg.Server.TLS.Enabled)
	if err != nil {
		return fmt.Errorf("server tls: %w", err)
	}

	// A worker still binds server.addr, but serves only the probes.
	// Serving nothing makes a worker unschedulable: an orchestrator
	// needs a liveness endpoint. Running the full console there would
	// instead put the whole authenticated surface on a machine that has
	// no reason to expose it.
	srv, err := server.New(server.Options{Runtime: rt, TLS: serverTLS, HealthOnly: !r.api})
	if err != nil {
		return fmt.Errorf("server init: %w", err)
	}

	// The scrape endpoint, on metrics.addr rather than as a route on the
	// one above. nil when metrics are off, and every role runs it.
	metricsSrv := server.NewMetrics(cfg)

	// Two listeners, ONE channel: either failing is a boot failure that
	// takes the process down. The nil case is a BRANCH rather than a
	// nil-safe Start, since returning immediately is how this channel
	// says stop.
	errCh := make(chan error, 2)
	go func() { errCh <- srv.Start() }()

	if metricsSrv != nil {
		go func() { errCh <- metricsSrv.Start() }()
	}

	// drain is the one shutdown sequence, whatever started it. Order:
	// stop the scheduling, end the event streams (or the drain runs to
	// its full timeout on every open console tab), drain the requests,
	// then stop the SMTP listeners, the runner, the worker and the jobs,
	// and only then close the consumers they all write into - a closed
	// recorder drops what handlers still record. db.Close is deferred
	// and runs last.
	//
	// stopWorker runs here and not only in the defer, which cannot fire
	// until runServe returns - and it is about to block on shutdown, so
	// scheduled jobs kept firing for the whole drain.
	//
	// It answers the HTTP drain's error and nothing else.
	drain := func(cause error, t drainTimeouts) error {
		stopWorker(cause)
		rt.Events.Close()

		shutdownErr := srv.Shutdown(t.server)
		listeners.stop(t.smtp)
		runner.Stop(t.runner)
		worker.Stop(t.worker)
		rt.Cron.Wait(t.cron)
		dispatcher.Close(t.dispatch)
		rt.Audit.Close(t.audit)

		// Logged rather than returned: a scrape must not decide the
		// exit status of a clean shutdown.
		if err := metricsSrv.Shutdown(5 * time.Second); err != nil {
			log.Warn("metrics shutdown", "err", err)
		}

		return shutdownErr
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	select {
	case s := <-sigCh:
		log.Info("shutdown requested", "signal", s.String())

		return drain(fmt.Errorf("signal %s", s), drainTimeouts{
			server: shutdownTimeout, smtp: 10 * time.Second, runner: 10 * time.Second,
			worker: 30 * time.Second, cron: 30 * time.Second, dispatch: 10 * time.Second,
			audit: 5 * time.Second,
		})

	case err := <-errCh:
		// One listener failing takes the OTHER down with it, rather than
		// leaving a survivor serving under a config just reported
		// broken. Both are no-ops on the one that already stopped.
		const quick = 5 * time.Second
		if serr := drain(fmt.Errorf("listener failed: %w", err), drainTimeouts{
			server: quick, smtp: quick, runner: quick, worker: quick, cron: quick, dispatch: quick, audit: quick,
		}); serr != nil {
			log.Warn("server shutdown", "err", serr)
		}

		return err
	}
}

// openDatabase connects to PostgreSQL and binds the per-domain
// stores. The DSN is checked by Config.Validate.
func openDatabase(cfg *env.DatabaseConfig, cr *crypto.Service, migrate bool) (database.Database, *store.Store, error) {
	pg, err := postgres.OpenWith(cfg.DSN, cfg.ReplicaDSNs, migrate, postgres.Pool{
		MaxOpen:     cfg.MaxOpenConns,
		MaxIdle:     cfg.MaxIdleConns,
		MaxLifetime: cfg.ConnMaxLifetime,
		MaxIdleTime: cfg.ConnMaxIdleTime,
	})
	if err != nil {
		return nil, nil, err
	}

	return pg, postgres.BindStore(pg, cr, cfg.ReplicaReads), nil
}

// bootstrapUser inserts the first operator user when the users table is empty.
// Generates a 16-char random password, hashes it, and
// LOGS THE PLAINTEXT ONCE so the operator can copy it.
// Subsequent starts find the user and no-op.
// requireAnAccount refuses an SSO-only boot against an empty users
// table, where nobody could sign in to configure an identity provider.
func requireAnAccount(ctx context.Context, rt *env.Runtime) error {
	count, err := rt.Store.User.Count(ctx)
	if err != nil {
		return fmt.Errorf("count users: %w", err)
	}

	if count == 0 {
		return errors.New("no account exists and local sign-in is off - set auth.local.enabled true " +
			"and auth.local.email to create the first administrator, then configure an identity provider")
	}

	return nil
}

func bootstrapUser(ctx context.Context, rt *env.Runtime) error {
	count, err := rt.Store.User.Count(ctx)
	if err != nil {
		return fmt.Errorf("count users: %w", err)
	}

	if count > 0 {
		return nil
	}

	plain, err := authenticator.GeneratePassword()
	if err != nil {
		return fmt.Errorf("generate password: %w", err)
	}

	hash, err := authenticator.HashPassword(plain)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	u := &usermodel.User{
		ID:            ids.New(),
		Email:         rt.Config.Auth.Local.Email,
		PasswordHash:  hash,
		AccountType:   usermodel.AccountLocal,
		EmailVerified: true,
	}

	// The count above is the cheap answer, this is the locked one.
	first, err := rt.Store.User.PutFirst(ctx, u, true)
	if err != nil {
		return fmt.Errorf("put user: %w", err)
	}

	if !first {
		return nil
	}

	// No project is created here. The first admin signs in, lands on the
	// projects page and makes one, which leaves an install nobody ever
	// uses with nothing in it.
	fmt.Fprintf(os.Stderr,
		"\n========================================================\n"+
			"AUTH BOOTSTRAP: created user %s\n"+
			"  password: %s\n"+
			"  (this is the only time the password is shown)\n"+
			"========================================================\n\n",
		u.Email, plain)
	rt.Log.Info("auth: bootstrap user created", "email", u.Email, "user_id", u.ID)

	return nil
}

// finalHook is the worker's OnFinal, run once a message reaches a final
// status. It reads rt.Notify when it runs rather than when it is built,
// because the raiser is set after the worker.
func finalHook(rt *env.Runtime, st *store.Store, dispatcher *dispatch.Dispatcher, log *slog.Logger) func(*emailmodel.Email, string, string, []string) {
	return func(job *emailmodel.Email, status, errMsg string, refused []string) {
		metrics.EmailsFinalized.WithLabelValues(status).Inc()

		// Platform mail through a project is not the project's: no
		// campaign to mark, no contact to record, nothing for its live
		// feed or its webhooks. Finalize already cleared the body.
		if job.System {
			return
		}

		// Sync the campaign message (no-op for transactional sends).
		msgStatus := map[string]string{
			emailmodel.StatusSent:       campaignmodel.MsgSent,
			emailmodel.StatusFailed:     campaignmodel.MsgFailed,
			emailmodel.StatusSuppressed: campaignmodel.MsgSkipped,
		}[status]
		if msgStatus != "" {
			if err := campaign.Settle(context.Background(), st, dispatcher.Emit, rt.Notify,
				job.ProjectID, job.ID, msgStatus, errMsg); err != nil {
				log.Error("campaign: settle message", "email_id", job.ID, "err", err)
			}
		}

		// Fold the outcome into the per-recipient contact tallies.
		// Only terminal sent/failed count: a suppressed message was
		// never attempted, so recording it as a failure would blame
		// the address for a decision we made about it.
		if status == emailmodel.StatusSent || status == emailmodel.StatusFailed {
			trackContacts(context.Background(), st, log, job, status == emailmodel.StatusSent, refused)
		}

		// Push to any live console viewer. Best effort and
		// non-blocking - see core/eventbus.
		if busType := map[string]string{
			emailmodel.StatusSent:   eventbus.TypeEmailSent,
			emailmodel.StatusFailed: eventbus.TypeEmailFailed,
		}[status]; busType != "" {
			rt.Events.Publish(eventbus.Event{
				Type:      busType,
				ProjectID: job.ProjectID,
				Data: map[string]any{
					"id":         job.ID,
					"subject":    job.Subject,
					"recipients": job.Recipients,
					"status":     status,
					"error":      errMsg,
				},
			})
		}

		event := map[string]string{
			emailmodel.StatusSent:       webhookmodel.EventEmailSent,
			emailmodel.StatusFailed:     webhookmodel.EventEmailFailed,
			emailmodel.StatusSuppressed: webhookmodel.EventEmailSuppressed,
		}[status]
		if event == "" {
			return
		}

		job.Status = status
		job.ErrorMessage = errMsg
		dispatcher.Emit(context.Background(), job.ProjectID, event, job.Sender, email.EventPayload(job))
	}
}

// trackContacts records one terminal delivery outcome against each
// recipient's contact row. A recipient the server refused on a message
// it took for the others counts as a failure for that address alone.
//
// Best effort: a contact tally is reporting, not delivery. A failure is
// logged and dropped rather than retried - the message has already been
// delivered or permanently failed, so there is nothing to roll back.
//
// Runs inline in the worker's finalize hook: one upsert per recipient
// per message, the same order of writes a campaign already does.
func trackContacts(ctx context.Context, st *store.Store, log *slog.Logger, job *emailmodel.Email, sent bool, refused []string) {
	now := time.Now().UTC()

	for _, raw := range job.Recipients {
		addr, name := splitRecipient(raw)
		if addr == "" {
			continue
		}

		delivered := sent && !slices.ContainsFunc(refused, func(r string) bool { return strings.EqualFold(r, addr) })
		if err := st.Contact.RecordOutcome(ctx, job.ProjectID, addr, name, delivered, now); err != nil {
			log.Warn("contacts: record outcome failed",
				"email_id", job.ID, "recipient", addr, "err", err)
		}
	}
}

// splitRecipient pulls the address and any display name out of a
// recipient entry, which may be either "a@b.com" or "Alice <a@b.com>".
func splitRecipient(raw string) (addr, name string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}

	if parsed, err := mail.ParseAddress(raw); err == nil {
		return parsed.Address, parsed.Name
	}

	return raw, ""
}

// warnBounceAddressUnreachable says so at boot when reports sent to
// sending.bounce_address would arrive nowhere.
//
// Both halves are easy to get half-right and impossible to notice
// afterward: the reports simply stop, and nothing in the product
// says why. A provider will happily forward bounces to an address
// that refuses them for months.
func warnBounceAddressUnreachable(ctx context.Context, cfg *env.Config, st *store.Store, log *slog.Logger) {
	addr := strings.TrimSpace(cfg.Sending.BounceAddress)
	if addr == "" {
		return
	}

	_, host, ok := strings.CutLast(addr, "@")
	if !ok {
		return
	}

	if !cfg.Inbound.Enabled {
		log.Warn("sending.bounce_address is set but the inbound listener is off, so bounce reports have nowhere to arrive",
			"bounce_address", addr)

		return
	}

	d, err := st.Domain.GetVerifiedCovering(ctx, host)
	if err != nil {
		log.Warn("sending.bounce_address: could not check whether its domain is verified", "err", err)

		return
	}

	if d == nil {
		log.Warn("sending.bounce_address is on a domain no project has verified, so the inbound listener will refuse its reports at RCPT",
			"bounce_address", addr, "domain", host)
	}
}
