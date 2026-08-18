// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package certstore

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"log/slog"
	"sync"
	"time"

	certmodel "github.com/yousysadmin/mailyard/internal/models/certificate"
)

// Resolver serves the certificate an administrator ASSIGNED to a
// listener, falling back to whatever the config file built.
//
// The database wins, deliberately. A certificate that has expired is
// the one emergency where the operator most needs a way in that does
// not involve a shell on the box and a restart of the mail server -
// and the config file is exactly that. An installation that assigns
// nothing behaves as it always did, because the fallback is the
// config-built certificate itself.
//
// It resolves per HANDSHAKE, through a short cache. That is what makes
// a replacement take effect without a restart, and what makes a second
// node pick it up: both read the same row, and neither holds it for
// longer than the TTL.
type Resolver struct {
	Store Store

	// Assigned returns the name of the managed certificate this
	// listener should use, or "" for none. A function rather than a
	// value because it is a platform setting an admin can change while
	// the process runs.
	Assigned func() string

	// TTL bounds how long a resolved certificate is held. Short
	// enough that a replacement is picked up while somebody is still
	// looking at the page, long enough that a busy listener is not
	// asking the database per connection.
	TTL time.Duration

	// Log is where a failure to load an assigned certificate is
	// reported. Silence there would mean serving the config
	// certificate while the console shows the assigned one.
	Log *slog.Logger

	mu       sync.Mutex
	cached   *tls.Certificate
	cachedAt time.Time

	// lastName is what the cache holds, so changing the assignment
	// invalidates it immediately rather than at the end of the TTL.
	lastName string
}

const defaultResolverTTL = 30 * time.Second

// Wrap returns a GetCertificate for cfg that prefers the assigned
// certificate and otherwise defers to what cfg already does.
//
// Wrapping rather than replacing is what keeps every existing mode
// working untouched: acme keeps its own GetCertificate, manual keeps
// its Certificates slice, and both are simply what happens when
// nothing is assigned.
func (r *Resolver) Wrap(cfg *tls.Config) func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	fallback := fallbackOf(cfg)

	return func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		if cert := r.assigned(); cert != nil {
			return cert, nil
		}

		return fallback(hello)
	}
}

// assigned returns the managed certificate, or nil to fall back.
func (r *Resolver) assigned() *tls.Certificate {
	if r == nil || r.Store == nil || r.Assigned == nil {
		return nil
	}

	name := r.Assigned()
	if name == "" {
		return nil
	}

	r.mu.Lock()
	ttl := r.TTL
	if ttl <= 0 {
		ttl = defaultResolverTTL
	}

	if r.cached != nil && r.lastName == name && time.Since(r.cachedAt) < ttl {
		cert := r.cached
		r.mu.Unlock()

		return cert
	}

	r.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rec, err := r.Store.Get(ctx, certmodel.ScopeManaged, name)
	if err != nil || rec == nil || rec.Data == "" {
		// Fall back rather than fail the handshake. An assignment
		// naming a row that is gone should degrade to the config
		// certificate, not take the listener down - but it must be
		// audible, or the console shows one certificate while the wire
		// carries another.
		r.logf("certificates: assigned certificate unavailable, serving the configured one", "name", name, "err", err)

		return nil
	}

	cert, err := tls.X509KeyPair([]byte(rec.Data), []byte(rec.Data))
	if err != nil {
		r.logf("certificates: assigned certificate does not load", "name", name, "err", err)

		return nil
	}

	// An AUTHORITY assigned to a listener. Refused here rather than
	// served, because a CA carries no subject alt name and no
	// ServerAuth, so serving it fails every client - where the
	// configured certificate works.
	//
	// The settings handler refuses this at the moment somebody writes
	// the assignment, and the console does not offer one. This is the
	// check that still holds when the row was edited in the database
	// directly, which is the only path the other two do not cover.
	if leaf := leafOf(&cert); leaf != nil && leaf.IsCA {
		r.logf("certificates: the assigned certificate is a certificate authority, serving the configured one instead", "name", name)

		return nil
	}

	r.mu.Lock()
	r.cached, r.cachedAt, r.lastName = &cert, time.Now(), name
	r.mu.Unlock()

	return &cert
}

// leafOf parses the leaf out of a loaded pair.
//
// tls.X509KeyPair does not fill Leaf, so this parses the first DER in
// the chain. Nil on anything unexpected: the caller treats that as
// "not a CA", which serves the certificate rather than refusing over a
// parse this code could not do.
func leafOf(cert *tls.Certificate) *x509.Certificate {
	if cert == nil {
		return nil
	}

	if cert.Leaf != nil {
		return cert.Leaf
	}

	if len(cert.Certificate) == 0 {
		return nil
	}

	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil
	}

	return leaf
}

func (r *Resolver) logf(msg string, args ...any) {
	if r.Log != nil {
		r.Log.Warn(msg, args...)

		return
	}

	slog.Warn(msg, args...)
}

// fallbackOf turns whatever cfg already does into one function.
//
// A config from acme carries GetCertificate - one from manual or self
// carries Certificates. Reading both here is what lets Wrap treat
// every mode the same.
func fallbackOf(cfg *tls.Config) func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	if cfg == nil {
		return func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return nil, errNoCertificate }
	}

	if cfg.GetCertificate != nil {
		return cfg.GetCertificate
	}

	certs := cfg.Certificates

	return func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		for i := range certs {
			if err := hello.SupportsCertificate(&certs[i]); err == nil {
				return &certs[i], nil
			}
		}

		if len(certs) > 0 {
			// Present the first anyway. A name mismatch is the
			// client's to judge, and refusing here would turn a
			// warning in a browser into a connection that never
			// happens.
			return &certs[0], nil
		}

		return nil, errNoCertificate
	}
}

type certErr string

// Error renders the failure for a log or a caller.
func (e certErr) Error() string { return string(e) }

const errNoCertificate = certErr("certificates: nothing to serve")
