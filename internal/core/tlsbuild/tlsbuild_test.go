// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tlsbuild

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"net/http"
	"slices"

	"math/big"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/certgen"
	certmodel "github.com/yousysadmin/mailyard/internal/models/certificate"

	"github.com/gofiber/fiber/v3"
	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

// autocert keys its cache on whether the ClientHello advertised ECDSA
// support: an ECDSA-capable hello reads "example.com", one without
// reads "example.com+rsa". A zero-valued ClientHelloInfo lands on the
// RSA key, so a naive touch would fetch and renew a second, RSA
// certificate that no real client is ever served, while the ECDSA one
// browsers actually get aged out untouched.
//
// The cache here holds only an ECDSA certificate under the plain
// domain key. validCert refuses to serve it under a +rsa key, so this
// call can only succeed if acmeHello asked the way a browser does.
func TestACMEHelloHitsTheECDSACacheEntry(t *testing.T) {
	dir := t.TempDir()
	leaf := writeCachedECDSACert(t, dir, "example.com", 60*24*time.Hour)

	m := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		Cache:      autocert.DirCache(dir),
		HostPolicy: autocert.HostWhitelist("example.com"),
	}

	got, err := m.GetCertificate(acmeHello("example.com"))
	if err != nil {
		t.Fatalf("GetCertificate: %v\nacmeHello did not advertise ECDSA, so autocert looked for the +rsa cache entry", err)
	}

	if !bytes.Equal(got.Certificate[0], leaf.Raw) {
		t.Error("served a different certificate than the one in the cache")
	}
}

// The renewal timer is armed inside autocert's cache-load path, which
// only runs on a handshake. A listener nobody connects to therefore never
// renews. watchACME exists to run that path itself, so every configured
// host is touched whether or not it sees traffic.
//
// It goes through the MANAGER, never the served config. The served one
// falls back to the self-signed pair on any failure, so touching that
// would report a healthy certificate while the ACME one quietly expired.
// Seeding the cache is what keeps this offline: a hit means no CA is
// involved.
func TestWatchACMETouchesEveryHost(t *testing.T) {
	hosts := []string{"a.example.com", "b.example.com", "c.example.com"}

	rows := map[string]string{}
	for _, h := range hosts {
		certPEM, keyPEM := selfSignedFixture(t, h)
		rows[certmodel.ScopeACME+"/"+h] = keyPEM + certPEM
	}

	store := &recordingStore{rows: rows}

	b := &Builder{Store: store, ACME: staticACME(ACME{Enabled: true, Hosts: hosts})}
	b.watchACME()
	t.Cleanup(func() { _ = b.Shutdown(context.Background()) })

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if touched(store, hosts) {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("did not touch every host: asked %v", store.asked())
}

// touched reports whether the cache was consulted for every host.
func touched(store *recordingStore, hosts []string) bool {
	asked := map[string]bool{}
	for _, k := range store.asked() {
		asked[k] = true
	}

	for _, h := range hosts {
		if !asked[certmodel.ScopeACME+"/"+h] {
			return false
		}
	}

	return true
}

// Shutdown has to stop the ticker goroutine. Left running it would keep
// calling into a certificate manager the process is finished with, and in
// tests it leaks a goroutine per Builder.
func TestShutdownStopsTheACMEWatcher(t *testing.T) {
	const host = "a.example.com"
	certPEM, keyPEM := selfSignedFixture(t, host)
	store := &recordingStore{rows: map[string]string{
		certmodel.ScopeACME + "/" + host: keyPEM + certPEM,
	}}

	b := &Builder{Store: store, ACME: staticACME(ACME{Enabled: true, Hosts: []string{host}})}
	b.watchACME()

	// Let the first pass land, then stop.
	waitFor(t, func() bool { return len(store.asked()) > 0 })
	if err := b.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	after := len(store.asked())

	// The ticker is an hour out, so any further call within this window
	// means the goroutine outlived Shutdown.
	time.Sleep(100 * time.Millisecond)
	if now := len(store.asked()); now != after {
		t.Errorf("watcher kept running after Shutdown: %d -> %d reads", after, now)
	}
}

// A listener that does not terminate TLS must build nothing at all: no
// certificate, no watcher, no challenge listener. It returns before it
// even reaches the store, which is why this can pass a nil one.
func TestADisabledListenerBuildsNothing(t *testing.T) {
	b := &Builder{}
	cfg, err := b.Build("server", false)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if cfg != nil {
		t.Error("a listener with TLS off should serve without TLS")
	}

	if len(b.shutdowns) != 0 {
		t.Errorf("a disabled listener registered %d shutdown hooks, want 0", len(b.shutdowns))
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatal("condition not met within 5s")
}

// writeCachedECDSACert puts a self-signed ECDSA certificate into an
// autocert DirCache under the plain domain key, in the layout
// autocert writes: the private key PEM followed by the chain.
func writeCachedECDSACert(t *testing.T, dir, host string, validFor time.Duration) *x509.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(validFor),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := pem.Encode(&buf, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}); err != nil {
		t.Fatal(err)
	}

	if err := pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, host), buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	return leaf
}

