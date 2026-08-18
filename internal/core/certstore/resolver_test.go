// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package certstore

import (
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"testing"
	"time"

	tlsutils "github.com/yousysadmin/go-tlsutils"

	"github.com/yousysadmin/mailyard/internal/core/certgen"
	certmodel "github.com/yousysadmin/mailyard/internal/models/certificate"
)

// Nothing assigned means every handshake reaches what the mode built.
//
// That is what makes ACME renewal need no listener restart: autocert
// updates its own state when it renews, and the next handshake gets
// the new certificate because the config asks it each time. A wrapper
// that answered from a cache of its own would hold the expired one
// until the cache turned over.
func TestAnUnassignedListenerAsksEveryTime(t *testing.T) {
	calls := 0
	base := &tls.Config{
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			calls++

			return &tls.Certificate{}, nil
		},
	}
	r := &Resolver{Store: newFake(), Assigned: func() string { return "" }}
	get := r.Wrap(base)

	for range 5 {
		if _, err := get(&tls.ClientHelloInfo{ServerName: "mail.example.com"}); err != nil {
			t.Fatalf("GetCertificate: %v", err)
		}
	}

	if calls != 5 {
		t.Errorf("the underlying config was asked %d times for 5 handshakes", calls)
	}
}

// An ASSIGNED certificate is cached, deliberately - it is a database
// read on a path that runs per connection. The TTL is what bounds how
// stale it can be, and it is short enough that a replacement lands
// while somebody is still looking at the page.
func TestAnAssignedCertificateIsCachedForItsTTL(t *testing.T) {
	store := newFake()
	certPEM, keyPEM := managedFixture(t)
	if _, err := store.PutIfAbsent(t.Context(), managedRecord("web", certPEM, keyPEM)); err != nil {
		t.Fatalf("seed: %v", err)
	}

	r := &Resolver{
		Store:    store,
		Assigned: func() string { return "web" },
		TTL:      time.Minute,
	}
	get := r.Wrap(&tls.Config{})

	before := len(store.rows)
	for range 5 {
		if _, err := get(&tls.ClientHelloInfo{}); err != nil {
			t.Fatalf("GetCertificate: %v", err)
		}
	}

	if len(store.rows) != before {
		t.Error("the store changed under a read")
	}

	// A changed assignment invalidates immediately rather than at the
	// end of the TTL - waiting would serve the old certificate after
	// an admin had already switched away from it.
	r.Assigned = func() string { return "" }
	if cert := r.assigned(); cert != nil {
		t.Error("unassigning kept serving the cached certificate")
	}
}

func managedFixture(t *testing.T) (certPEM, keyPEM string) {
	t.Helper()
	c, k, err := tlsutils.SelfSignedPEM(tlsutils.SelfSignedOptions{
		Hosts:     []string{"web.example.com"},
		Algorithm: tlsutils.AlgECDSA,
	})
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}

	return string(c), string(k)
}

func managedRecord(name, certPEM, keyPEM string) *certmodel.Certificate {
	return &certmodel.Certificate{
		Scope:   certmodel.ScopeManaged,
		Name:    name,
		Data:    keyPEM + certPEM,
		CertPEM: certPEM,
	}
}

// An AUTHORITY assigned to a listener, which happens when somebody
// edits the row or the setting directly - the settings handler refuses
// it and the console does not offer it, and neither of those reaches
// the database.
//
// It has to fall back rather than serve: a CA carries no subject alt
// name and no ServerAuth, so serving it fails every client, where the
// configured certificate works. And it has to be audible, or the
// console shows one certificate while the wire carries another.
func TestAnAssignedAuthorityIsNotServed(t *testing.T) {
	caPEM, caKey, err := certgen.MintCA(certgen.CARequest{
		Subject:  certgen.Subject{CommonName: "Acme Internal CA"},
		Validity: 3650 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("MintCA: %v", err)
	}

	store := newFake()
	if err := store.Put(t.Context(), managedRecord("root", caPEM, caKey)); err != nil {
		t.Fatalf("Put: %v", err)
	}

	var warned int
	r := &Resolver{
		Store:    store,
		Assigned: func() string { return "root" },
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	// Count the warnings by watching the handler, since the point is
	// that this is not silent.
	r.Log = slog.New(countingHandler{n: &warned})

	if cert := r.assigned(); cert != nil {
		t.Fatal("a certificate authority was served to a listener")
	}

	if warned == 0 {
		t.Error("falling back was silent - the console would show one certificate and the wire carry another")
	}

	// And the ordinary case still works, so the check is not simply
	// refusing everything.
	leaf, leafKey := managedFixture(t)
	if err := store.Put(t.Context(), managedRecord("edge", leaf, leafKey)); err != nil {
		t.Fatalf("Put: %v", err)
	}

	r.Assigned = func() string { return "edge" }
	if cert := r.assigned(); cert == nil {
		t.Error("an ordinary certificate was refused")
	}
}

// countingHandler counts records without formatting them.
type countingHandler struct{ n *int }

func (h countingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h countingHandler) Handle(context.Context, slog.Record) error {
	*h.n++

	return nil
}
func (h countingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h countingHandler) WithGroup(string) slog.Handler      { return h }
