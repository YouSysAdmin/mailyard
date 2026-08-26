// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package certificate

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	tlsutils "github.com/yousysadmin/go-tlsutils"
	"github.com/yousysadmin/mailyard/internal/core/certstore"

	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	certmodel "github.com/yousysadmin/mailyard/internal/models/certificate"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)

	return NewStore(db, crypto.New("0123456789abcdef0123456789abcdef"))
}

// selfSigned mints a real certificate so the expiry parsing is
// exercised against x509 rather than a hand-written string.
func selfSigned(t *testing.T, cn string, notAfter time.Time) (certPEM, keyPEM string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
}

func TestPutAndGetRoundTrip(t *testing.T) {
	s := testStore(t)
	certPEM, keyPEM := selfSigned(t, "relay-ca", time.Now().Add(3650*24*time.Hour))

	if err := s.Put(t.Context(), &certmodel.Certificate{
		Scope: certmodel.ScopeRelayCA, Data: keyPEM, CertPEM: certPEM,
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.Get(t.Context(), certmodel.ScopeRelayCA, "")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got == nil {
		t.Fatal("Get returned nothing")
	}

	if got.Data != keyPEM {
		t.Error("the private half did not survive the round trip")
	}

	if got.CertPEM != certPEM {
		t.Error("the certificate did not survive the round trip")
	}
}

// The column holds ciphertext or it holds something wrong. Reading
// the raw value is the only way to prove sealing actually happened -
// Get decrypts, so it would look identical either way.
func TestThePrivateHalfIsSealedAtRest(t *testing.T) {
	s := testStore(t)
	_, keyPEM := selfSigned(t, "relay-ca", time.Now().Add(time.Hour))

	if err := s.Put(t.Context(), &certmodel.Certificate{
		Scope: certmodel.ScopeRelayCA, Data: keyPEM,
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	var stored string
	row := s.QueryRow(t.Context(), `SELECT data FROM certificates WHERE scope = ?`,
		certmodel.ScopeRelayCA)
	if err := row.Scan(&stored); err != nil {
		t.Fatalf("read raw: %v", err)
	}

	if strings.Contains(stored, "PRIVATE KEY") {
		t.Fatal("the private key is in the database in the clear")
	}

	if stored == keyPEM {
		t.Fatal("the stored value equals the plaintext")
	}
}

// Every listing path must leave the sealed column alone. This is what
// lets the console show certificates without the encryption key being
// anywhere near the request.
func TestListingNeverReturnsTheKey(t *testing.T) {
	s := testStore(t)
	certPEM, keyPEM := selfSigned(t, "node1.example.com", time.Now().Add(time.Hour))

	if err := s.Put(t.Context(), &certmodel.Certificate{
		Scope: certmodel.ScopeRelayNode, Name: "2cf116d8-4b45-45ad-800c-83a00e73d477", Data: keyPEM, CertPEM: certPEM,
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	list, err := s.ListScope(t.Context(), certmodel.ScopeRelayNode)
	if err != nil {
		t.Fatalf("ListScope: %v", err)
	}

	if len(list) != 1 {
		t.Fatalf("listed %d entries", len(list))
	}

	if list[0].Data != "" {
		t.Error("ListScope returned the sealed column")
	}

	if list[0].CertPEM != certPEM {
		t.Error("ListScope lost the public certificate")
	}
}

func TestNotAfterIsDerivedFromTheCertificate(t *testing.T) {
	s := testStore(t)
	want := time.Now().Add(72 * time.Hour).Truncate(time.Second)
	certPEM, keyPEM := selfSigned(t, "node1.example.com", want)

	if err := s.Put(t.Context(), &certmodel.Certificate{
		Scope: certmodel.ScopeRelayNode, Name: "2cf116d8-4b45-45ad-800c-83a00e73d477", Data: keyPEM, CertPEM: certPEM,
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.Get(t.Context(), certmodel.ScopeRelayNode, "2cf116d8-4b45-45ad-800c-83a00e73d477")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.NotAfter == nil {
		t.Fatal("no expiry was derived")
	}

	if diff := got.NotAfter.Sub(want); diff > time.Second || diff < -time.Second {
		t.Errorf("expiry is %v, want %v", got.NotAfter, want)
	}
}

// A chain is only usable while every certificate in it is valid, so
// the earliest expiry is the one that matters. Taking the leaf's
// would let an expired intermediate go unnoticed until handshakes
// started failing.
func TestTheEarliestExpiryInAChainWins(t *testing.T) {
	s := testStore(t)
	leaf, key := selfSigned(t, "leaf.example.com", time.Now().Add(90*24*time.Hour))
	soon := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	inter, _ := selfSigned(t, "intermediate.example.com", soon)

	if err := s.Put(t.Context(), &certmodel.Certificate{
		Scope: certmodel.ScopeACME, Name: "bundle", Data: key, CertPEM: leaf + inter,
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.Get(t.Context(), certmodel.ScopeACME, "bundle")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.NotAfter == nil {
		t.Fatal("no expiry was derived")
	}

	if diff := got.NotAfter.Sub(soon); diff > time.Second || diff < -time.Second {
		t.Errorf("expiry is %v, want the intermediate's %v", got.NotAfter, soon)
	}
}

// An autocert account key has no CERTIFICATE block at all. Refusing
// to store it would break the very cache this table exists for.
func TestAnOpaqueEntryStoresWithNoExpiry(t *testing.T) {
	s := testStore(t)
	if err := s.Put(t.Context(), &certmodel.Certificate{
		Scope: certmodel.ScopeACME, Name: "acme_account+key", Data: "opaque bytes",
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.Get(t.Context(), certmodel.ScopeACME, "acme_account+key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.NotAfter != nil {
		t.Errorf("an expiry was invented for an opaque entry: %v", got.NotAfter)
	}

	if got.Data != "opaque bytes" {
		t.Errorf("data is %q", got.Data)
	}
}

func TestPutReplacesInPlace(t *testing.T) {
	s := testStore(t)
	first, key1 := selfSigned(t, "node1.example.com", time.Now().Add(time.Hour))
	second, key2 := selfSigned(t, "node1.example.com", time.Now().Add(90*24*time.Hour))

	for _, c := range []*certmodel.Certificate{
		{Scope: certmodel.ScopeRelayNode, Name: "2cf116d8-4b45-45ad-800c-83a00e73d477", Data: key1, CertPEM: first},
		{Scope: certmodel.ScopeRelayNode, Name: "2cf116d8-4b45-45ad-800c-83a00e73d477", Data: key2, CertPEM: second},
	} {
		if err := s.Put(t.Context(), c); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}

	list, err := s.ListScope(t.Context(), certmodel.ScopeRelayNode)
	if err != nil {
		t.Fatalf("ListScope: %v", err)
	}

	if len(list) != 1 {
		t.Fatalf("renewal created %d rows, want 1", len(list))
	}

	got, err := s.Get(t.Context(), certmodel.ScopeRelayNode, "2cf116d8-4b45-45ad-800c-83a00e73d477")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.Data != key2 {
		t.Error("the replacement did not take")
	}
}

func TestGetMissingIsNilNil(t *testing.T) {
	s := testStore(t)
	got, err := s.Get(t.Context(), certmodel.ScopeRelayCA, "")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}

func TestExpiringBefore(t *testing.T) {
	s := testStore(t)
	soonPEM, key := selfSigned(t, "soon.example.com", time.Now().Add(12*time.Hour))
	laterPEM, _ := selfSigned(t, "later.example.com", time.Now().Add(120*24*time.Hour))

	for _, c := range []*certmodel.Certificate{
		{Scope: certmodel.ScopeRelayNode, Name: "soon", Data: key, CertPEM: soonPEM},
		{Scope: certmodel.ScopeRelayNode, Name: "later", Data: key, CertPEM: laterPEM},
		{Scope: certmodel.ScopeACME, Name: "opaque", Data: "no cert here"},
	} {
		if err := s.Put(t.Context(), c); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}

	got, err := s.ExpiringBefore(t.Context(), time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("ExpiringBefore: %v", err)
	}

	if len(got) != 1 || got[0].Name != "soon" {
		names := make([]string, len(got))
		for i, c := range got {
			names[i] = c.Name
		}

		t.Errorf("expiring set is %v, want [soon]", names)
	}
}

func TestDelete(t *testing.T) {
	s := testStore(t)
	certPEM, key := selfSigned(t, "node1.example.com", time.Now().Add(time.Hour))
	if err := s.Put(t.Context(), &certmodel.Certificate{
		Scope: certmodel.ScopeRelayNode, Name: "2cf116d8-4b45-45ad-800c-83a00e73d477", Data: key, CertPEM: certPEM,
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := s.Delete(t.Context(), certmodel.ScopeRelayNode, "2cf116d8-4b45-45ad-800c-83a00e73d477"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, err := s.Get(t.Context(), certmodel.ScopeRelayNode, "2cf116d8-4b45-45ad-800c-83a00e73d477")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got != nil {
		t.Error("the entry survived deletion")
	}
}

// autocert cache against the real table, in the shape autocert
// actually hands over: the private key first, then the chain.
//
// Everything else about the ACME path is proven with a fake store or
// a seeded row. This is the write - what a successful issuance leaves
// behind - and it is where the parts that are ours live: sealing the
// blob, splitting the chain into the readable column, and deriving the
// expiry the console and the sweep both read.
func TestAutocertCacheWritesTheRealRow(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := NewStore(db, crypto.New("test-key-at-least-32-characters-long"))
	cache := &certstore.Cache{Store: s}
	ctx := t.Context()

	certPEM, keyPEM, err := tlsutils.SelfSignedPEM(tlsutils.SelfSignedOptions{
		Hosts:     []string{"mail.example.com"},
		Algorithm: tlsutils.AlgECDSA,
	})
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}

	blob := append(append([]byte{}, keyPEM...), certPEM...)

	if err := cache.Put(ctx, "mail.example.com", blob); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// autocert reads it back byte for byte, or the certificate it
	// cached is not the one it serves.
	got, err := cache.Get(ctx, "mail.example.com")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if !bytes.Equal(got, blob) {
		t.Error("what came back is not what autocert stored")
	}

	// And the row carries what everything else reads: an expiry for
	// the sweep, a public half for the console, and no key in it.
	rec, err := s.Get(ctx, certmodel.ScopeACME, "mail.example.com")
	if err != nil || rec == nil {
		t.Fatalf("row: %v %v", rec, err)
	}

	if rec.NotAfter == nil {
		t.Error("not_after was not derived, so the expiry sweep cannot see this")
	}

	if !strings.Contains(rec.CertPEM, "BEGIN CERTIFICATE") {
		t.Error("cert_pem holds no certificate")
	}

	if strings.Contains(rec.CertPEM, "PRIVATE KEY") {
		t.Error("the readable column carries the private key")
	}

	// Sealed on disk: the raw column is not the blob.
	var stored string
	if err := db.QueryRowContext(ctx,
		`SELECT data FROM certificates WHERE scope = $1 AND name = $2`,
		certmodel.ScopeACME, "mail.example.com").Scan(&stored); err != nil {
		t.Fatalf("raw read: %v", err)
	}

	if strings.Contains(stored, "PRIVATE KEY") {
		t.Error("the private half is stored in the clear")
	}
}

// GetPublic is what serves an authority to the operator installing it,
// so the one thing it must never hand back is the private half. The
// sealed column is not merely left sealed - it is CLEARED, because a
// caller handed ciphertext in a field called Data is a caller that may
// pass it on believing it is the key.
func TestGetPublicReturnsNoKeyAtAll(t *testing.T) {
	s := testStore(t)
	certPEM, keyPEM := selfSigned(t, "root", time.Now().Add(90*24*time.Hour))
	if err := s.Put(t.Context(), &certmodel.Certificate{
		Scope: certmodel.ScopeManaged, Name: "root", Data: keyPEM + certPEM, CertPEM: certPEM,
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	pub, err := s.GetPublic(t.Context(), certmodel.ScopeManaged, "root")
	if err != nil {
		t.Fatalf("GetPublic: %v", err)
	}

	if pub == nil {
		t.Fatal("GetPublic found nothing")
	}

	if pub.Data != "" {
		t.Errorf("GetPublic returned the sealed column: %q", clip(pub.Data))
	}

	if pub.CertPEM != certPEM {
		t.Error("GetPublic did not return the public half unchanged")
	}

	if strings.Contains(pub.CertPEM, "PRIVATE KEY") {
		t.Error("the public half carries a private key")
	}

	// Get still decrypts, so the two are genuinely different calls and
	// not one aliased to the other.
	full, err := s.Get(t.Context(), certmodel.ScopeManaged, "root")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if !strings.Contains(full.Data, "PRIVATE KEY") {
		t.Error("Get no longer returns the private half")
	}
}

func TestGetPublicMissingIsNilNil(t *testing.T) {
	s := testStore(t)
	rec, err := s.GetPublic(t.Context(), certmodel.ScopeManaged, "nope")
	if err != nil || rec != nil {
		t.Errorf("GetPublic on a missing row = %v, %v, want nil, nil", rec, err)
	}
}

func clip(s string) string {
	if len(s) > 40 {
		return s[:40] + "..."
	}

	return s
}