// recordingStore is a certstore.Store that remembers what was asked
// of it.
type recordingStore struct {
	mu   sync.Mutex
	rows map[string]string
	gets []string
	puts int
}

func (r *recordingStore) Get(_ context.Context, scope, name string) (*certmodel.Certificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := scope + "/" + name
	r.gets = append(r.gets, key)
	data, ok := r.rows[key]
	if !ok {
		return nil, nil
	}

	return &certmodel.Certificate{Scope: scope, Name: name, Data: data}, nil
}
func (r *recordingStore) Put(context.Context, *certmodel.Certificate) error { return nil }
func (r *recordingStore) PutIfAbsent(context.Context, *certmodel.Certificate) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.puts++

	return true, nil
}
func (r *recordingStore) Delete(context.Context, string, string) error { return nil }

func (r *recordingStore) asked() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.gets...)
}

// The load-bearing claim of putting certificates in the database: a
// certificate IN THE TABLE is what the listener serves.
//
// Seeding the cache is what keeps this offline. autocert consults its
// Cache before it contacts anybody, so a hit means no CA is involved.
// Seed nothing and the test goes to the real Let's Encrypt, which is
// slow, flaky and rude - a test that needs the internet to prove a
// wiring decision is testing the internet.
func TestACMEServesWhatIsInTheStore(t *testing.T) {
	const host = "mail.example.com"

	// ECDSA, and that is not a detail. autocert keys its cache on
	// supportsECDSA(hello) and then REQUIRES the cached private key to
	// be of that type. An RSA fixture under the plain key is rejected as
	// if the cache were empty and the manager goes to the CA, reaching
	// the real Let's Encrypt and failing only because they refuse
	// example.com. Same trap acmeHello was written for.
	certPEM, keyPEM := selfSignedFixture(t, host)
	// autocert's own layout: the private key first, then the chain,
	// under the hostname as the cache key.
	store := &recordingStore{
		rows: map[string]string{
			certmodel.ScopeACME + "/" + host: keyPEM + certPEM,
		},
	}

	b := &Builder{Store: store, Host: host, ACME: staticACME(ACME{
		Enabled: true,
		Hosts:   []string{host},
	}),
		// Port 0 so the test does not fight for :80. The address is bound
		// before Build returns either way, which is the property that
		// makes a taken port fail the boot.
		ChallengeAddr: "127.0.0.1:0",
	}
	t.Cleanup(func() { _ = b.Shutdown(context.Background()) })

	cfg, err := b.Build("server", true)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if cfg == nil || cfg.GetCertificate == nil {
		t.Fatal("acme produced no GetCertificate")
	}

	got, err := cfg.GetCertificate(acmeHello(host))
	if err != nil {
		t.Fatalf("GetCertificate: %v", err)
	}

	if got == nil || len(got.Certificate) == 0 {
		t.Fatal("no certificate came back")
	}

	want, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		t.Fatalf("fixture pair: %v", err)
	}

	if !bytes.Equal(got.Certificate[0], want.Certificate[0]) {
		t.Error("the served certificate is not the one in the store")
	}

	if asked := store.asked(); len(asked) == 0 {
		t.Error("the store was never consulted - the cache is still a directory")
	}
}

