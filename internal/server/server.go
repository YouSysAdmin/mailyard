// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package server is the HTTP edge.
// Fiber app + middleware + route registration.
// Domain logic lives in internal/domain/<thing>/.
package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	docsite "github.com/yousysadmin/mailyard/docs"

	"github.com/gofiber/fiber/v3"
	slogfiber "github.com/samber/slog-fiber"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/response"
)

// Server is the HTTP listener and the routes mounted on it. New builds
// it, Start binds, Shutdown drains.
type Server struct {
	app    *fiber.App
	rt     *env.Runtime
	tlsCfg *tls.Config // nil = serve plain HTTP
}

// Options is what New needs that the Runtime does not already carry.
type Options struct {
	Runtime *env.Runtime

	// TLS is the already-materialized server certificate config, nil
	// to serve plain HTTP. serve.go builds it (through the shared
	// tlsbuild.Builder) so the HTTP server and the SMTP listeners can
	// share one certificate manager.
	TLS *tls.Config

	// HealthOnly registers the probes and the metrics scrape and
	// nothing else. Set by the worker subcommand, where the console
	// and the API have no reason to be reachable but an orchestrator
	// still needs somewhere to point a liveness check.
	HealthOnly bool
}

// baseBodyLimit is the floor for the request body across the API.
// 1 MiB is generous headroom for any operator-driven JSON CRUD.
const baseBodyLimit = 1 * 1024 * 1024

// bodyLimitFor sizes the request body cap so a send carrying the
// configured attachment payload can actually reach the handler.
//
// Attachments travel as base64 inside the JSON body, which inflates
// them by 4/3, so a limit equal to sending.max_total_attachment_size
// would reject a payload of exactly that size. Without the headroom
// the size checks in smtpclient.ValidateAttachments are unreachable -
// fasthttp answers 413 before any handler runs, and the configured
// limit is a number nothing enforces.
//
// The trade is deliberate: this raises the ceiling for every endpoint,
// not just the send routes, because the limit is a property of the
// fasthttp server and Fiber has no per-route override - checked again
// on v3, where BodyLimit still reaches only app.server. Operators who
// do not send large attachments should lower
// sending.max_total_attachment_size, which lowers this with it.
func bodyLimitFor(cfg *env.Config) int {
	total := cfg.Sending.MaxTotalAttachmentSize

	// A relay node forwards a whole received message as base64 in a
	// JSON body, so inbound.max_message_size lands here too. Left out,
	// an operator raising that above the attachment ceiling gets a 413
	// from fasthttp before any handler runs - and a 413 is not in
	// ControlError.Fatal(), so the node would retry a message that can
	// never fit, forever.
	if cfg.RelayNodes.Enabled && cfg.Inbound.MaxMessageSize > total {
		total = cfg.Inbound.MaxMessageSize
	}

	if total <= 0 {
		return baseBodyLimit
	}

	// maxBodyLimit keeps a mistyped config from asking fasthttp to
	// buffer an absurd body. Config.Validate rejects anything larger,
	// so reaching this clamp means validation was bypassed.
	const maxBodyLimit = 512 * 1024 * 1024
	limit := total*4/3 + baseBodyLimit
	if limit < baseBodyLimit {
		return baseBodyLimit
	}

	if limit > maxBodyLimit {
		return maxBodyLimit
	}

	return int(limit)
}

// New builds the Fiber app around an already-resolved TLS config.
// The caller materializes *tls.Config (rather than Start doing it) so
// a bad cert path / missing ACME hosts surfaces synchronously during
// boot, and so every listener in the process can share one.
func New(opts Options) (*Server, error) {
	fiberCfg := fiber.Config{
		BodyLimit: bodyLimitFor(opts.Runtime.Config),

		// Generous rather than tight on purpose. These are a backstop
		// against a socket going nowhere, not a latency policy, and a
		// reverse proxy in front will usually be stricter.
		ReadTimeout:  2 * time.Minute,
		WriteTimeout: 2 * time.Minute,
		IdleTimeout:  75 * time.Second,
	}

	// Fiber treats the TCP peer's address as c.IP() by default.
	// When operators terminate TLS at a reverse proxy / ALB and only
	// trusted hosts ever connect to us, we honor X-Forwarded-For but
	// strictly limit it to the trusted-proxy list so an unproxied
	// caller cannot spoof their source IP through the header.
	if proxies := opts.Runtime.Config.Server.TrustedProxies; len(proxies) > 0 {
		fiberCfg.TrustProxy = true
		fiberCfg.TrustProxyConfig = fiber.TrustProxyConfig{Proxies: proxies}
		fiberCfg.ProxyHeader = fiber.HeaderXForwardedFor
	}

	app := fiber.New(fiberCfg)
	app.Use(safeRecover)

	// script-src is built once, from the hashes of whatever inline scripts
	// the embedded documentation actually contains.
	app.Use(securityHeaders(scriptSrcFor(docsite.FS())))
	app.Use(skipPaths(
		slogfiber.NewWithFilters(opts.Runtime.Log, accessLogFilters()...),
		streamingPaths...,
	))
	app.Use(requestContext(opts.Runtime))

	registerRoutes(app, opts.Runtime, opts.HealthOnly)

	return &Server{app: app, rt: opts.Runtime, tlsCfg: opts.TLS}, nil
}

