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
	"strings"
	"time"

	docsite "github.com/yousysadmin/mailyard/docs"

	"github.com/gofiber/fiber/v3"
	slogfiber "github.com/samber/slog-fiber"
	"github.com/valyala/fasthttp"

	"github.com/yousysadmin/mailyard/internal/core/clientip"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/domain/eventstream"
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
// This is the SERVER's limit, and only the routes in LargeBodyPaths
// get it. Fiber has no per-route override - checked again on v3, where
// BodyLimit still reaches only app.server - but fasthttp's
// HeaderReceived hook can lower it per request, and perRequestLimits
// does, because a body is buffered in full BEFORE the handler chain
// runs: neither auth nor a rate limiter gets to refuse it first. Left
// as one ceiling, a stranger could hold Concurrency times this much
// memory by posting to the login route. Operators who do not send
// large attachments should still lower sending.max_total_attachment_size,
// which lowers this with it.
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
		//
		// WriteTimeout is not the whole story for a STREAMED response -
		// see streamWriteTimeout below, which is why the event feed does
		// not die every two minutes.
		ReadTimeout:  2 * time.Minute,
		WriteTimeout: 2 * time.Minute,
		IdleTimeout:  75 * time.Second,

		// A request body is read into memory BEFORE the handler chain
		// runs, so neither auth nor the rate limiter can refuse one
		// first. This is the only bound on how many of those exist at
		// once - see env.ServerConfig.MaxConcurrentRequests.
		Concurrency: concurrencyFor(opts.Runtime.Config),

		// 8 KiB rather than fasthttp's 4 KiB. Our own session cookie is
		// a 369-byte JWT (measured, on an account with one project),
		// and it arrives alongside whatever else the browser has for
		// the origin plus the headers each proxy hop adds. Past the
		// buffer fasthttp answers 431 before any handler runs, which
		// reads as the site being broken for one person and fine for
		// everybody else.
		ReadBufferSize: 8 * 1024,

		// Every error a handler returns, and Fiber's own 404, answered
		// in the envelope three generated SDKs expect. Without it the
		// stock handler writes err.Error() as text/plain, so a store
		// failure reaching the top of a handler became a 500 carrying
		// the Postgres message.
		ErrorHandler: errorHandler,

		// The wire policy, stated once in the response package: every
		// c.JSON in the tree marshals under it (json/v2 - a nil slice
		// is [], a nil map stays null, a bad byte from received mail is
		// coerced instead of failing the response), and request parsing
		// through the app decoder is v2-strict. Left unset, Fiber falls
		// back to v1 semantics and empty lists quietly go back to null
		// - which the generated Python and Ruby clients do not survive.
		JSONEncoder: func(v any) ([]byte, error) { return response.Marshal(v) },
		JSONDecoder: func(data []byte, v any) error { return response.Unmarshal(data, v) },
	}

	// TrustProxy governs what Fiber itself will read out of a forwarding
	// header: c.Scheme() (which the HSTS check in securityHeaders rests
	// on) and c.Host(). Behind a proxy those come from X-Forwarded-Proto and
	// X-Forwarded-Host, and only from a peer on this list.
	//
	// ProxyHeader is deliberately NOT set, so c.IP() stays the TCP peer
	// and nothing else. Fiber reads X-Forwarded-For leftmost-first,
	// which is the half of the header the CALLER writes - the address of
	// the caller is resolved in internal/core/clientip instead, by
	// walking the header from the right.
	if proxies := opts.Runtime.Config.Server.TrustedProxies; len(proxies) > 0 {
		fiberCfg.TrustProxy = true
		fiberCfg.TrustProxyConfig = fiber.TrustProxyConfig{Proxies: proxies}
	}

	// Parsed once, at boot: an unreadable entry is a trust list that is
	// not what the operator wrote, and finding that out per request
	// means finding it out never.
	resolver, err := clientip.New(opts.Runtime.Config.Server.TrustedProxies)
	if err != nil {
		return nil, fmt.Errorf("server.trusted_proxies: %w", err)
	}

	app := fiber.New(fiberCfg)
	app.Server().HeaderReceived = perRequestLimits

	app.Use(safeRecover)

	// script-src is built once, from the hashes of whatever inline scripts
	// the embedded documentation actually contains.
	app.Use(securityHeaders(scriptSrcFor(docsite.FS())))
	app.Use(skipPaths(
		// redactURLs first: the paths whose URL is itself a credential
		// are logged by route pattern and never reach slog-fiber, which
		// records the raw path, the query and every route param.
		redactURLs(opts.Runtime.Log,
			slogfiber.NewWithFilters(opts.Runtime.Log, accessLogFilters()...)),
		streamingPaths...,
	))
	app.Use(requestContext(opts.Runtime, resolver))

	registerRoutes(app, opts.Runtime, opts.HealthOnly)

	return &Server{app: app, rt: opts.Runtime, tlsCfg: opts.TLS}, nil
}

