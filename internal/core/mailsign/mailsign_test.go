// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package mailsign

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/smallstep/pkcs7"
	"software.sslmate.com/src/go-pkcs12"

	"github.com/yousysadmin/mailyard/internal/core/certgen"
)

const entity = "Content-Type: text/plain; charset=\"UTF-8\"\r\n" +
	"Content-Transfer-Encoding: quoted-printable\r\n\r\n" +
	"hello, signed world\r\n"

// Generate, sign, verify against the public half, and describe: the
// whole PGP life of a key, checked by the library that will read it.
func TestPGPSignsAndVerifies(t *testing.T) {
	m, err := GeneratePGP("Billing (Acme)", "billing@example.com")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(m.Private, "-----BEGIN PGP PRIVATE KEY BLOCK-----") ||
		!strings.HasPrefix(m.Public, "-----BEGIN PGP PUBLIC KEY BLOCK-----") {
		t.Fatalf("material is not armored:\n%s\n%s", m.Private, m.Public)
	}

	s, err := New(m)
	if err != nil {
		t.Fatal(err)
	}

	if s.Protocol() != "application/pgp-signature" || s.MicAlg() != "pgp-sha256" {
		t.Errorf("protocol %q micalg %q", s.Protocol(), s.MicAlg())
	}

	part, err := s.Sign([]byte(entity))
	if err != nil {
		t.Fatal(err)
	}

	if part.TransferEncoding != "7bit" || !bytes.HasPrefix(part.Body, []byte("-----BEGIN PGP SIGNATURE-----")) {
		t.Fatalf("unexpected signature part: %+v", part)
	}

	ring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(m.Public))
	if err != nil {
		t.Fatal(err)
	}

	signer, err := openpgp.CheckArmoredDetachedSignature(ring, strings.NewReader(entity), bytes.NewReader(part.Body), nil)
	if err != nil {
		t.Fatalf("the signature does not verify: %v", err)
	}

	if _, err := openpgp.CheckArmoredDetachedSignature(ring, strings.NewReader(entity+"x"), bytes.NewReader(part.Body), nil); err == nil {
		t.Fatal("a changed entity still verified")
	}

	d, err := Describe(m)
	if err != nil {
		t.Fatal(err)
	}

	if d.Kind != KindPGP || d.Algorithm != "Ed25519" || len(d.Fingerprint) != 40 || d.NotAfter != nil {
		t.Errorf("description %+v", d)
	}

	if !strings.Contains(d.Subject, "billing@example.com") || strings.Contains(d.Subject, "(Acme)") {
		t.Errorf("subject %q: the name should be cleaned of brackets and carry the address", d.Subject)
	}

	if strings.ToUpper(signer.PrimaryKey.KeyIdString()) != d.Fingerprint[len(d.Fingerprint)-16:] {
		t.Errorf("the verifying key %s is not the described one %s", signer.PrimaryKey.KeyIdString(), d.Fingerprint)
	}

	keydata, err := AutocryptKeyData(m.Public)
	if err != nil {
		t.Fatal(err)
	}

	raw, err := base64.StdEncoding.DecodeString(keydata)
	if err != nil {
		t.Fatal(err)
	}

	minimal, err := openpgp.ReadKeyRing(bytes.NewReader(raw))
	if err != nil || len(minimal) != 1 {
		t.Fatalf("autocrypt keydata is not a key ring: %v", err)
	}

	if _, ok := minimal[0].EncryptionKey(time.Now()); !ok {
		t.Error("the autocrypt key carries no encryption subkey, a client could not encrypt back")
	}

	verifyWithGPG(t, m.Public, entity, part.Body)
}

// An imported key opens with its passphrase, is refused with another,
// and is refused however it opens when none of its ids is the sender.
func TestPGPImportChecksThePassphraseAndTheAddress(t *testing.T) {
	m, err := GeneratePGP("Billing", "billing@example.com")
	if err != nil {
		t.Fatal(err)
	}

	ring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(m.Private))
	if err != nil {
		t.Fatal(err)
	}

	if err := ring[0].EncryptPrivateKeys([]byte("hunter2"), nil); err != nil {
		t.Fatal(err)
	}

	var protected bytes.Buffer
	w, _ := armor.Encode(&protected, armorPrivate, nil)
	if err := ring[0].SerializePrivateWithoutSigning(w, nil); err != nil {
		t.Fatal(err)
	}

	_ = w.Close()

	if _, err := ImportPGP(protected.String(), "wrong", "billing@example.com"); !errors.Is(err, ErrPassphrase) {
		t.Errorf("wrong passphrase: %v", err)
	}

	if _, err := ImportPGP(protected.String(), "hunter2", "someone-else@example.com"); err == nil ||
		!strings.Contains(err.Error(), "no user id") {
		t.Errorf("another address: %v", err)
	}

	got, err := ImportPGP(protected.String(), "hunter2", "BILLING@example.com")
	if err != nil {
		t.Fatal(err)
	}

	// Stored unprotected: the caller seals it, and a passphrase would
	// have to be stored beside it to be of any use.
	if _, err := New(got); err != nil {
		t.Errorf("the imported key does not open without a passphrase: %v", err)
	}

	if _, err := ImportPGP(m.Public, "", "billing@example.com"); err == nil {
		t.Error("a public key was accepted as a private one")
	}
}

