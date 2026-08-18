// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package tlsbuild decides what each listener presents on a handshake.
//
// There is one chain, and every listener that terminates TLS walks it:
//
//	assigned managed certificate  ->  ACME  ->  self-signed
//
// The assignment is a platform setting, resolved per handshake so a
// replacement needs no restart and reaches a second node. ACME is
// consulted only for a name it was configured to issue for. The
// self-signed pair is generated once, stored in the database and
// shared by every node.
//
// Whether a listener terminates TLS is a boolean in config; WHICH
// certificate it serves is never a config key, only this chain.
package tlsbuild

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"

	"github.com/yousysadmin/mailyard/internal/core/certstore"
)

// ACME is what the installation is currently configured to do.
//
// Read fresh on every use rather than captured, because all of it lives
// in platform settings now and an administrator changes it while the
// process runs. See env.ACMEConfig for why it stopped being config.
type ACME struct {
	Enabled bool
	Hosts   []string
	Email   string

	// DirectoryURL points at a different CA. Empty is Let's Encrypt
	// production.
	DirectoryURL string
}

// Builder materializes the chain once and hands each listener its own
// view of it.
type Builder struct {
	// Store is where a minted certificate lives: the ACME cache and the
	// self-signed pair. Both are installation-wide secrets and a
	// directory is per node - see dbbacked.go for what that cost.
	//
	// Required. Falling back to a directory cache is only reachable by a
	// caller with no database, and there is no longer any mode that
	// works without a store.
	Store certstore.Store

	// Host is the name a generated self-signed pair carries, normally
	// derived from server.public_url. Empty means localhost, which is
	// the right answer for a scratch instance nobody named.
	Host string

	// ACME answers what Let's Encrypt is configured to do, read fresh on
	// every handshake and every order. A function rather than a value
	// because these are settings, not config: turning ACME on, adding a
	// host and ordering all happen while the process runs.
	//
	// Nil means ACME is off, which is the honest answer for a caller
	// with no settings behind it.
	ACME func() ACME

	// ChallengeAddr binds an HTTP-01 responder. Empty means tls-alpn-01
	// only, which needs no port - see startChallengeListener.
	ChallengeAddr string

	// Assigned answers "which managed certificate does this listener
	// serve", by the name an admin gave it.
	//
	// It takes the listener, not a config block. Passing the block and
	// inferring the listener by comparing values cannot work, because
	// the ordinary configuration is three identical blocks. Three
	// `{mode: self}` blocks make the HTTP listener match the submission
	// block, read the submission setting, find it
	// empty and serve the config certificate while the console showed an
	// assignment. Reported from a live instance, and the tests missed it
	// because they set TLS on one block only.
	Assigned func(listener string) string

	// Log carries the one thing that must not be silent: an assigned
	// certificate that will not load, where the listener quietly serves
	// something else.
	Log *slog.Logger

	// buildOnce guards the base config, and is deliberately not the
	// mutex below. Build() reaches manager(), which takes that mutex, and
	// holding it across the build deadlocked instantly - Go mutexes are
	// not reentrant. Two locks because there are two lifetimes: the base
	// is built once, the manager is rebuilt whenever the account details
	// change.
	buildOnce sync.Once
	base      *tls.Config
	baseErr   error

	mu        sync.Mutex
	shutdowns []func(context.Context) error

	// acmeMgr is the manager for acmeKey, the account details it was
	// built with. Rebuilt when those change - see manager.
	acmeMgr *autocert.Manager
	acmeKey string
}

// Build resolves what one listener presents. Not enabled returns
// (nil, nil), meaning "serve without TLS".
//
// Two layers. What the installation builds is built once - one
// self-signed pair, one ACME manager, one challenge listener - because
// three listeners asking must not mean three orders for one name, and
// Let's Encrypt allows five duplicates a week. What the listener serves
// is decided per listener, on a clone, since three listeners are three
// assignments.
//
// Memoizing the wrapped config would collapse the two and leave all
// three sharing one Resolver.
func (b *Builder) Build(listener string, enabled bool) (*tls.Config, error) {
	if !enabled {
		return nil, nil
	}

	base, err := b.baseConfig()
	if err != nil {
		return nil, err
	}

	if b.Assigned == nil {
		return base, nil
	}

	// The assignment WRAPS the chain rather than replacing it, so
	// "nothing assigned" is simply the rest of the chain.
	r := &certstore.Resolver{
		Store:    b.Store,
		Assigned: func() string { return b.Assigned(listener) },
		Log:      b.Log,
	}
	out := base.Clone()
	out.GetCertificate = r.Wrap(base)
	// Certificates must go, or a client whose hello matches one of them
	// is served from the slice and never reaches GetCertificate at all.
	// On the CLONE, so the base keeps them as the fallback and the other
	// listeners sharing it are untouched.
	out.Certificates = nil

	return out, nil
}

