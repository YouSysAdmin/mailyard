// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/mail"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/emersion/go-smtp"
	"github.com/spf13/cobra"
	"github.com/yousysadmin/mailyard/internal/server"

	"github.com/yousysadmin/mailyard/internal/core/alertmail"
	coreaudit "github.com/yousysadmin/mailyard/internal/core/audit"
	"github.com/yousysadmin/mailyard/internal/core/authenticator"
	"github.com/yousysadmin/mailyard/internal/core/blob"
	"github.com/yousysadmin/mailyard/internal/core/certexpiry"
	"github.com/yousysadmin/mailyard/internal/core/cron"
	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/dispatch"
	"github.com/yousysadmin/mailyard/internal/core/emailverify"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/eventbus"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/core/iplimit"
	"github.com/yousysadmin/mailyard/internal/core/metrics"
	"github.com/yousysadmin/mailyard/internal/core/notify"
	coreoidc "github.com/yousysadmin/mailyard/internal/core/oidc"
	"github.com/yousysadmin/mailyard/internal/core/partition"
	"github.com/yousysadmin/mailyard/internal/core/partitionalert"
	"github.com/yousysadmin/mailyard/internal/core/proxylisten"
	"github.com/yousysadmin/mailyard/internal/core/queue"
	"github.com/yousysadmin/mailyard/internal/core/retention"
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
	"github.com/yousysadmin/mailyard/internal/domain/inbound"
	"github.com/yousysadmin/mailyard/internal/domain/sandbox"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	"github.com/yousysadmin/mailyard/internal/domain/submission"
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