// Start blocks serving HTTP or HTTPS depending on whether TLS was configured.
// Returns Fiber's listener error verbatim so serve.go's
// errCh path keeps working unchanged.
func (s *Server) Start() error {
	addr := s.rt.Config.Server.Addr
	// v3 asks about listening at Listen time rather than at New time.
	// One value for both paths below: the startup banner is noise in a
	// service that logs its own start line.
	listenCfg := fiber.ListenConfig{DisableStartupMessage: true}

	if s.tlsCfg != nil {
		ln, err := tls.Listen("tcp", addr, s.tlsCfg)
		if err != nil {
			return fmt.Errorf("tls listen %s: %w", addr, err)
		}

		// No mode to name anymore. Which certificate this serves is
		// resolved per handshake - assigned, then acme, then the
		// self-signed pair - so a word logged here would be a guess that
		// stops being true the moment an admin assigns one.
		slog.Info("server start", "addr", addr, "tls", true)

		return s.app.Listener(ln, listenCfg)
	}

	slog.Info("server start", "addr", addr, "tls", false)

	return s.app.Listen(addr, listenCfg)
}

// Shutdown stops accepting connections and waits for in-flight
// requests, but only up to timeout.
//
// The bound is not optional. fasthttp treats a streamed response as an
// active connection, so any endpoint that holds one open (the SSE
// feed) makes an unbounded app.Shutdown wait for the CLIENT to hang
// up. Callers close the event bus first, which ends those streams
// cooperatively - this is the backstop for anything that does not.
func (s *Server) Shutdown(timeout time.Duration) error {
	err := s.app.ShutdownWithTimeout(timeout)
	if errors.Is(err, context.DeadlineExceeded) {
		// Not fatal: the listener is closed and the process is exiting
		// anyway. Worth saying out loud, because a handler that will
		// not finish is a bug somewhere.
		slog.Warn("server: shutdown timed out with requests still in flight", "timeout", timeout)

		return nil
	}

	return err
}

// safeRecover catches panics inside any downstream handler, logs the
// reason + stack trace server-side, and returns a generic 500 to the
// caller. Fiber's stock recover.New writes the panic value into the
// response body, which leaks internal state (paths, types, sometimes
// secrets in *fmt.wrapError values) to whoever triggered the panic.
func safeRecover(c fiber.Ctx) (err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic recovered",
				"reason", fmt.Sprintf("%v", r),
				"path", c.Path(),
				"method", c.Method(),
				"client_ip", c.IP(),
				"stack", string(debug.Stack()),
			)
			err = response.Internal(c, nil)
		}
	}()

	return c.Next()
}