// baseConfig builds the installation's chain below the assignment, once.
//
// The error is remembered too. Retrying per listener would report the
// same failure three times, and with ACME it would bind the challenge
// port again on the attempt after the one that half succeeded.
func (b *Builder) baseConfig() (*tls.Config, error) {
	b.buildOnce.Do(func() { b.base, b.baseErr = b.build() })

	return b.base, b.baseErr
}

// build assembles the chain below the assignment: ACME where it applies,
// the self-signed pair everywhere else.
//
// The self-signed pair is built EVEN WITH ACME ON, and that is
// deliberate: it is the fallback for every name ACME was not configured
// for. A listener whose hostname is not in the list - the ordinary case
// for an MX, where the public URL names the console - would otherwise
// fail every handshake. This gives it working opportunistic TLS.
//
// Nothing here depends on whether ACME is enabled RIGHT NOW. The config
// is built once and consults the settings per handshake, so turning ACME
// on is a change an administrator makes rather than a restart.
func (b *Builder) build() (*tls.Config, error) {
	if b.Store == nil {
		return nil, errors.New("tlsbuild: no certificate store")
	}

	self, err := b.selfSigned()
	if err != nil {
		return nil, err
	}

	// Bound at boot or not at all, because it takes a port. Only when
	// the operator asked for one: tls-alpn-01 needs none.
	if b.ChallengeAddr != "" {
		if m := b.manager(b.acme()); m != nil {
			if err := b.startChallengeListener(m, b.ChallengeAddr); err != nil {
				return nil, err
			}
		} else {
			slog.Warn("acme.challenge_addr is set but ACME is off, so no challenge listener was bound - turn ACME on and restart if you need http-01",
				"addr", b.ChallengeAddr)
		}
	}

	cfg := &tls.Config{
		Certificates: []tls.Certificate{self},
		MinVersion:   tls.VersionTLS12,
		NextProtos:   servableProtos(),
	}
	cfg.GetCertificate = b.acmeOrSelfSigned(self)
	b.watchACME()

	return cfg, nil
}

// acmeOrSelfSigned sends a configured name to the CA and anything else
// to the self-signed pair.
//
// Decided from the hello and the CURRENT settings, not from an error the
// manager returned. Matching on a failure would fall back on a network
// problem or a rate limit too, and serving a self-signed certificate for
// a name that has a real one is the one substitution nobody wants.
func (b *Builder) acmeOrSelfSigned(self tls.Certificate) func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		a := b.acme()
		m := b.manager(a)

		// A TLS-ALPN-01 handshake IS the validation, and the manager is
		// the only thing that can answer it. Handing back the self-signed
		// pair here would fail the order for a name that is configured,
		// which is the opposite of what the fallback is for. Its error is
		// returned rather than swallowed: the CA sees a failed handshake
		// either way, and hiding it would lose the reason.
		if wantsALPNChallenge(hello) {
			if m == nil {
				return nil, errors.New("acme: a tls-alpn-01 challenge arrived but acme is not enabled")
			}

			return m.GetCertificate(hello)
		}

		if m != nil && allowedHost(a, hello.ServerName) {
			cert, err := m.GetCertificate(hello)
			if err == nil {
				return cert, nil
			}

			// Audible, then fall through. A configured host with no
			// certificate yet is the ordinary state before the first
			// order, and refusing the handshake would take the console
			// down at exactly the moment somebody needs it to press
			// Order.
			b.logf("acme: no certificate for a configured host, serving the self-signed pair", "host", hello.ServerName, "err", err)
		}

		// No SNI lands here too, which is right: autocert refuses a hello
		// with no server name, and a client connecting by IP address
		// getting a certificate it will warn about beats one getting no
		// handshake at all.
		cert := self

		return &cert, nil
	}
}

// servableProtos is what this server may advertise over ALPN.
//
// autocert's own TLSConfig offers h2, which fasthttp cannot speak at
// all: a client negotiates it, sends the HTTP/2 preface, and fasthttp
// reads that as a request line. Measured - a Go client preferring h2
// gets "frame too large, note that the frame header looked like an
// HTTP/1.1 header" where http/1.1 gets 200.
//
// acme-tls/1 is advertised even with ACME off, which is what makes
// turning ACME on a setting rather than a restart. It costs nothing,
// because nothing asks for it.
func servableProtos() []string {
	return []string{"http/1.1", acme.ALPNProto}
}

// wantsALPNChallenge reports whether this hello is a CA validating a
// domain rather than a client wanting to talk.
func wantsALPNChallenge(hello *tls.ClientHelloInfo) bool {
	return slices.Contains(hello.SupportedProtos, acme.ALPNProto)
}

func (b *Builder) logf(msg string, args ...any) {
	if b.Log != nil {
		b.Log.Warn(msg, args...)

		return
	}

	slog.Warn(msg, args...)
}