// The S/MIME life of a certificate: imported as PEM and as PKCS#12,
// signed, verified against the issuing authority by the library that
// will read it and by openssl.
func TestSMIMESignsAndVerifies(t *testing.T) {
	caPEM, leafPEM, keyPEM := mintMailCertificate(t, "billing@example.com", time.Now(), true)

	m, err := ImportSMIME(leafPEM+caPEM, keyPEM, "Billing@Example.com")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(m.Private, "-----BEGIN PRIVATE KEY-----") || strings.Count(m.Public, "BEGIN CERTIFICATE") != 2 {
		t.Fatalf("material: private %q, chain of %d", m.Private[:30], strings.Count(m.Public, "BEGIN CERTIFICATE"))
	}

	s, err := New(m)
	if err != nil {
		t.Fatal(err)
	}

	if s.Protocol() != "application/pkcs7-signature" || s.MicAlg() != "sha-256" {
		t.Errorf("protocol %q micalg %q", s.Protocol(), s.MicAlg())
	}

	part, err := s.Sign([]byte(entity))
	if err != nil {
		t.Fatal(err)
	}

	if part.TransferEncoding != "base64" || part.Filename != "smime.p7s" {
		t.Fatalf("unexpected signature part: %+v", part)
	}

	p7, err := pkcs7.Parse(part.Body)
	if err != nil {
		t.Fatal(err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(caPEM)) {
		t.Fatal("ca pem")
	}

	p7.Content = []byte(entity)
	if err := p7.VerifyWithChain(pool); err != nil {
		t.Fatalf("the signature does not verify: %v", err)
	}

	if len(p7.Certificates) != 2 {
		t.Errorf("the signature carries %d certificates, want the leaf and its issuer", len(p7.Certificates))
	}

	p7.Content = []byte(entity + "x")
	if err := p7.VerifyWithChain(pool); err == nil {
		t.Fatal("a changed entity still verified")
	}

	d, err := Describe(m)
	if err != nil {
		t.Fatal(err)
	}

	if d.Kind != KindSMIME || d.Algorithm != "ECDSA P-256" || len(d.Fingerprint) != 64 || d.NotAfter == nil ||
		!strings.Contains(d.Subject, "Billing") || !strings.Contains(d.Issuer, "Test CA") {
		t.Errorf("description %+v", d)
	}

	verifyWithOpenSSL(t, caPEM, entity, part.Body)

	// The same pair through PKCS#12, which is how a CA hands it over.
	leaf, key := parsePair(t, leafPEM, keyPEM)
	ca, _ := certgen.ParseCertificate(caPEM)
	der, err := pkcs12.Modern.Encode(key, leaf, []*x509.Certificate{ca}, "secret")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := ImportPKCS12(der, "nope", "billing@example.com"); !errors.Is(err, ErrPassphrase) {
		t.Errorf("wrong password: %v", err)
	}

	fromP12, err := ImportPKCS12(der, "secret", "billing@example.com")
	if err != nil {
		t.Fatal(err)
	}

	if fromP12.Public != m.Public {
		t.Error("the pkcs12 import stored a different chain than the PEM import")
	}
}

// Every refusal a mail client would make later is made at import.
func TestSMIMEImportRefusesWhatAClientWould(t *testing.T) {
	caPEM, leafPEM, keyPEM := mintMailCertificate(t, "billing@example.com", time.Now(), true)
	_, otherLeaf, otherKey := mintMailCertificate(t, "other@example.com", time.Now(), true)
	_, expiredLeaf, expiredKey := mintMailCertificate(t, "billing@example.com", time.Now().Add(-48*time.Hour), true)
	_, tlsLeaf, tlsKey := mintMailCertificate(t, "billing@example.com", time.Now(), false)
	caLeaf, caKey := mintMailCA(t, "billing@example.com")

	cases := []struct {
		name, cert, key, want string
	}{
		{"another address", otherLeaf, otherKey, "not issued to"},
		{"mismatched key", leafPEM, otherKey, "does not match"},
		{"expired", expiredLeaf, expiredKey, "expired"},
		{"no mail usage", tlsLeaf, tlsKey, "email protection"},
		{"a CA certificate", caLeaf, caKey, "certificate authority"},
		{"no certificate", keyPEM, keyPEM, "no certificate"},
		{"no key", leafPEM + caPEM, caPEM, "unsupported private key block"},
	}
	for _, c := range cases {
		_, err := ImportSMIME(c.cert, c.key, "billing@example.com")
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want an error naming %q", c.name, err, c.want)
		}
	}
}