// Three identical blocks, three different assignments.
//
// This is the ordinary configuration - `{mode: self}` written out for
// the HTTP listener and both SMTP ones - and it is the case that breaks
// if the assignment is looked up by comparing the block
// against each listener's block, so the HTTP listener matched the
// submission one and read a setting nobody had written. Worse, the
// WRAPPED config was memoized on the block, so all three listeners
// shared one *tls.Config and one Resolver and could not have served
// different certificates whatever the settings said.
//
// Reported from a live instance. The tests missed it because every one
// of them configured TLS on a single block.
func TestIdenticalBlocksStillGetTheirOwnAssignment(t *testing.T) {
	serverCert, serverKey := selfSignedFixture(t, "console.example.com")
	mxCert, mxKey := selfSignedFixture(t, "mx.example.com")

	store := &recordingStore{rows: map[string]string{
		certmodel.ScopeManaged + "/console": serverKey + serverCert,
		certmodel.ScopeManaged + "/mx":      mxKey + mxCert,
	}}

	// What an operator wrote in the console: the HTTP listener on one
	// certificate, the MX on another, submission on neither.
	assigned := map[string]string{
		"server":     "console",
		"submission": "",
		"inbound":    "mx",
	}
	b := &Builder{
		Store:    store,
		Host:     "shared.example.com",
		Assigned: func(l string) string { return assigned[l] },
	}
	t.Cleanup(func() { _ = b.Shutdown(context.Background()) })

	got := map[string]string{}
	for _, listener := range []string{"server", "submission", "inbound"} {
		cfg, err := b.Build(listener, true)
		if err != nil {
			t.Fatalf("%s: Build: %v", listener, err)
		}

		if cfg == nil || cfg.GetCertificate == nil {
			t.Fatalf("%s: no GetCertificate", listener)
		}

		crt, err := cfg.GetCertificate(&tls.ClientHelloInfo{
			ServerName:        "anything.example.com",
			SignatureSchemes:  []tls.SignatureScheme{tls.ECDSAWithP256AndSHA256},
			SupportedVersions: []uint16{tls.VersionTLS13},
		})
		if err != nil {
			t.Fatalf("%s: GetCertificate: %v", listener, err)
		}

		leaf, err := x509.ParseCertificate(crt.Certificate[0])
		if err != nil {
			t.Fatalf("%s: parse: %v", listener, err)
		}

		got[listener] = leaf.Subject.CommonName
	}

	if got["server"] != "console.example.com" {
		t.Errorf("the HTTP listener served %q, want the certificate assigned to it", got["server"])
	}

	if got["inbound"] != "mx.example.com" {
		t.Errorf("the MX served %q, want the certificate assigned to it", got["inbound"])
	}

	// Nothing assigned means the rest of the chain, which is what makes
	// an unassigned listener keep working.
	if got["submission"] != "shared.example.com" {
		t.Errorf("the unassigned listener served %q, want the installation self-signed pair", got["submission"])
	}
}

// selfSignedFixture is a managed pair for one name.
func selfSignedFixture(t *testing.T, cn string) (certPEM, keyPEM string) {
	t.Helper()
	c, k, err := certgen.MintLeaf(certgen.LeafRequest{
		Subject:   certgen.Subject{CommonName: cn},
		Hosts:     []string{cn},
		Algorithm: certgen.AlgECDSA,
		Validity:  90 * 24 * time.Hour,
	}, nil)
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}

	return c, k
}

// A name ACME was not configured for falls through to the self-signed
// pair instead of failing the handshake.
//
// This is the ordinary state of an MX. Server.public_url names the
// console, so acme.hosts derives to the console name, and the mail
// listeners answer under a hostname nobody put in that list. Before the
// chain existed those listeners were on a mode of their own and this
// question could not arise - with one installation-wide ACME setup it
// arises immediately, and the wrong answer is a listener that offers
// STARTTLS and then refuses every session.
func TestANameOutsideTheACMEListGetsTheSelfSignedPair(t *testing.T) {
	self, _ := loadFixture(t, "mx.example.com")
	b := &Builder{ACME: staticACME(ACME{Enabled: true, Hosts: []string{"console.example.com"}})}

	got, err := b.acmeOrSelfSigned(self)(&tls.ClientHelloInfo{ServerName: "mx.example.com"})
	if err != nil {
		t.Fatalf("GetCertificate: %v", err)
	}

	if got == nil {
		t.Fatal("no certificate came back")
	}

	// No store, so a manager could not have been built even if the name
	// had been allowed - which is the point: an unconfigured name must
	// never reach the CA path at all.
	leaf, err := x509.ParseCertificate(got.Certificate[0])
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if leaf.Subject.CommonName != "mx.example.com" {
		t.Errorf("served %q, want the self-signed pair", leaf.Subject.CommonName)
	}
}

// No SNI lands on the self-signed pair too. autocert refuses a hello
// with no server name, and a client dialling by IP getting a warning
// beats it getting no handshake.
func TestNoSNIGetsTheSelfSignedPair(t *testing.T) {
	self, _ := loadFixture(t, "console.example.com")
	b := &Builder{ACME: staticACME(ACME{Enabled: true, Hosts: []string{"console.example.com"}})}

	if _, err := b.acmeOrSelfSigned(self)(&tls.ClientHelloInfo{}); err != nil {
		t.Fatalf("a hello with no SNI: %v", err)
	}
}

// A configured name is recognised whatever its case or trailing dot,
// because that is what decides whether the CA is asked at all.
func TestAConfiguredNameIsMatchedLoosely(t *testing.T) {
	a := ACME{Enabled: true, Hosts: []string{"Console.Example.com."}}
	for _, name := range []string{"console.example.com", "CONSOLE.example.com", "console.example.com."} {
		if !allowedHost(a, name) {
			t.Errorf("allowedHost(%q) = false - case and a trailing dot must not decide this", name)
		}
	}

	if allowedHost(a, "other.example.com") {
		t.Error("an unconfigured name was allowed")
	}

	// And nothing is allowed with ACME off, however the list reads.
	if allowedHost(ACME{Hosts: []string{"console.example.com"}}, "console.example.com") {
		t.Error("a name was allowed with acme disabled")
	}
}