// concurrencyFor turns the operator's cap into what fasthttp wants,
// where 0 means "use the library default" in both places.
func concurrencyFor(cfg *env.Config) int {
	if cfg.Server.MaxConcurrentRequests <= 0 {
		return 0
	}

	return cfg.Server.MaxConcurrentRequests
}

// streamWriteTimeout is the write deadline for the endpoints that stream.
//
// fasthttp sets the write deadline ONCE, after the handler returns and
// before it writes the response, and a streamed body is written under
// that one deadline - so the app-wide WriteTimeout is not a per-write
// budget for a stream, it is the stream's whole lifetime. Measured: at
// WriteTimeout 2s a stream writing every 500ms was cut at 2.0s exactly.
//
// The feed recycles itself at eventstream.MaxStreamLife, so this only has
// to sit above that. Derived from it rather than written down, because
// two numbers that must stay ordered are one number and a margin.
const streamWriteTimeout = eventstream.MaxStreamLife + 5*time.Minute

// apiBodyLimit is the request body cap on the authenticated API
// surfaces, /api/v1 and /app/api, for every route that does not carry
// attachments. Sized from what the biggest ordinary body actually is:
// a template version is two 1 MiB fields plus sample data, a
// subscriber import is 10 000 rows. Well under the attachment ceiling,
// which is what a stranger otherwise gets to make us buffer.
const apiBodyLimit = 8 * 1024 * 1024

// LargeBodyPaths are the routes whose body may legitimately be as big
// as the server's limit (bodyLimitFor): attachments travel inside the
// JSON, base64 encoded, and a relay node forwards a whole received
// message the same way. A `*` stands for one path segment, an id.
// Everything else gets apiBodyLimit or, off the API, baseBodyLimit.
//
// Exported for the guard in tests/ that asks the router whether each
// entry still names a route - an entry nobody matches is a route that
// has been renamed out from under its ceiling and now answers 413.
var LargeBodyPaths = []string{
	"/api/v1/emails/send",
	"/api/v1/emails/send-template",
	"/api/v1/emails/batch",
	"/api/v1/templates/*/attachments",
	// A template export bundle carries every version, each up to a
	// template's own size.
	"/api/v1/templates/import",
	// Enterprise only, but a path is a string and the community binary
	// has no route here, so the entry costs it nothing.
	"/api/relay-nodes/inbound",
}

// perRequestLimits raises the write deadline for the streaming endpoints
// and lowers the body ceiling for every route that does not carry
// attachments.
//
// A zero field in the returned config means "honor the server's own
// value", so the read timeout is untouched everywhere and the body
// limit only where LargeBodyPaths says so. The URI here is the RAW
// request target, so the path is normalized the same way the router
// normalizes it before matching - see normalizePath.
func perRequestLimits(h *fasthttp.RequestHeader) fasthttp.RequestConfig {
	uri := string(h.RequestURI())
	if i := strings.IndexAny(uri, "?#"); i >= 0 {
		uri = uri[:i]
	}

	cfg := fasthttp.RequestConfig{MaxRequestBodySize: bodyLimitForPath(uri)}
	if isStreamingPath(uri) {
		cfg.WriteTimeout = streamWriteTimeout
	}

	return cfg
}