// mintMailCertificate issues a leaf for the address under a fresh
// authority, valid for a day either side of now. mail=false leaves
// the email protection usage out, which is what a TLS certificate
// looks like.
func mintMailCertificate(t *testing.T, email string, now time.Time, mail bool) (caPEM, leafPEM, keyPEM string) {
	t.Helper()
	caPEM, caKeyPEM, err := certgen.MintCA(certgen.CARequest{
		Subject: certgen.Subject{CommonName: "Test CA"}, Algorithm: "ecdsa", Validity: 365 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}

	issuer, err := certgen.LoadIssuer(caPEM, caKeyPEM)
	if err != nil {
		t.Fatal(err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber:   big.NewInt(time.Now().UnixNano()),
		Subject:        pkix.Name{CommonName: "Billing"},
		EmailAddresses: []string{email},
		NotBefore:      now.Add(-24 * time.Hour),
		NotAfter:       now.Add(24 * time.Hour),
		KeyUsage:       x509.KeyUsageDigitalSignature,
		ExtKeyUsage:    []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if mail {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageEmailProtection}
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, issuer.Certificate, &key.PublicKey, issuer.Key)
	if err != nil {
		t.Fatal(err)
	}

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}

	leafPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))

	return caPEM, leafPEM, keyPEM
}

// mintMailCA is a self-signed authority that also names the address
// and carries the mail usage, which a client still refuses as a sender
// certificate.
func mintMailCA(t *testing.T, email string) (certPEM, keyPEM string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "Billing CA"},
		EmailAddresses:        []string{email},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageEmailProtection},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
}

func parsePair(t *testing.T, certPEM, keyPEM string) (*x509.Certificate, any) {
	t.Helper()
	leaf, err := certgen.ParseCertificate(certPEM)
	if err != nil {
		t.Fatal(err)
	}

	block, _ := pem.Decode([]byte(keyPEM))
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}

	return leaf, key
}

// verifyWithOpenSSL asks a second implementation, when one is
// installed. Two libraries agreeing with each other is not the same as
// a mail client agreeing with either.
func verifyWithOpenSSL(t *testing.T, caPEM, content string, sig []byte) {
	t.Helper()
	bin, err := exec.LookPath("openssl")
	if err != nil {
		t.Log("openssl not installed, skipping the second verification")

		return
	}

	dir := t.TempDir()
	files := map[string][]byte{"ca.pem": []byte(caPEM), "content": []byte(content), "sig.p7s": sig}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cmd := exec.Command(bin, "cms", "-verify", "-binary", "-inform", "DER",
		"-in", filepath.Join(dir, "sig.p7s"), "-content", filepath.Join(dir, "content"),
		"-CAfile", filepath.Join(dir, "ca.pem"), "-purpose", "smimesign", "-out", filepath.Join(dir, "out"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("openssl does not verify the signature: %v\n%s", err, out)
	}
}

// verifyWithGPG does the same for OpenPGP.
func verifyWithGPG(t *testing.T, pubArmored, content string, sig []byte) {
	t.Helper()
	bin, err := exec.LookPath("gpg")
	if err != nil {
		t.Log("gpg not installed, skipping the second verification")

		return
	}

	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}

	files := map[string][]byte{"pub.asc": []byte(pubArmored), "content": []byte(content), "sig.asc": sig}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	env := append(os.Environ(), "GNUPGHOME="+home)
	imp := exec.Command(bin, "--batch", "--import", filepath.Join(dir, "pub.asc"))
	imp.Env = env
	if out, err := imp.CombinedOutput(); err != nil {
		t.Fatalf("gpg import: %v\n%s", err, out)
	}

	ver := exec.Command(bin, "--batch", "--verify", filepath.Join(dir, "sig.asc"), filepath.Join(dir, "content"))
	ver.Env = env
	if out, err := ver.CombinedOutput(); err != nil {
		t.Fatalf("gpg does not verify the signature: %v\n%s", err, out)
	}
}