// acmeCheckInterval is how often each ACME host is touched after the
// first pass. Renewal itself happens ~30 days before expiry, so this
// only has to be short enough that a host which failed its first touch
// gets another chance the same day.
const acmeCheckInterval = time.Hour

// acmeWarnBefore is when a certificate that has not renewed starts being
// reported as a problem. autocert aims to renew 30 days out, so still
// holding a cert with 14 days left means renewal has been failing for a
// fortnight and nothing has said so.
const acmeWarnBefore = 14 * 24 * time.Hour

// watchACME makes autocert's background renewal actually cover every
// configured host.
//
// autocert renews on its own, but the timer is armed inside
// Manager.cert, which runs on the first handshake for that SNI name and
// nowhere else. So after a restart nothing renews until someone
// connects, and a host that never gets traffic - the ordinary state of
// an MX or a quiet relay - never renews at all.
//
// Touching each host through GetCertificate arms the timer. Hourly,
// because the first touch can fail on DNS that was not ready at
// boot.
func (b *Builder) watchACME() {
	ctx, cancel := context.WithCancel(context.Background())
	b.onShutdown(func(context.Context) error {
		cancel()

		return nil
	})

	go func() {
		t := time.NewTicker(acmeCheckInterval)
		defer t.Stop()
		for {
			// Through the MANAGER, never the served config. The served
			// one falls back to the self-signed pair on any failure, so
			// touching it would report a healthy certificate while the
			// ACME one quietly expired - the same trap that made Renew
			// order nothing when it went through a wrapped config.
			//
			// Read per pass, not captured: a host added in the console
			// has to start being renewed without a restart, the same way
			// it starts being served without one.
			a := b.acme()
			if m := b.manager(a); m != nil {
				for _, h := range a.Hosts {
					touchACMEHost(m, h)
				}
			}

			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

// touchACMEHost asks the manager for one host's certificate and reports
// what came back.
func touchACMEHost(m *autocert.Manager, host string) {
	cert, err := m.GetCertificate(acmeHello(host))
	if err != nil {
		// Not fatal. An ACME failure at boot usually means DNS has not
		// propagated or the challenge port is not reachable yet, and the
		// next tick may well succeed. Serving continues on whatever is
		// already cached, and on the self-signed pair if nothing is.
		slog.Error("acme: could not obtain a certificate, will retry",
			"host", host, "interval", acmeCheckInterval, "err", err)

		return
	}

	if cert == nil || cert.Leaf == nil {
		return
	}

	left := time.Until(cert.Leaf.NotAfter)
	switch {
	case left <= 0:
		slog.Error("acme: certificate has EXPIRED and renewal is not succeeding",
			"host", host, "expired_at", cert.Leaf.NotAfter)
	case left < acmeWarnBefore:
		slog.Warn("acme: certificate is close to expiry and should have renewed by now",
			"host", host, "expires_at", cert.Leaf.NotAfter, "days_left", int(left.Hours()/24))
	default:
		slog.Debug("acme: certificate valid",
			"host", host, "expires_at", cert.Leaf.NotAfter, "days_left", int(left.Hours()/24))
	}
}

// acmeHello is the synthetic ClientHello used to touch a host.
//
// The ECDSA fields are not decoration. autocert picks the cache key from
// supportsECDSA(hello), and a zero-valued hello reports no ECDSA support
// - so it would fetch, and then renew, the RSA variant of the
// certificate while real clients keep being served the ECDSA one.
// Advertising an ECDSA suite makes this touch land on the same cache
// entry a browser gets.
func acmeHello(host string) *tls.ClientHelloInfo {
	return &tls.ClientHelloInfo{
		ServerName:        host,
		SignatureSchemes:  []tls.SignatureScheme{tls.ECDSAWithP256AndSHA256},
		SupportedCurves:   []tls.CurveID{tls.CurveP256},
		SupportedVersions: []uint16{tls.VersionTLS13, tls.VersionTLS12},
		CipherSuites:      []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256},
	}
}

// onShutdown records a cleanup. Guarded because build() no longer runs
// under the mutex, so an append here can race a Shutdown.
func (b *Builder) onShutdown(fn func(context.Context) error) {
	b.mu.Lock()
	b.shutdowns = append(b.shutdowns, fn)
	b.mu.Unlock()
}

// Shutdown stops every background resource the built config started
// (currently the ACME HTTP-01 challenge listener and the watch loop).
// Safe to call on a Builder that never built anything.
func (b *Builder) Shutdown(ctx context.Context) error {
	b.mu.Lock()
	fns := b.shutdowns
	b.shutdowns = nil
	b.mu.Unlock()

	var errs []error
	for _, fn := range fns {
		if fn == nil {
			continue
		}

		if err := fn(ctx); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}