// bodyLimitForPath is the body ceiling one request gets, where 0 means
// the server's own. Three tiers: the attachment routes take the
// server's, the two API prefixes take apiBodyLimit, and everything
// else - login, the SES webhook, tracking, health, the console shell -
// takes baseBodyLimit. The auth routes are carved out of the API tier
// because they are the ones a stranger reaches, and nothing there is
// bigger than a form.
func bodyLimitForPath(path string) int {
	p := normalizePath(path)
	for _, pattern := range LargeBodyPaths {
		if pathMatches(pattern, p) {
			return 0
		}
	}

	if strings.HasPrefix(p, env.ConsolePath+"/api/auth/") {
		return baseBodyLimit
	}

	if strings.HasPrefix(p, "/api/v1/") || strings.HasPrefix(p, env.ConsolePath+"/api/") {
		return apiBodyLimit
	}

	return baseBodyLimit
}

// pathMatches compares a normalized path against a LargeBodyPaths
// pattern segment by segment, `*` standing for any one segment.
func pathMatches(pattern, path string) bool {
	want := strings.Split(pattern, "/")
	got := strings.Split(path, "/")
	if len(want) != len(got) {
		return false
	}

	for i := range want {
		if want[i] != "*" && want[i] != got[i] {
			return false
		}
	}

	return true
}

// errorHandler answers every error that reaches the top of the chain,
// including Fiber's own for a path that matched no route.
//
// A *fiber.Error carries the status the framework chose and a message it
// wrote, and both are safe to repeat - nothing in this tree constructs
// one, so the message is never a detail of ours. Anything else is ours and goes
// through response.Internal, which logs it, softens a malformed uuid to
// a 404, and tells the caller nothing it should not know.
func errorHandler(c fiber.Ctx, err error) error {
	if fe, ok := errors.AsType[*fiber.Error](err); ok {
		return response.Coded(c, fe.Code, strings.ToLower(fe.Message))
	}

	return response.Internal(c, err)
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
				"client_ip", clientip.From(c),
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
		// no-referrer, not strict-origin-when-cross-origin: that one
		// still sends the FULL URL on same-origin requests, and three
		// SPA documents carry a credential in theirs - /reset-password,
		// /verify-email and /invitations all take ?token=. The console
		// fires six API calls on page load, each arrived with the
		// reset token in its Referer, and the access logger records
		// the referer field - so the token walked around redactURLs
		// through a side door. Nothing of ours reads Referer (checked,
		// server and console both), Origin is unaffected (CORS and
		// passkeys keep working), and a click-tracking redirect now
		// leaks nothing to the destination site either.
		c.Set("Referrer-Policy", "no-referrer")
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
//
// The comparison is against the NORMALIZED path, because the router is
// neither case sensitive nor strict about a trailing slash: a request for
// /app/api/events/STREAM/ reaches the streaming handler, and a raw string
// compare did not skip the logger for it - which is a request that hangs
// with no headers ever sent, holding a connection until the stream
// recycles itself half an hour later.
func skipPaths(h fiber.Handler, paths ...string) fiber.Handler {
	skip := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		skip[normalizePath(p)] = struct{}{}
	}

	return func(c fiber.Ctx) error {
		if _, ok := skip[normalizePath(c.Path())]; ok {
			return c.Next()
		}

		return h(c)
	}
}

// isStreamingPath reports whether a path is one of the endpoints whose
// response body never ends.
func isStreamingPath(path string) bool {
	target := normalizePath(path)
	for _, p := range streamingPaths {
		if normalizePath(p) == target {
			return true
		}
	}

	return false
}

// normalizePath folds a request path the way the router does before it
// matches: lowercased, since CaseSensitive is off, and without trailing
// slashes, since StrictRouting is off.
//
// Those two are the whole set of variations that reach the same handler -
// proven rather than assumed in TestTheLoggerSkipsEveryFormOfAStreamingPath,
// which asks the router which forms arrive and requires this to agree.
func normalizePath(path string) string {
	trimmed := strings.TrimRight(path, "/")
	if trimmed == "" {
		return "/"
	}

	return strings.ToLower(trimmed)
}

func accessLogFilters() []slogfiber.Filter {
	return []slogfiber.Filter{
		func(c fiber.Ctx) bool {
			return c.Path() != env.ConsolePath+"/api/auth/me" ||
				c.Response().StatusCode() != http.StatusUnauthorized
		},
	}
}
