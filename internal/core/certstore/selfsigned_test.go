// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package certstore

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"slices"
	"strings"
	"sync"
	"testing"

	certmodel "github.com/yousysadmin/mailyard/internal/models/certificate"
)

func fingerprint(t *testing.T, c tls.Certificate) string {
	t.Helper()
	if len(c.Certificate) == 0 {
		t.Fatal("certificate has no chain")
	}

	leaf, err := x509.ParseCertificate(c.Certificate[0])
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}

	return string(leaf.Signature)
}

// The whole point: a second caller gets the same certificate, not
// another one. On separate nodes that is the difference between one
// identity and N, and a client pinning a self-signed fingerprint
// cannot tell N identities from an interception.
func TestSelfSignedIsGeneratedOnceAndReused(t *testing.T) {
	store := newFake()
	ctx := context.Background()
	hosts := []string{"mail.example.com", "localhost"}

	first, err := SelfSigned(ctx, store, hosts, "")
	if err != nil {
		t.Fatalf("first: %v", err)
	}

	second, err := SelfSigned(ctx, store, hosts, "")
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	if fingerprint(t, first) != fingerprint(t, second) {
		t.Error("a second call minted a different certificate")
	}

	// And it went to the table, with the public half readable so the
	// console can show an expiry without the encryption key.
	rec, err := store.Get(ctx, certmodel.ScopeSelfSigned, selfSignedName(hosts, ""))
	if err != nil || rec == nil {
		t.Fatalf("stored entry: %v %v", rec, err)
	}

	if rec.CertPEM == "" {
		t.Error("cert_pem is empty, so nothing can display this without decrypting it")
	}

	if rec.Data == rec.CertPEM {
		t.Error("data holds no private half")
	}
}

// Two nodes booting together both generate. Only one may store, and
// the loser must adopt the winner's rather than overwrite it.
func TestConcurrentCallersConvergeOnOneCertificate(t *testing.T) {
	store := newFake()
	hosts := []string{"mail.example.com"}

	const n = 8
	var mu sync.Mutex
	prints := map[string]int{}

	var wg sync.WaitGroup
	var barrier sync.WaitGroup
	barrier.Add(1)
	for range n {
		wg.Go(func() {
			barrier.Wait()
			cert, err := SelfSigned(context.Background(), store, hosts, "")
			if err != nil {
				t.Errorf("SelfSigned: %v", err)

				return
			}

			mu.Lock()
			prints[fingerprint(t, cert)]++
			mu.Unlock()
		})
	}

	barrier.Done()
	wg.Wait()

	if len(prints) != 1 {
		t.Errorf("%d callers ended up with %d different certificates, want 1", n, len(prints))
	}
}

// Changing the host set has to mint a new pair. Serving the old one
// would present a certificate that does not name the host, which
// every client rejects and no log explains.
func TestADifferentHostSetGetsItsOwnCertificate(t *testing.T) {
	store := newFake()
	ctx := context.Background()

	a, err := SelfSigned(ctx, store, []string{"one.example.com"}, "")
	if err != nil {
		t.Fatalf("a: %v", err)
	}

	b, err := SelfSigned(ctx, store, []string{"two.example.com"}, "")
	if err != nil {
		t.Fatalf("b: %v", err)
	}

	if fingerprint(t, a) == fingerprint(t, b) {
		t.Error("two host sets share one certificate")
	}
}

// Replace is how an operator forces a new pair without going near the
// table.
func TestReplaceMintsANewPair(t *testing.T) {
	store := newFake()
	ctx := context.Background()
	hosts := []string{"mail.example.com"}

	before, err := SelfSigned(ctx, store, hosts, "")
	if err != nil {
		t.Fatalf("before: %v", err)
	}

	after, err := Replace(ctx, store, hosts, "")
	if err != nil {
		t.Fatalf("replace: %v", err)
	}

	if fingerprint(t, before) == fingerprint(t, after) {
		t.Error("Replace returned the old certificate")
	}

	// And the new one is what a later caller now gets.
	next, err := SelfSigned(ctx, store, hosts, "")
	if err != nil {
		t.Fatalf("next: %v", err)
	}

	if fingerprint(t, next) != fingerprint(t, after) {
		t.Error("the replacement was not the one stored")
	}
}

// The generator behind this changed from go-tlsutils to
// internal/core/certgen. What an installation SERVES must not have
// changed with it: an empty algorithm still means RSA-2048 for 180
// days with ServerAuth, because that is what every existing
// deployment already has in this row and what its clients have
// already seen.
//
// The one thing that differs is the Subject, which would otherwise be
// "tlsutils self-signed" - a third-party library's name in the
// certificate this product serves.
func TestReplacingTheGeneratorKeptTheDefaults(t *testing.T) {
	store := newFake()
	cert, err := SelfSigned(t.Context(), store, []string{"mail.example.com", "localhost"}, "")
	if err != nil {
		t.Fatalf("SelfSigned: %v", err)
	}

	leaf := cert.Leaf
	if leaf == nil {
		var perr error
		if leaf, perr = x509.ParseCertificate(cert.Certificate[0]); perr != nil {
			t.Fatalf("parse leaf: %v", perr)
		}
	}

	key, ok := leaf.PublicKey.(*rsa.PublicKey)
	if !ok {
		t.Fatalf("an empty algorithm produced %T, want rsa - existing rows are rsa", leaf.PublicKey)
	}

	if key.N.BitLen() != 2048 {
		t.Errorf("rsa key is %d bits, want 2048", key.N.BitLen())
	}

	if days := leaf.NotAfter.Sub(leaf.NotBefore).Hours() / 24; days < 180 || days > 181 {
		t.Errorf("validity is %.1f days, want 180", days)
	}

	if !slices.Contains(leaf.ExtKeyUsage, x509.ExtKeyUsageServerAuth) {
		t.Errorf("extended key usage = %v, want ServerAuth", leaf.ExtKeyUsage)
	}

	if !slices.Contains(leaf.DNSNames, "mail.example.com") || !slices.Contains(leaf.DNSNames, "localhost") {
		t.Errorf("dns names = %v, want both hosts", leaf.DNSNames)
	}

	if strings.Contains(strings.ToLower(leaf.Subject.String()), "tlsutils") {
		t.Errorf("the subject still names the library: %s", leaf.Subject.String())
	}

	if leaf.Subject.CommonName != "mail.example.com" {
		t.Errorf("common name = %q, want the first host", leaf.Subject.CommonName)
	}
}
