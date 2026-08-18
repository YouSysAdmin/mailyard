// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"io"
	"log/slog"
	"testing"

	"github.com/yousysadmin/mailyard/internal/domain/store"
	dmodel "github.com/yousysadmin/mailyard/internal/models/domain"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
)

func testKeyPEM(t *testing.T) string {
	t.Helper()
	// 1024 is far too short to send with and perfectly adequate to
	// prove which domain the signer was built for. Generating 2048 in
	// a unit test costs a noticeable fraction of a second.
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}

	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func signerProcessor(d *dmodel.Domain) *Processor {
	return &Processor{
		Store: &store.Store{Domain: &fakeDomains{verified: d}},
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// Mail from a subdomain is signed with the verified ancestor's key,
// so d= is the apex.
//
// That is not a compromise, it is the correct signature: DMARC
// relaxed alignment - the default - compares organizational domains,
// so d=managebac.com aligns with From: news@mail.managebac.com. The
// alternative, refusing to sign because no key exists at the exact
// name, would send subdomain mail unsigned, which is strictly worse.
func TestSubdomainMailIsSignedWithTheVerifiedAncestorsKey(t *testing.T) {
	d := &dmodel.Domain{
		ProjectID: "e66e7a4d-9e6c-4884-869a-cf9ffcf22181", Domain: "managebac.com", Verified: true,
		DKIMSelector: "mailyard", DKIMPrivateKey: testKeyPEM(t),
	}
	p := signerProcessor(d)

	signer, err := p.signerFor(t.Context(), &emailmodel.Email{
		ProjectID: "e66e7a4d-9e6c-4884-869a-cf9ffcf22181", Sender: "news@mail.managebac.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	if signer == nil {
		t.Fatal("subdomain mail was left unsigned, so it carries no DKIM at all")
	}

	if signer.Domain() != "managebac.com" {
		t.Errorf("signed with d=%s, want the verified ancestor managebac.com", signer.Domain())
	}
}

// The project comparison survives the change. Covering decides which
// domain row answers, never WHOSE it is.
func TestASubdomainOfAnotherProjectsDomainIsNotSigned(t *testing.T) {
	d := &dmodel.Domain{
		ProjectID: "6a5f0b90-6a56-47f4-8926-7cc56968798b", Domain: "managebac.com", Verified: true,
		DKIMSelector: "mailyard", DKIMPrivateKey: testKeyPEM(t),
	}
	p := signerProcessor(d)

	signer, err := p.signerFor(t.Context(), &emailmodel.Email{
		ProjectID: "e66e7a4d-9e6c-4884-869a-cf9ffcf22181", Sender: "news@mail.managebac.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	if signer != nil {
		t.Error("one project signed as a subdomain of another project's domain")
	}
}
