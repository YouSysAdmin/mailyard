// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tlsbuild

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"

	"github.com/yousysadmin/mailyard/internal/core/certstore"
	certmodel "github.com/yousysadmin/mailyard/internal/models/certificate"
)

// A minted certificate - the ACME cache and the self-signed pair - is
// built here rather than by go-tlsutils, so the result lands in the
// certificates table instead of a directory on one node.
//
// go-tlsutils takes a CacheDir string and builds autocert.DirCache
// itself, with no way to hand it a Cache. That is the whole reason
// these lines exist.
//
// A directory cost this: with ACME every node ordered its own
// certificate, and Let's Encrypt allows five duplicates a week, so the
// sixth node served no TLS. Self-signed, every node generated a
// different pair, so a client reaching two nodes saw two certificates
// under one name - which is what pinning a fingerprint is meant to
// detect.
//
// The shared cache is also what makes validation work on more than one
// node: both challenge types write their token to it, so the CA can be
// answered by whichever node it reaches.

// selfSigned returns the installation's shared self-signed pair.
func (b *Builder) selfSigned() (tls.Certificate, error) {
	cert, err := certstore.SelfSigned(context.Background(), b.Store, selfSignedHosts(b.Host), "")
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("self-signed certificate: %w", err)
	}

	return cert, nil
}

// manager returns the ACME manager for the current settings, building
// it the first time and rebuilding it when the account details change.
//
// Nil when ACME is off, which is what makes the chain skip it.
//
// Rebuilt on a change to email or directory because autocert fixes both
// at construction. The directory one matters: moving from the staging
// CA to production is the ordinary last step of setting this up, and
// making it need a restart would put a restart in the middle of the one
// workflow this feature exists for.
func (b *Builder) manager(a ACME) *autocert.Manager {
	if !a.Enabled || b.Store == nil {
		return nil
	}

	key := a.Email + "\x00" + a.DirectoryURL
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.acmeMgr != nil && b.acmeKey == key {
		return b.acmeMgr
	}

	m := &autocert.Manager{
		Prompt: autocert.AcceptTOS,
		Cache:  &certstore.Cache{Store: b.Store},
		Email:  a.Email,
		// The policy is asked PER REQUEST rather than closed over a
		// list, so adding a host in the console takes effect without a
		// restart - which is the whole point of these being settings.
		HostPolicy: func(_ context.Context, host string) error {
			if allowedHost(b.acme(), host) {
				return nil
			}

			return fmt.Errorf("acme: %q is not in the configured host list", host)
		},
	}
	if a.DirectoryURL != "" {
		m.Client = &acme.Client{DirectoryURL: a.DirectoryURL}
	}

	b.acmeMgr, b.acmeKey = m, key

	return m
}

// acme reads the current settings, tolerating an unset provider so a
// Builder with no settings behind it simply has ACME off.
func (b *Builder) acme() ACME {
	if b.ACME == nil {
		return ACME{}
	}

	return b.ACME()
}

// allowedHost reports whether the CA may be asked for this name.
func allowedHost(a ACME, host string) bool {
	if !a.Enabled {
		return false
	}

	host = normalizeHost(host)
	for _, h := range a.Hosts {
		if normalizeHost(h) == host {
			return true
		}
	}

	return false
}

func normalizeHost(h string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(h), "."))
}

// startChallengeListener binds the HTTP-01 responder.
//
// Only when an address is configured, and the default is empty. An
// installation that terminates TLS here needs nothing: the CA validates
// over tls-alpn-01 against the listener that is already up. This is for
// the deployment where it cannot - a proxy that TERMINATES TLS answers
// the handshake itself, so ALPN validation never reaches us.
//
// Bound before anything is served, so a taken port fails the boot rather
// than being discovered at the first order.
func (b *Builder) startChallengeListener(m *autocert.Manager, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("acme challenge listener on %s: %w", addr, err)
	}

	srv := &http.Server{
		Handler:           m.HTTPHandler(http.HandlerFunc(redirectToHTTPS)),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		slog.Info("ACME HTTP-01 challenge listener", "addr", ln.Addr().String(), "cache", "database")
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			// Logged rather than fatal: only issuance is affected, and
			// whatever is already cached keeps being served.
			slog.Error("ACME HTTP server failed, http-01 issuance will not work", "err", err, "addr", addr)
		}
	}()
	b.onShutdown(srv.Shutdown)

	return nil
}

// redirectToHTTPS is the fallback behind autocert's challenge listener.
// The URL is built from r.Host and r.URL so the query string survives.
func redirectToHTTPS(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "https://"+r.Host+r.URL.RequestURI(), http.StatusPermanentRedirect)
}

