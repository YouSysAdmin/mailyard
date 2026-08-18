// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package certificate_test

import (
	"strings"
	"testing"

	tlsutils "github.com/yousysadmin/go-tlsutils"

	certmodel "github.com/yousysadmin/mailyard/internal/models/certificate"
)

func pair(t *testing.T, hosts ...string) (certPEM, keyPEM string) {
	t.Helper()
	c, k, err := tlsutils.SelfSignedPEM(tlsutils.SelfSignedOptions{
		Hosts:     hosts,
		Algorithm: tlsutils.AlgECDSA,
	})
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}

	return string(c), string(k)
}

func TestParseDetailsReadsTheLeaf(t *testing.T) {
	certPEM, _ := pair(t, "mail.example.com", "alt.example.com")

	d, err := certmodel.ParseDetails(certPEM)
	if err != nil {
		t.Fatalf("ParseDetails: %v", err)
	}

	if len(d.DNSNames) != 2 || d.DNSNames[0] != "mail.example.com" {
		t.Errorf("DNSNames = %v", d.DNSNames)
	}

	if !d.SelfSigned {
		t.Error("a self-signed certificate did not report itself as one")
	}

	if d.Chain != 1 {
		t.Errorf("Chain = %d, want 1", d.Chain)
	}

	if !strings.HasPrefix(d.KeyAlgo, "ECDSA") {
		t.Errorf("KeyAlgo = %q", d.KeyAlgo)
	}

	// The form openssl prints, so the two can be compared by eye.
	if len(d.Fingerprint) != 95 || !strings.Contains(d.Fingerprint, ":") {
		t.Errorf("Fingerprint = %q", d.Fingerprint)
	}

	if d.ExpiresIn() <= 0 {
		t.Error("a fresh certificate reports as expired")
	}
}

func TestParseDetailsRejectsRubbish(t *testing.T) {
	for name, in := range map[string]string{
		"empty":       "",
		"not pem":     "hello",
		"key only":    mustKey(t),
		"broken der":  "-----BEGIN CERTIFICATE-----\nZm9v\n-----END CERTIFICATE-----\n",
		"pem no cert": "-----BEGIN NOTHING-----\nZm9v\n-----END NOTHING-----\n",
	} {
		if _, err := certmodel.ParseDetails(in); err == nil {
			t.Errorf("%s: parsed without error", name)
		}
	}
}

func mustKey(t *testing.T) string {
	t.Helper()
	_, k := pair(t, "x.test")

	return k
}

// A pair that does not match is a listener that comes up and then
// fails every handshake, with nothing in the upload to suggest why.
func TestVerifyPairCatchesAMismatch(t *testing.T) {
	certA, keyA := pair(t, "a.example.com")
	_, keyB := pair(t, "b.example.com")

	if err := certmodel.VerifyPair(certA, keyA); err != nil {
		t.Errorf("a matching pair was rejected: %v", err)
	}

	if err := certmodel.VerifyPair(certA, keyB); err == nil {
		t.Error("a key from another certificate was accepted")
	}

	if err := certmodel.VerifyPair(certA, "not a key"); err == nil {
		t.Error("rubbish was accepted as a key")
	}

	if err := certmodel.VerifyPair("not a certificate", keyA); err == nil {
		t.Error("rubbish was accepted as a certificate")
	}
}

// RSA is what most uploads are, so the parser has to handle both
// PKCS#8 and PKCS#1 - openssl writes one or the other depending on
// which command produced the key.
func TestVerifyPairAcceptsRSA(t *testing.T) {
	c, k, err := tlsutils.SelfSignedPEM(tlsutils.SelfSignedOptions{
		Hosts:     []string{"rsa.example.com"},
		Algorithm: tlsutils.AlgRSA,
	})
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}

	if err := certmodel.VerifyPair(string(c), string(k)); err != nil {
		t.Errorf("an RSA pair was rejected: %v", err)
	}

	d, err := certmodel.ParseDetails(string(c))
	if err != nil {
		t.Fatalf("ParseDetails: %v", err)
	}

	if !strings.HasPrefix(d.KeyAlgo, "RSA ") {
		t.Errorf("KeyAlgo = %q", d.KeyAlgo)
	}
}