// A TLS-ALPN-01 handshake IS the validation, so it must never be answered
// from the fallback - that fails the order, and the symptom is a
// certificate that never issues with nothing pointing at the cause.
//
// With ACME off there is no manager to answer it, and the honest reply is
// an error rather than the self-signed pair.
func TestTheALPNChallengeIsNeverAnsweredFromTheFallback(t *testing.T) {
	self, _ := loadFixture(t, "console.example.com")
	b := &Builder{ACME: staticACME(ACME{})}

	_, err := b.acmeOrSelfSigned(self)(&tls.ClientHelloInfo{
		ServerName:      "console.example.com",
		SupportedProtos: []string{acme.ALPNProto},
	})
	if err == nil {
		t.Error("an ALPN validation handshake was answered from the fallback, which fails the order")
	}
}

// Three listeners, one self-signed pair.
//
// The sharing is the reason a Builder exists at all: three listeners
// each minting their own would put three certificates under one
// hostname, and with ACME it would be three orders for one name - Let's
// Encrypt allows five duplicates a week.
func TestEveryListenerSharesOneGeneratedPair(t *testing.T) {
	store := &recordingStore{rows: map[string]string{}}
	b := &Builder{Store: store, Host: "shared.example.com", ACME: staticACME(ACME{})}
	t.Cleanup(func() { _ = b.Shutdown(context.Background()) })

	for _, listener := range []string{"server", "submission", "inbound"} {
		if _, err := b.Build(listener, true); err != nil {
			t.Fatalf("%s: %v", listener, err)
		}
	}

	if store.puts != 1 {
		t.Errorf("stored %d generated pairs for three listeners, want 1", store.puts)
	}
}

// loadFixture is a parsed pair for one name.
func loadFixture(t *testing.T, cn string) (tls.Certificate, *x509.Certificate) {
	t.Helper()
	certPEM, keyPEM := selfSignedFixture(t, cn)
	cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		t.Fatalf("fixture pair: %v", err)
	}

	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("fixture leaf: %v", err)
	}

	return cert, leaf
}

// A browser must be able to open the console with ACME on.
//
// autocert's TLSConfig advertises h2, and the HTTP server is fasthttp,
// which has no HTTP/2 support. So the handshake agreed on a protocol the
// server could not speak: a client preferring h2 got
// "frame too large, note that the frame header looked like an HTTP/1.1
// header" and the page never loaded.
//
// The client here is the browser case - offers h2, handles either. It
// must come back 200 over HTTP/1.1.
func TestABrowserCanOpenTheConsoleWithACMEOn(t *testing.T) {
	const host = "mail.example.com"
	certPEM, keyPEM := selfSignedFixture(t, host)
	store := &recordingStore{rows: map[string]string{
		certmodel.ScopeACME + "/" + host: keyPEM + certPEM,
	}}

	b := &Builder{
		Store: store, Host: host,
		ACME:          staticACME(ACME{Enabled: true, Hosts: []string{host}}),
		ChallengeAddr: "127.0.0.1:0",
	}
	t.Cleanup(func() { _ = b.Shutdown(context.Background()) })

	cfg, err := b.Build("server", true)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if slices.Contains(cfg.NextProtos, "h2") {
		t.Error("the listener advertises h2, which fasthttp cannot speak")
	}

	// The ALPN challenge protocol is what lets the CA validate against
	// 443 with no port 80 anywhere. Dropping it fails the CA's handshake
	// with no_application_protocol.
	if !slices.Contains(cfg.NextProtos, acme.ALPNProto) {
		t.Error("the listener does not advertise acme-tls/1, so tls-alpn-01 validation cannot happen")
	}

	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error { return c.SendString("ok") })
	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	go func() { _ = app.Listener(ln) }()
	t.Cleanup(func() { _ = app.Shutdown() })

	client := &http.Client{Transport: &http.Transport{
		ForceAttemptHTTP2: true,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // a fixture certificate for one name
			ServerName:         host,
		},
	}}
	res, err := client.Get("https://" + ln.Addr().String() + "/")
	if err != nil {
		t.Fatalf("a browser-shaped client could not load the page: %v", err)
	}

	defer func() { _ = res.Body.Close() }()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || string(body) != "ok" {
		t.Errorf("got %d %q, want 200 ok", res.StatusCode, body)
	}
}

// staticACME is a settings provider that never changes, for the tests
// that are not about a change taking effect.
func staticACME(a ACME) func() ACME { return func() ACME { return a } }
