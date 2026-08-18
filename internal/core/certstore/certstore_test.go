// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package certstore

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/acme/autocert"

	certmodel "github.com/yousysadmin/mailyard/internal/models/certificate"
)

// Guarded, because TestConcurrentCallersConvergeOnOneCertificate
// drives it from eight goroutines - the real store is a database and
// serializes for us, so a fake that races would be testing the fake.
type fakeStore struct {
	mu   sync.Mutex
	rows map[string]*certmodel.Certificate
	err  error
}

func newFake() *fakeStore {
	return &fakeStore{rows: map[string]*certmodel.Certificate{}}
}

// PutIfAbsent is the real semantics, not a wrapper over Put: the
// self-signed path depends on the loser of a race not overwriting the
// winner, so a fake that always writes would prove the opposite of
// what the test asks.
func (f *fakeStore) PutIfAbsent(_ context.Context, c *certmodel.Certificate) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return false, f.err
	}

	key := c.Scope + "/" + c.Name
	if _, taken := f.rows[key]; taken {
		return false, nil
	}

	cp := *c
	f.rows[key] = &cp

	return true, nil
}

func (f *fakeStore) Get(_ context.Context, scope, name string) (*certmodel.Certificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}

	return f.rows[scope+"/"+name], nil
}

func (f *fakeStore) Put(_ context.Context, c *certmodel.Certificate) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}

	f.rows[c.Scope+"/"+c.Name] = c

	return nil
}

func (f *fakeStore) Delete(_ context.Context, scope, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}

	delete(f.rows, scope+"/"+name)

	return nil
}

// A real autocert entry: the private key, then the chain, all PEM in
// one blob.
const acmeBlob = `-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIBSECRETKEYMATERIALDONOTPUBLISHoAoGCCqGSM49AwEHoUQDQgAE
-----END EC PRIVATE KEY-----
-----BEGIN CERTIFICATE-----
MIIBkTCCATegAwIBAgIQLEAF3GkOWSBTLEAFCERTIFICATEBYTESHEREaaaaaaaa
-----END CERTIFICATE-----
-----BEGIN CERTIFICATE-----
MIIBkTCCATegAwIBAgIQINTERMEDIATECERTIFICATEBYTESGOHEREbbbbbbbbbb
-----END CERTIFICATE-----
`

func TestRoundTrip(t *testing.T) {
	c := &Cache{Store: newFake()}
	if err := c.Put(t.Context(), "mail.example.com", []byte(acmeBlob)); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := c.Get(t.Context(), "mail.example.com")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if string(got) != acmeBlob {
		t.Error("the blob came back different from what autocert stored")
	}
}

// autocert gives up on a host entirely for any error that is not this
// exact sentinel, so a miss has to be it and nothing else.
func TestAMissIsTheSentinel(t *testing.T) {
	c := &Cache{Store: newFake()}
	_, err := c.Get(t.Context(), "never-stored.example.com")
	if !errors.Is(err, autocert.ErrCacheMiss) {
		t.Fatalf("err is %v, want autocert.ErrCacheMiss", err)
	}

	if !IsMiss(err) {
		t.Error("IsMiss disagreed with errors.Is")
	}
}

// The inverse, and the one that costs money. A database blip reported
// as a miss makes autocert order a certificate that already exists,
// and five of those in a week is the duplicate limit.
func TestADatabaseFailureIsNotAMiss(t *testing.T) {
	f := newFake()
	f.err = errors.New("connection refused")
	c := &Cache{Store: f}

	_, err := c.Get(t.Context(), "mail.example.com")
	if err == nil {
		t.Fatal("a store failure was swallowed")
	}

	if errors.Is(err, autocert.ErrCacheMiss) {
		t.Error("a store failure was reported as a cache miss, which re-orders a live certificate")
	}
}

// The public column is readable without the encryption key. Copying
// the blob into it wholesale would publish the private key to every
// console reader.
func TestThePublicColumnHoldsNoPrivateKey(t *testing.T) {
	f := newFake()
	c := &Cache{Store: f}
	if err := c.Put(t.Context(), "mail.example.com", []byte(acmeBlob)); err != nil {
		t.Fatalf("Put: %v", err)
	}

	rec := f.rows[certmodel.ScopeACME+"/mail.example.com"]
	if rec == nil {
		t.Fatal("nothing was stored")
	}

	if strings.Contains(rec.CertPEM, "PRIVATE KEY") {
		t.Error("the private key was copied into the unsealed column")
	}

	if strings.Contains(rec.CertPEM, "SECRETKEYMATERIAL") {
		t.Error("private key bytes reached the unsealed column")
	}

	if n := strings.Count(rec.CertPEM, "BEGIN CERTIFICATE"); n != 2 {
		t.Errorf("the public column has %d certificates, want the whole chain (2)", n)
	}

	if rec.Data != acmeBlob {
		t.Error("the sealed column lost part of the blob")
	}
}

// An entry stored with no key half is a miss, not an empty success.
// autocert would take empty bytes as a certificate and fail to parse
// it on every handshake.
func TestAnEmptyEntryIsAMiss(t *testing.T) {
	f := newFake()
	f.rows[certmodel.ScopeACME+"/mail.example.com"] = &certmodel.Certificate{
		Scope: certmodel.ScopeACME, Name: "mail.example.com", Data: "",
	}
	c := &Cache{Store: f}

	if _, err := c.Get(t.Context(), "mail.example.com"); !errors.Is(err, autocert.ErrCacheMiss) {
		t.Errorf("err is %v, want a miss", err)
	}
}

func TestDelete(t *testing.T) {
	f := newFake()
	c := &Cache{Store: f}
	if err := c.Put(t.Context(), "mail.example.com", []byte(acmeBlob)); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := c.Delete(t.Context(), "mail.example.com"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := c.Get(t.Context(), "mail.example.com"); !errors.Is(err, autocert.ErrCacheMiss) {
		t.Errorf("the entry survived deletion: %v", err)
	}
}

// autocert stores more than certificates under this cache - the
// account key among them - and those entries have no CERTIFICATE
// block at all. Storing them must still work.
func TestAnEntryWithNoCertificateStillStores(t *testing.T) {
	f := newFake()
	c := &Cache{Store: f}
	const accountKey = "-----BEGIN EC PRIVATE KEY-----\nMHcCAQEE\n-----END EC PRIVATE KEY-----\n"

	if err := c.Put(t.Context(), "acme_account+key", []byte(accountKey)); err != nil {
		t.Fatalf("Put: %v", err)
	}

	rec := f.rows[certmodel.ScopeACME+"/acme_account+key"]
	if rec.CertPEM != "" {
		t.Errorf("public column is %q, want empty", rec.CertPEM)
	}

	got, err := c.Get(t.Context(), "acme_account+key")
	if err != nil || string(got) != accountKey {
		t.Errorf("account key round trip failed: %v", err)
	}
}
