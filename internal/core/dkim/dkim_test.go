package dkim

import (
	"bytes"
	"strings"
	"testing"
)

// Round trip: sign a message and verify it against the public key we
// would have published, so a broken selector or canonicalization
// choice fails here rather than at a receiver.
func TestSignAndVerifyRoundTrip(t *testing.T) {
	priv, pub, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(TXTValue(pub), "v=DKIM1; k=rsa; p=") {
		t.Errorf("unexpected record value %q", TXTValue(pub))
	}

	if got := TXTHost("mailyard", "example.com"); got != "mailyard._domainkey.example.com" {
		t.Errorf("TXTHost = %q", got)
	}

	s, err := NewSigner("example.com", "mailyard", priv)
	if err != nil {
		t.Fatal(err)
	}

	raw := []byte("From: a@example.com\r\n" +
		"To: b@example.net\r\n" +
		"Subject: hi\r\n" +
		"Date: Mon, 01 Jan 2026 00:00:00 +0000\r\n" +
		"Message-ID: <x@example.com>\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=\"UTF-8\"\r\n" +
		"\r\nhello\r\n")

	signed, err := s.Sign(raw)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Contains(signed, []byte("DKIM-Signature:")) {
		t.Fatal("no DKIM-Signature header was added")
	}

	if !bytes.Contains(signed, []byte("hello")) {
		t.Error("body lost")
	}

	// Actually verify it, against the public key we would have
	// published. Asserting only that a header exists would pass even
	// with a broken canonicalization choice or a mismatched selector,
	// which is exactly the class of bug that shows up as silent
	// spam-foldering months later.
	lookup := func(domain string) ([]string, error) {
		if domain != "mailyard._domainkey.example.com" {
			t.Errorf("verifier looked up %q", domain)

			return nil, nil
		}

		return []string{TXTValue(pub)}, nil
	}
	results, err := Verify(bytes.NewReader(signed), lookup)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}

	if !results[0].Valid {
		t.Fatalf("signature did not verify: %s", results[0].Error)
	}

	if results[0].Domain != "example.com" {
		t.Errorf("domain = %q", results[0].Domain)
	}
}

// A message altered after signing must fail, or the signature is
// decorative.
func TestVerifyDetectsTampering(t *testing.T) {
	priv, pub, _ := GenerateKey()
	s, _ := NewSigner("example.com", "mailyard", priv)
	raw := []byte("From: a@example.com\r\nSubject: hi\r\n\r\nhello\r\n")
	signed, err := s.Sign(raw)
	if err != nil {
		t.Fatal(err)
	}

	tampered := bytes.Replace(signed, []byte("hello"), []byte("goodbye"), 1)

	results, err := Verify(bytes.NewReader(tampered),
		func(string) ([]string, error) { return []string{TXTValue(pub)}, nil })
	if err != nil {
		t.Fatalf("verify: %v", err)
	}

	if len(results) != 1 || results[0].Valid {
		t.Fatalf("tampered body verified as valid: %+v", results)
	}
}

func TestNewSignerRejectsGarbage(t *testing.T) {
	if _, err := NewSigner("example.com", "mailyard", "not a pem"); err == nil {
		t.Error("garbage key accepted")
	}

	priv, _, _ := GenerateKey()
	if _, err := NewSigner("", "mailyard", priv); err == nil {
		t.Error("empty domain accepted")
	}
}