// ACMEHosts lists the hosts this installation issues for, empty when
// ACME is off.
func (b *Builder) ACMEHosts() []string {
	a := b.acme()
	if !a.Enabled {
		return nil
	}

	return append([]string(nil), a.Hosts...)
}

// Order obtains a certificate for one host, now.
//
// Synchronous, and that is a judgement rather than an oversight. Over
// tls-alpn-01 the CA connects straight back to a listener that is
// already up, so an order is seconds rather than the minutes an HTTP-01
// setup behind DNS propagation can take - and an administrator who
// pressed a button wants to be told whether it worked, not handed an id
// to poll. The failure is the valuable part: the CA says exactly what it
// could not do, and that sentence is what gets shown.
// Takes no context, deliberately: autocert's GetCertificate accepts only
// a ClientHelloInfo and manages its own deadline internally, so a context
// here would be accepted and ignored - which is worse than not offering
// one.
func (b *Builder) Order(host string) error {
	a := b.acme()
	if !a.Enabled {
		return errors.New("acme is not enabled")
	}

	if !allowedHost(a, host) {
		return fmt.Errorf("%s is not in the ACME host list", host)
	}

	m := b.manager(a)
	if m == nil {
		return errors.New("acme is not available on this node")
	}

	// Through the manager rather than the served config: the served one
	// falls back to the self-signed pair for anything that fails, which
	// would report success having ordered nothing.
	if _, err := m.GetCertificate(acmeHello(host)); err != nil {
		// The client REMEMBERS a failed attempt for a minute
		// (createCertRetryAfter in autocert) and answers every ask in that
		// window with "acme/autocert: missing certificate" instead of
		// trying again. That is right for issuance driven by handshakes -
		// it stops a busy listener hammering the CA - and wrong for a
		// button: somebody who just fixed a DNS record and pressed Order
		// again would read a sentence that names nothing, having lost the
		// CA's actual complaint from the first attempt.
		//
		// Discarding the manager is what makes the retry a retry. The
		// account key lives in the shared cache, so nothing re-registers,
		// and any certificate already issued is re-read from there.
		//
		// Found by pressing the button twice.
		b.forgetManager()

		return err
	}

	return nil
}

// forgetManager drops the ACME client so the next use builds a fresh one.
func (b *Builder) forgetManager() {
	b.mu.Lock()
	b.acmeMgr, b.acmeKey = nil, ""
	b.mu.Unlock()
}

// Renew forces a fresh certificate for one host.
//
// It DELETES the cached entry and then asks again, because autocert has
// no "renew now": it renews when it decides to, from the timer armed
// inside Manager.cert. Dropping the cache is what turns the next request
// into an order.
//
// Both cache keys go, the ECDSA one and the RSA variant. Deleting only
// the one this process would ask for leaves the other stale, and which
// one a client gets depends on the client.
func (b *Builder) Renew(ctx context.Context, host string) error {
	if b.Store == nil {
		return errors.New("this node serves no ACME certificate")
	}

	for _, key := range []string{host, host + "+rsa"} {
		if err := b.Store.Delete(ctx, certmodel.ScopeACME, key); err != nil {
			return fmt.Errorf("clearing the cached certificate: %w", err)
		}
	}

	return b.Order(host)
}

// selfSignedHosts is the SAN list for a generated pair.
//
// The name itself AND a wildcard under it, because they cover different
// things: *.mail.example.com does not match mail.example.com, so a
// certificate carrying only the wildcard fails on the very name the
// operator configured. Together they cover the host and anything an
// installation puts beneath it - a per-tenant tracking name, a separate
// MX - without a second certificate.
//
// localhost stays, so a probe or a health check from the same machine
// works without disabling verification.
func selfSignedHosts(fqdn string) []string {
	fqdn = strings.TrimSpace(fqdn)
	// Empty AND the case where the operator's own name IS localhost,
	// which a scratch instance has. Appending it a second time put
	// "DNS:localhost, DNS:localhost" in the SAN list of every generated
	// pair on such an installation - harmless, and it reads as a bug to
	// anybody running openssl on it.
	if fqdn == "" || strings.EqualFold(fqdn, "localhost") {
		return []string{"localhost"}
	}

	hosts := []string{fqdn}
	// Only where a wildcard is meaningful. A single label has nothing
	// under it worth naming, and an IP address cannot carry one at all
	// - "*.127.0.0.1" is not a name any client will match.
	if strings.Contains(fqdn, ".") && net.ParseIP(fqdn) == nil {
		hosts = append(hosts, "*."+fqdn)
	}

	return append(hosts, "localhost")
}