// securityHeaders sets baseline browser-side defenses on every
// response. The headers are cheap, idempotent and path-independent, so
// a bare 401 from an unprotected handler still gets them.
//
// script-src carries no 'unsafe-inline'. The embedded documentation
// writes one inline script - the colour-mode probe, which must run
// before paint - so the policy names its sha256 instead, computed at
// boot from the bytes the binary ships (inlineScriptHashes). A constant
// here would be refused by the browser after the next docs build, and
// the only symptom would be pages rendering without their theme. One
// hash covers the site: the script is byte-identical on every page.
//
// A hash makes a browser ignore 'unsafe-inline' entirely, and inline
// event handlers, which a hash cannot cover, appear in neither build.
//
// style-src KEEPS 'unsafe-inline' and cannot lose it: Vue writes element
// styles for every :style binding, and there is no hash or nonce for a
// style attribute. Rendering goes through Vue's escaping.
//
// Fonts are bundled and served from this origin. 'unsafe-eval' is off,
// so bundling that needs it has to opt in deliberately.
func securityHeaders(scriptSrc string) fiber.Handler {
	return func(c fiber.Ctx) error {
		// HSTS, but only on a connection that really arrived over TLS.
		// Without it a browser has nothing telling it to refuse the
		// plain-HTTP version of the site, and the first request of a
		// session is exactly the one that can be intercepted. The cookie's
		// Secure flag is no help there, because the attacker is the one
		// answering.
		//
		// We check c.Scheme(), which honours the trusted-proxy list, so
		// the usual deployment (TLS at a proxy, plain HTTP upstream) sets
		// the header and a local dev instance does not. Browsers ignore
		// HSTS over HTTP anyway, so setting it always would be harmless
		// but misleading to read.
		//
		// NOT c.Protocol(), which is what this read was called in Fiber
		// v2. In v3 that name answers the HTTP version - "HTTP/1.1" -
		// and compares equal to nothing here, so the header would simply
		// never be sent and no build or test would say so.
		//
		// No preload and no includeSubDomains. Both are promises about
		// hostnames this process does not own, and preload is hard to
		// undo.
		if c.Scheme() == "https" {
			c.Set("Strict-Transport-Security", "max-age=31536000")
		}

		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("X-Frame-Options", "DENY")
		c.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Set("Cross-Origin-Opener-Policy", "same-origin")
		c.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		c.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src "+scriptSrc+"; "+
				"style-src 'self' 'unsafe-inline'; "+

				// Images from anywhere. This is the one directive here that
				// is not as tight as it could be, and we mean it.
				//
				// An email's images are the email: a logo on a CDN, a hero
				// image in somebody's marketing bucket, a signature graphic
				// on a host we have never heard of. A preview that renders
				// none of them is not a preview, and the console shows
				// bodies on seven pages, including template and campaign
				// previews built on the fly that have no route of their own
				// to be served from.
				//
				// `*` covers neither data: nor blob:, so both are listed.
				// Emails embed logos as data URIs constantly and dropping
				// them would break the case this exists for.
				//
				// The cost is real and worth writing down: an injected
				// script could beacon data out through an image URL, where
				// `default-src 'self'` would otherwise keep it on our
				// origin.
				//
				// It costs nothing in privacy, though. HtmlPreview.vue
				// rewrites every remote src to a data-attribute before it
				// renders, so images stay off until the reader asks for
				// them, the same way a mail client behaves and for the same
				// reason - fetching a remote image tells its host that the
				// message was opened.
				"img-src * data: blob:; "+
				"font-src 'self'; "+
				"connect-src 'self'; "+
				"frame-ancestors 'none'; "+
				"base-uri 'self'; "+
				"form-action 'self'")

		return c.Next()
	}
}

// accessLogFilters returns the slog-fiber filters applied to every
// request. Each filter returns false to drop the entry, true to keep.
//
// The /auth/me probe is the SPA's "is the user logged in?" probe fired on
// every page load. A 401 there is the expected answer when there's
// no session cookie, not a security event. slog-fiber's default
// behavior promotes any 4xx to WARN, which floods the log on every
// unauthenticated visit to the login page. Drop just that exact
// (path, status) pair so login 401s and 401s on protected endpoints
// still log - those signal credential stuffing or someone probing
// the API.
// streamingPaths are endpoints whose response body never ends.
//
// The access logger records the response size, which it gets from
// c.Response().Body() - and on a streamed body that call drains the
// stream, so on an endless one it blocks forever. The request then
// hangs with no headers ever sent. slog-fiber's own filters do not
// help: they are evaluated after the body has already been read.
var streamingPaths = []string{env.ConsolePath + "/api/events/stream"}

// skipPaths runs h for every request except the listed paths, which
// are passed straight through.
func skipPaths(h fiber.Handler, paths ...string) fiber.Handler {
	skip := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		skip[p] = struct{}{}
	}

	return func(c fiber.Ctx) error {
		if _, ok := skip[c.Path()]; ok {
			return c.Next()
		}

		return h(c)
	}
}

func accessLogFilters() []slogfiber.Filter {
	return []slogfiber.Filter{
		func(c fiber.Ctx) bool {
			return c.Path() != env.ConsolePath+"/api/auth/me" ||
				c.Response().StatusCode() != http.StatusUnauthorized
		},
	}
}