// The api and worker commands live in roles_ee.go. The role STRUCT and
// every branch on it stay here, in both editions: a community node is
// always role{api: true, worker: true}, and forking this bootstrap to
// delete branches that are simply always taken would be a second copy
// of eight hundred lines to keep in step.
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
	oauthRegistry := coreoidc.NewRegistry(cfg.Server.PublicURL)
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

	// Platform mail (invitations, password resets, signup
	// confirmations). Always present, Enabled() false until an admin
	// sets a from address - every call site degrades to copyable
	// links.
	//
	// It leaves through the SHARED SMTP POOL rather than a second
	// server in the config file. One place to configure platform
	// credentials, and a pool row marked platform_only reserves itself
	// for this traffic.
	//
	// The relay identity is resolved FIRST, because the pool row this
	// picks can be a relay node, and a node is dialled with our client
	// certificate and no AUTH - so the sender needs the same transport
	// builder the delivery worker and the console's Test button use.
	relayClient := relayClientSource(cfg, st)
	if relayClient != nil {
		rt.RelayNodeTLS = relayClient.WorkerTLS
	}

	rt.SystemMail = systemmail.New(st.SharedSMTP, func() systemmail.Address {
		return systemmail.Address{
			From:     rt.Settings.String(smodel.KeyPlatformMailFrom),
			FromName: rt.Settings.String(smodel.KeyPlatformMailFromName),
		}
	}, log, rt.RelayNodeTLS)
	if rt.SystemMail.Enabled() {
		log.Info("platform mail enabled", "from", rt.SystemMail.From())
	} else {
		log.Warn("platform mail disabled, invitations return copyable links and password reset is unavailable - set platform_mail_from")
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
	dispatcher := dispatch.New(st.Webhook, dispatch.Config{
		Timeout:             cfg.Webhook.Timeout,
		MaxAttempts:         cfg.Webhook.MaxAttempts,
		RetryDelay:          cfg.Webhook.RetryDelay,
		AllowPrivateTargets: cfg.Webhook.AllowPrivateTargets,
	}, log)
	rt.Dispatch = dispatcher

	// Delivery worker: the email store is its queue source, the email
	// processor its delivery leg. Started before the HTTP server so
	// rows left queued by a previous run drain immediately. relayClient
	// was resolved beside SystemMail above - one identity for the three
	// places a node is dialled from: the worker, the console's Test
	// button (see smtpserver.testTransport) and platform mail.
	worker := queue.NewWorker(st.Email, &email.Processor{
		Store:         st,
		Log:           log,
		AutoSuppress:  cfg.Sending.AutoSuppressOnReject,
		Blob:          rt.Blob,
		BounceAddress: strings.TrimSpace(cfg.Sending.BounceAddress),
		RelayClient:   relayClient,
	}, queue.Config{
		Concurrency:    cfg.Worker.Concurrency,
		PollInterval:   cfg.Worker.PollInterval,
		MaxAttempts:    cfg.Worker.MaxAttempts,
		RetryBaseDelay: cfg.Worker.RetryBaseDelay,
		RetryMaxDelay:  cfg.Worker.RetryMaxDelay,
		ClaimTimeout:   cfg.Worker.ClaimTimeout,
	}, log)
	worker.OnFinal = func(job *emailmodel.Email, status, errMsg string) {
		metrics.EmailsFinalized.WithLabelValues(status).Inc()

		// Sync the campaign message (no-op for transactional sends).
		msgStatus := map[string]string{
			emailmodel.StatusSent:       campaignmodel.MsgSent,
			emailmodel.StatusFailed:     campaignmodel.MsgFailed,
			emailmodel.StatusSuppressed: campaignmodel.MsgSkipped,
		}[status]
		if msgStatus != "" {
			if err := st.Campaign.MarkMessageByEmail(context.Background(), job.ID, msgStatus, errMsg); err != nil {
				log.Error("campaign: sync message status", "email_id", job.ID, "err", err)
			}
		}

		// Fold the outcome into the per-recipient contact tallies.
		// Only terminal sent/failed count: a suppressed message was
		// never attempted, so recording it as a failure would blame
		// the address for a decision we made about it.
		if status == emailmodel.StatusSent || status == emailmodel.StatusFailed {
			trackContacts(context.Background(), st, log, job, status == emailmodel.StatusSent)
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
	rt.Queue = worker
	workerCtx, stopWorker := context.WithCancel(context.Background())
	defer stopWorker()
	// The worker is CONSTRUCTED on every role, and only STARTED on a
	// worker one. An api node still hands rt.Queue to the email
	// service, whose Wake now broadcasts - so accepting a send on an
	// api node is what tells the worker nodes to look. Leaving
	// rt.Queue nil there would drop that signal and put every send
	// back on the poll interval.
	if r.worker {
		// safego.Go on the long-lived loops: each already guards its
		// own unit of work, this catches a panic in the loop
		// machinery itself rather than letting it end the process.
		safego.Go(log, "queue: worker loop", func() { worker.Start(workerCtx) })
	}

	// Tracking signer: mints the public /t/ URLs. Needs the public
	// base URL to build absolute links - without it campaigns send
	// untracked.
	rt.Tracking = coretracking.NewSigner(cfg.Server.PublicURL, crypto.DeriveKey(cfg.Auth.JWTSecret, crypto.KeyTracking))
	if !rt.Tracking.Enabled() {
		log.Warn("tracking: disabled, set server.public_url (and auth.jwt_secret) to enable open/click tracking and hosted unsubscribe pages")
	}

	// Campaign runner: drains sending campaigns into the email queue
	// in throttled batches. Built after the queue worker so its email
	// service can wake it.
	runner := campaign.NewRunner(st, email.NewService(rt), log,
		dispatcher.Emit, rt.Tracking, cfg.Campaign.BatchSize, cfg.Campaign.PollInterval)
	rt.CampaignWake = runner.Wake
	if r.worker {
		safego.Go(log, "campaign: runner loop", func() { runner.Start(workerCtx) })
	}

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
		// it is platform settings now - an administrator turns ACME on
		// and names a host without restarting anything.
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
	// concurrently on several nodes - they are all delete-by-age
	// sweeps - so no leader election is needed.
	rt.Cron = cron.New(log)

	// Alerts: an in-app notification that means something is WRONG also
	// goes out as mail. The raiser calls it rather than alertmail
	// watching the event bus, because a notification is not an audit
	// event - nobody made a request, a job noticed something.
	rt.Notify = &notify.Raiser{Store: st, Bus: rt.Events, Log: log, Alerts: rt.Alerts}

	// settings-refresh is registered on every role, unlike the sweeps
	// below. It is not maintenance - it is how a node's settings
	// cache learns about a change written on another node, so an api
	// node that skipped it would serve stale settings forever.
	rt.Cron.Register(cron.Job{
		Name:     "settings-refresh",
		Schedule: cron.EveryInterval(5 * time.Minute),
		Run:      rt.Settings.Reload,
	})

	// A certificate is the one thing here that breaks by doing
	// nothing, and a listener holding an expired one starts perfectly
	// - only the handshake fails. Worker role, like the other sweeps.
	if r.worker {
		expiry := &certexpiry.Checker{
			Store: st.Certificate,
			Mail:  rt.SystemMail,
			Admins: func(ctx context.Context) ([]string, error) {
				users, err := st.User.List(ctx)
				if err != nil {
					return nil, err
				}

				var to []string
				for _, u := range users {
					if u.IsAdmin() && !u.Disabled && u.Email != "" {
						to = append(to, u.Email)
					}
				}

				return to, nil
			},
			Log: log,
		}
		rt.Cron.Register(cron.Job{
			Name: "certificate-expiry",

			// Six-hourly rather than daily: a certificate that enters
			// the window at noon should not first be reported the
			// following morning. The mail itself is capped at one a
			// day inside the checker.
			Schedule: cron.EveryInterval(6 * time.Hour),
			Run:      expiry.Run,
		})
	}

	if r.worker {
		// Partition maintenance runs before the retention job in the
		// day, and far more often than it strictly needs to. Creating
		// a partition that already exists costs one catalog lookup,
		// while failing to create one costs every INSERT for that week
		// - so the cheap direction is to try hourly and let almost
		// every run be a no-op.
		parts := &partition.Maintainer{DB: db.DB(), Log: log}
		rt.Cron.Register(cron.Job{
			Name:     "partition-maintenance",
			Schedule: cron.EveryInterval(time.Hour),
			Run: func(ctx context.Context) error {
				_, err := parts.EnsureAhead(ctx)

				return err
			},
		})

		// Run it once at boot too. A node starting after a long outage
		// must not wait an hour to find out it has nowhere to write.
		if _, err := parts.EnsureAhead(cmd.Context()); err != nil {
			log.Error("partition maintenance failed at startup", "err", err)
		}

		sweeper := &retention.Sweeper{
			Store: st, Settings: rt.Settings, Blob: rt.Blob, Log: log,
			Partitions: parts,
		}
		rt.Cron.Register(cron.Job{
			Name:     "retention-cleanup",
			Schedule: cron.DailyAt(3, 0),
			Run:      sweeper.Run,
		})

		// The partition ceiling, which only an installation keeping
		// everything forever can reach - and which it reaches without
		// anything going wrong until delivery stops. Daily rather than
		// hourly: the count moves by one a day at most, and the mail
		// collapses to one a day anyway.
		//
		// PlatformAdmins rather than a list built here: who hears about
		// an installation-wide condition is already answered once, in
		// the store the alert path uses.
		rt.Cron.Register(cron.Job{
			Name:     "partition-ceiling",
			Schedule: cron.DailyAt(3, 30),
			Run: (&partitionalert.Checker{
				Counter: parts,
				Mail:    rt.SystemMail,
				Admins:  st.AlertRecipients.PlatformAdmins,
				Log:     log,
			}).Run,
		})

		// The trend rollup. The window is the widest range the chart
		// offers plus a day, so a bar is at most one run stale.
		// Worker-only like the other sweeps - every node running it
		// would be the same scan several times for one answer.
		rt.Cron.Register(cron.Job{
			Name:     "email-stats",
			Schedule: cron.EveryInterval(10 * time.Minute),
			Run: func(ctx context.Context) error {
				return st.Analytics.RecomputeDaily(ctx, statsRollupDays)
			},
		})

		// Bounce rate is a property of a window, so it is judged on a
		// timer rather than from the delivery path - see the package
		// comment. Fifteen minutes is frequent enough to catch a bad
		// campaign early and rare enough that the sweep is free.
		bounceAlerter := &notify.BounceAlerter{
			Store: st, Settings: rt.Settings, Raiser: rt.Notify, Log: log,
		}
		rt.Cron.Register(cron.Job{
			Name:     "bounce-alert",
			Schedule: cron.EveryInterval(15 * time.Minute),
			Run:      bounceAlerter.Run,
		})
	}

	safego.Go(log, "cron: scheduler loop", func() { rt.Cron.Start(workerCtx) })

	// serveSMTP binds the port before returning and only then hands the listener to a goroutine.
	//
	// Binding inside the goroutine surfaced a refused bind as one log
	// line while boot carried on reporting success - and the defaults
	// are privileged ports, so "permission denied" is ordinary. A port
	// we cannot take is a failure to start, not a warning.
	serveSMTP := func(srv *smtp.Server, kind, addr string, secure bool, proxy env.ProxyProtocolConfig) error {
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

		log.Info(kind+" listening", "addr", bound, "starttls", secure, "proxy_protocol", proxy.Enabled)
		safego.Go(log, kind+": accept loop", func() {
			if serr := srv.Serve(ln); serr != nil && !errors.Is(serr, smtp.ErrServerClosed) {
				log.Error(kind+" stopped", "err", serr)
			}
		})

		return nil
	}

	// SMTP submission: optional listener that feeds the same send
	// pipeline. Authenticated by submission credentials or API keys,
	// so it needs nothing beyond the store and the email service.
	// The SMTP listeners are ingress, so they follow the api role
	// rather than the worker one: they accept messages and queue
	// them, exactly like POST /api/v1/emails/send does.
	var submissionSrv *smtp.Server
	if r.api && cfg.Submission.Enabled {
		backend := &submission.Backend{
			Credentials:    st.SMTPCredential,
			Keys:           st.APIKey,
			Sender:         email.NewService(rt),
			Sandbox:        &sandbox.Service{Store: st.Sandbox, Settings: rt.Settings, Log: log, All: st},
			Log:            log,
			MaxMessageSize: cfg.Submission.MaxMessageSize,
			Limiter:        iplimit.New(cfg.Submission.RatePerMinute, time.Minute),
		}
		submissionTLS, terr := tlsBuilder.Build(certificate.ListenerSubmission, cfg.Submission.TLS.Enabled)
		if terr != nil {
			return fmt.Errorf("submission tls: %w", terr)
		}

		submissionSrv = submission.NewServer(backend, cfg.Submission.Addr, cfg.Submission.Hostname, submissionTLS)
		if lerr := serveSMTP(submissionSrv, "smtp submission", cfg.Submission.Addr, submissionTLS != nil,
			cfg.Submission.ProxyProtocol); lerr != nil {
			return lerr
		}
	}

	// Inbound MX listener: receives mail for verified domains and
	// stores it per project, emitting inbound.received webhooks.
	var inboundSrv *smtp.Server
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
		inboundTLS, terr := tlsBuilder.Build(certificate.ListenerInbound, cfg.Inbound.TLS.Enabled)
		if terr != nil {
			return fmt.Errorf("inbound tls: %w", terr)
		}

		inboundSrv = inbound.NewServer(backend, cfg.Inbound.Addr, cfg.Inbound.Hostname, inboundTLS)
		if lerr := serveSMTP(inboundSrv, "inbound smtp", cfg.Inbound.Addr, inboundTLS != nil,
			cfg.Inbound.ProxyProtocol); lerr != nil {
			return lerr
		}
	}

	stopSMTP := func(srv *smtp.Server, timeout time.Duration) {
		if srv == nil {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		if serr := srv.Shutdown(ctx); serr != nil {
			_ = srv.Close()
		}
	}
	stopSMTPListeners := func(timeout time.Duration) {
		stopSMTP(submissionSrv, timeout)
		stopSMTP(inboundSrv, timeout)
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

	// A worker still binds server.addr, but serves only the probes and
	// the metrics scrape. Not serving anything was the alternative,
	// and it makes a worker unschedulable: an orchestrator needs a
	// liveness endpoint, and the delivery node is the one whose
	// metrics an operator most wants. Running the full console there
	// would instead put the whole authenticated surface on a machine
	// that has no reason to expose it.
	srv, err := server.New(server.Options{Runtime: rt, TLS: serverTLS, HealthOnly: !r.api})
	if err != nil {
		return fmt.Errorf("server init: %w", err)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	select {
	case s := <-sigCh:
		log.Info("shutdown requested", "signal", s.String())

		// Order matters: listeners, then the campaign runner, then the
		// queue worker, then the dispatcher - all before the HTTP
		// server and the deferred db.Close.
		//
		// stopWorker runs here and not only in the defer, which cannot
		// fire until this function returns - and it is about to block
		// on shutdown, so scheduled jobs kept firing for the whole
		// drain.
		stopWorker()
		stopSMTPListeners(10 * time.Second)
		runner.Stop(10 * time.Second)
		worker.Stop(30 * time.Second)
		dispatcher.Close(10 * time.Second)
		rt.Audit.Close(5 * time.Second)

		// Ends every open SSE stream. Without this the streams keep
		// their connections active and the server wait below runs to
		// its full timeout on every shutdown that had a console tab
		// attached.
		rt.Events.Close()

		return srv.Shutdown(shutdownTimeout)
	case err := <-errCh:
		stopWorker()
		stopSMTPListeners(5 * time.Second)
		runner.Stop(5 * time.Second)
		worker.Stop(5 * time.Second)
		dispatcher.Close(5 * time.Second)
		rt.Audit.Close(5 * time.Second)

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
		Admin:         true,
		EmailVerified: true,
	}
	if err := rt.Store.User.Put(ctx, u); err != nil {
		return fmt.Errorf("put user: %w", err)
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

// trackContacts records one terminal delivery outcome against each
// recipient's contact row.
//
// Best effort: a contact tally is reporting, not delivery. A failure is
// logged and dropped rather than retried - the message has already been
// delivered or permanently failed, so there is nothing to roll back.
//
// Runs inline in the worker's finalize hook: one upsert per recipient
// per message, the same order of writes a campaign already does.
func trackContacts(ctx context.Context, st *store.Store, log *slog.Logger, job *emailmodel.Email, sent bool) {
	now := time.Now().UTC()
	for _, raw := range job.Recipients {
		addr, name := splitRecipient(raw)
		if addr == "" {
			continue
		}

		if err := st.Contact.RecordOutcome(ctx, job.ProjectID, addr, name, sent, now); err != nil {
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
