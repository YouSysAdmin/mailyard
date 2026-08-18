// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package mailauth

import (
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/dkim"
)

// signedMessage builds a real DKIM-signed message for domain, and the
// stub resolver that can verify it.
func signedMessage(t *testing.T, signDomain, fromHeader, body string) ([]byte, func(string) ([]string, error)) {
	t.Helper()
	priv, pub, err := dkim.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	s, err := dkim.NewSigner(signDomain, "mailyard", priv)
	if err != nil {
		t.Fatal(err)
	}

	raw := []byte("From: " + fromHeader + "\r\n" +
		"To: rcpt@receiver.example\r\n" +
		"Subject: test\r\n" +
		"\r\n" + body + "\r\n")
	signed, err := s.Sign(raw)
	if err != nil {
		t.Fatal(err)
	}

	lookup := func(name string) ([]string, error) {
		switch {
		case name == "mailyard._domainkey."+signDomain:
			return []string{dkim.TXTValue(pub)}, nil
		case strings.HasPrefix(name, "_dmarc."):
			return []string{"v=DMARC1; p=reject"}, nil
		}

		return nil, nil
	}

	return signed, lookup
}

// The case the whole feature exists for: a signature from the same
// domain as the From header means the message really is from them.
func TestAlignedDKIMPassesDMARC(t *testing.T) {
	raw, lookup := signedMessage(t, "sender.example", "boss@sender.example", "hello")

	res := Verify(t.Context(), Config{LookupTXT: lookup}, "203.0.113.9", "", "relay.example", raw)
	if res.DKIM != ResultPass {
		t.Errorf("dkim = %s, want pass (%+v)", res.DKIM, res.DKIMSigs)
	}

	if res.DMARC != ResultPass {
		t.Errorf("dmarc = %s, want pass", res.DMARC)
	}

	if !res.Aligned {
		t.Error("aligned = false, want true")
	}

	if res.Rejectable() {
		t.Error("an aligned message must never be rejectable")
	}
}

// The spoofing case. A valid signature from a domain the attacker does
// control says nothing about a From header naming somebody else, and
// treating "some signature verified" as success is exactly the hole
// DMARC alignment closes.
func TestUnalignedSignatureIsNotTrusted(t *testing.T) {
	raw, lookup := signedMessage(t, "attacker.example", "boss@victim.example", "wire me money")

	res := Verify(t.Context(), Config{LookupTXT: lookup}, "203.0.113.9", "", "relay.example", raw)
	if res.DKIM != ResultPass {
		t.Fatalf("the attacker's own signature should verify, got %s", res.DKIM)
	}

	if res.Aligned {
		t.Fatal("a signature from attacker.example must not authenticate From: victim.example")
	}

	if res.DMARC != ResultFail {
		t.Errorf("dmarc = %s, want fail", res.DMARC)
	}

	if !res.Rejectable() {
		t.Error("p=reject with no aligned identifier should be rejectable")
	}
}

// Relaxed alignment (the DMARC default) admits a subdomain.
func TestRelaxedAlignmentAdmitsSubdomain(t *testing.T) {
	raw, lookup := signedMessage(t, "mail.sender.example", "boss@sender.example", "hi")

	res := Verify(t.Context(), Config{LookupTXT: lookup}, "203.0.113.9", "", "relay.example", raw)
	if !res.Aligned {
		t.Error("mail.sender.example should align with sender.example under relaxed policy")
	}
}

// No DMARC record means no policy to honor, so nothing is rejectable
// however badly the message scores.
func TestNoDMARCRecordIsNeverRejectable(t *testing.T) {
	raw, _ := signedMessage(t, "attacker.example", "boss@victim.example", "hi")
	none := func(string) ([]string, error) { return nil, nil }

	res := Verify(t.Context(), Config{LookupTXT: none}, "203.0.113.9", "", "relay.example", raw)
	if res.DMARC != ResultNone {
		t.Errorf("dmarc = %s, want none", res.DMARC)
	}

	if res.Rejectable() {
		t.Error("no published policy must never produce a rejection")
	}
}

func TestAuthenticationResultsHeader(t *testing.T) {
	h := AuthenticationResults("mx.example", Result{
		SPF: ResultPass, SPFDomain: "sender.example",
		DKIM: ResultPass, DKIMSigs: []dkim.Result{{Domain: "sender.example", Valid: true}},
		DMARC: ResultPass,
	})
	for _, want := range []string{
		"mx.example", "spf=pass smtp.mailfrom=sender.example",
		"dkim=pass header.d=sender.example", "dmarc=pass",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("header %q missing %q", h, want)
		}
	}
}

func TestDomainOf(t *testing.T) {
	cases := map[string]string{
		"a@b.example":          "b.example",
		"Name <a@B.Example>":   "b.example",
		"  spaced@x.example  ": "x.example",
		"no-at-sign":           "",
		"trailing@":            "",
	}
	for in, want := range cases {
		if got := domainOf(in); got != want {
			t.Errorf("domainOf(%q) = %q, want %q", in, got, want)
		}
	}
}

// A second From header must not be able to change the domain the
// alignment check uses.
func TestHeaderAddressTakesTheFirstOccurrence(t *testing.T) {
	raw := []byte("From: real@first.example\r\nFrom: fake@second.example\r\n\r\nbody\r\n")
	if got := domainOf(headerAddress(raw, "From")); got != "first.example" {
		t.Errorf("got %q, want first.example", got)
	}
}
