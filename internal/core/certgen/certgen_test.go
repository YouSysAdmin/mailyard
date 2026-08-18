// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package certgen

import (
	"crypto/tls"
	"crypto/x509"
	"strings"
	"testing"
	"time"
)

const day = 24 * time.Hour

func mintCA(t *testing.T, cn string, validity time.Duration) (certPEM, keyPEM string) {
	t.Helper()
	certPEM, keyPEM, err := MintCA(CARequest{
		Subject:  Subject{CommonName: cn, Organization: "Acme Ltd"},
		Validity: validity,
	})
	if err != nil {
		t.Fatalf("MintCA: %v", err)
	}

	return certPEM, keyPEM
}

// The one test that proves what this feature is for: an operator puts
// one root into their clients' trust stores and every listener
// certificate signed by it verifies. Everything else here is a detail
// of how the bytes are shaped - this is the property.
func TestALeafVerifiesAgainstItsAuthority(t *testing.T) {
	caCert, caKey := mintCA(t, "Acme Internal CA", 3650*day)
	issuer, err := LoadIssuer(caCert, caKey)
	if err != nil {
		t.Fatalf("LoadIssuer: %v", err)
	}

	leafPEM, _, err := MintLeaf(LeafRequest{
		Hosts:     []string{"mail.internal", "10.0.0.7"},
		Algorithm: AlgECDSA,
		Validity:  365 * day,
	}, issuer)
	if err != nil {
		t.Fatalf("MintLeaf: %v", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(caCert)) {
		t.Fatal("the authority did not go into a cert pool")
	}

	leaf, err := ParseCertificate(leafPEM)
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}

	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:     pool,
		DNSName:   "mail.internal",
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		t.Fatalf("a leaf did not verify against the authority that signed it: %v", err)
	}

	// And the name it was not issued for must fail, or the test above
	// would pass for a certificate covering everything.
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots: pool, DNSName: "other.internal",
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err == nil {
		t.Error("the leaf verified for a name it does not carry")
	}
}

// A self-signed leaf must not verify against an unrelated authority,
// which is the negative half of the above.
func TestASelfSignedLeafIsNotSignedByTheAuthority(t *testing.T) {
	caCert, _ := mintCA(t, "Acme Internal CA", 3650*day)
	leafPEM, _, err := MintLeaf(LeafRequest{Hosts: []string{"mail.internal"}, Validity: 90 * day}, nil)
	if err != nil {
		t.Fatalf("MintLeaf: %v", err)
	}

	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM([]byte(caCert))
	leaf, err := ParseCertificate(leafPEM)
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}

	if _, err := leaf.Verify(x509.VerifyOptions{Roots: pool, DNSName: "mail.internal"}); err == nil {
		t.Error("a self-signed leaf verified against an authority that never signed it")
	}
}

// x509.CreateCertificate mints a leaf outliving its authority without
// a word, and the certificate then stops verifying on a date nothing
// warns about - with an error naming the leaf, which is the one
// certificate that is still fine.
func TestALeafIsClampedToItsAuthority(t *testing.T) {
	caCert, caKey := mintCA(t, "Short Lived CA", 30*day)
	issuer, err := LoadIssuer(caCert, caKey)
	if err != nil {
		t.Fatalf("LoadIssuer: %v", err)
	}

	leafPEM, _, err := MintLeaf(LeafRequest{
		Hosts: []string{"mail.internal"}, Validity: 365 * day,
	}, issuer)
	if err != nil {
		t.Fatalf("MintLeaf: %v", err)
	}

	leaf, err := ParseCertificate(leafPEM)
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}

	if leaf.NotAfter.After(issuer.Certificate.NotAfter) {
		t.Errorf("leaf expires %s, after its authority at %s",
			leaf.NotAfter.Format(time.RFC3339), issuer.Certificate.NotAfter.Format(time.RFC3339))
	}
}

// A CA is a trust anchor, not something that serves a name. Serving
// one to a client fails every handshake, so the shape is what stops it
// being mistaken for a server certificate.
func TestAnAuthorityIsShapedLikeAnAuthority(t *testing.T) {
	certPEM, _ := mintCA(t, "Acme Internal CA", 3650*day)
	ca, err := ParseCertificate(certPEM)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if !ca.IsCA {
		t.Error("IsCA is not set")
	}

	if !ca.MaxPathLenZero || ca.MaxPathLen != 0 {
		t.Error("an intermediate can exist under this authority")
	}

	if ca.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Error("the authority cannot sign certificates")
	}

	if len(ca.ExtKeyUsage) != 0 {
		t.Errorf("the authority carries extended key usages %v", ca.ExtKeyUsage)
	}

	if len(ca.DNSNames) != 0 || len(ca.IPAddresses) != 0 {
		t.Errorf("the authority carries subject alt names %v %v", ca.DNSNames, ca.IPAddresses)
	}

	if !ca.BasicConstraintsValid {
		t.Error("basic constraints are not marked valid, so IsCA is not honoured")
	}
}

// Several OS trust stores refuse to install an Ed25519 root. It is
// legal x509 and Go is perfectly happy with it, which is why this has
// to be refused deliberately rather than left to fail somewhere else.
func TestAnEd25519AuthorityIsRefused(t *testing.T) {
	_, _, err := MintCA(CARequest{
		Subject:   Subject{CommonName: "Acme"},
		Algorithm: AlgEd25519,
		Validity:  365 * day,
	})
	if err == nil {
		t.Fatal("an ed25519 authority was minted")
	}

	if !strings.Contains(err.Error(), "ed25519") {
		t.Errorf("the refusal does not say why: %v", err)
	}
}

// The reported bug, pinned. The library default put its own name in
// the Subject of every certificate this product generated.
func TestNoMintedSubjectNamesTheLibrary(t *testing.T) {
	caCert, caKey := mintCA(t, "Acme Internal CA", 3650*day)
	issuer, err := LoadIssuer(caCert, caKey)
	if err != nil {
		t.Fatalf("LoadIssuer: %v", err)
	}

	selfSigned, _, err := MintLeaf(LeafRequest{Hosts: []string{"mail.internal"}, Validity: 90 * day}, nil)
	if err != nil {
		t.Fatalf("MintLeaf self-signed: %v", err)
	}

	signed, _, err := MintLeaf(LeafRequest{Hosts: []string{"MX.Internal."}, Validity: 90 * day}, issuer)
	if err != nil {
		t.Fatalf("MintLeaf signed: %v", err)
	}

	for name, pemStr := range map[string]string{
		"authority": caCert, "self-signed leaf": selfSigned, "signed leaf": signed,
	} {
		crt, err := ParseCertificate(pemStr)
		if err != nil {
			t.Fatalf("%s: parse: %v", name, err)
		}

		if strings.Contains(strings.ToLower(crt.Subject.String()), "tlsutils") {
			t.Errorf("%s subject still names the library: %s", name, crt.Subject.String())
		}
	}

	// With no common name given, a leaf takes its FIRST HOST -
	// normalized, because that is the form a verifier compares.
	crt, err := ParseCertificate(signed)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if crt.Subject.CommonName != "mx.internal" {
		t.Errorf("common name = %q, want the first host", crt.Subject.CommonName)
	}
}

// Every field the form offers has to reach the certificate, or the
// form is decoration.
func TestTheSubjectReachesTheCertificate(t *testing.T) {
	certPEM, _, err := MintLeaf(LeafRequest{
		Subject: Subject{
			CommonName: "mail.internal", Organization: "Acme Ltd", Unit: "Infrastructure",
			Country: "ua", State: "Kyiv", Locality: "Kyiv",
		},
		Hosts: []string{"mail.internal"}, Validity: 90 * day,
	}, nil)
	if err != nil {
		t.Fatalf("MintLeaf: %v", err)
	}

	crt, err := ParseCertificate(certPEM)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	s := crt.Subject
	switch {
	case s.CommonName != "mail.internal":
		t.Errorf("common name = %q", s.CommonName)
	case len(s.Organization) != 1 || s.Organization[0] != "Acme Ltd":
		t.Errorf("organization = %v", s.Organization)
	case len(s.OrganizationalUnit) != 1 || s.OrganizationalUnit[0] != "Infrastructure":
		t.Errorf("unit = %v", s.OrganizationalUnit)
	// Uppercased: a country is a two-letter code and verifiers that
	// look at it expect it that way.
	case len(s.Country) != 1 || s.Country[0] != "UA":
		t.Errorf("country = %v", s.Country)
	case len(s.Province) != 1 || s.Province[0] != "Kyiv":
		t.Errorf("state = %v", s.Province)
	case len(s.Locality) != 1 || s.Locality[0] != "Kyiv":
		t.Errorf("locality = %v", s.Locality)
	}
}

// An empty string appended to a pkix.Name slice encodes as a
// present-but-empty attribute rather than an absent one, which some
// verifiers reject and which is a lie in any case.
func TestAnUnsetSubjectFieldIsAbsentNotEmpty(t *testing.T) {
	certPEM, _, err := MintLeaf(LeafRequest{Hosts: []string{"mail.internal"}, Validity: 90 * day}, nil)
	if err != nil {
		t.Fatalf("MintLeaf: %v", err)
	}

	crt, err := ParseCertificate(certPEM)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	s := crt.Subject
	if len(s.Organization)+len(s.OrganizationalUnit)+len(s.Country)+len(s.Province)+len(s.Locality) != 0 {
		t.Errorf("unset fields were encoded anyway: %+v", s)
	}
}

func TestEveryAlgorithmLoadsAsAPair(t *testing.T) {
	for _, alg := range []string{AlgRSA, AlgECDSA, AlgEd25519} {
		certPEM, keyPEM, err := MintLeaf(LeafRequest{
			Hosts: []string{"mail.internal"}, Algorithm: alg, Validity: 90 * day,
		}, nil)
		if err != nil {
			t.Fatalf("%s: MintLeaf: %v", alg, err)
		}

		if _, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM)); err != nil {
			t.Errorf("%s: the pair does not load: %v", alg, err)
		}
	}
}

// RSA key exchange needs KeyEncipherment and a signing-only key does
// not. Asserting it anyway is meaningless, which is what relayca did
// for its ECDSA leaves before this was shared.
func TestKeyUsageFollowsTheKeyType(t *testing.T) {
	for alg, wantEncipherment := range map[string]bool{AlgRSA: true, AlgECDSA: false, AlgEd25519: false} {
		certPEM, _, err := MintLeaf(LeafRequest{
			Hosts: []string{"mail.internal"}, Algorithm: alg, Validity: 90 * day,
		}, nil)
		if err != nil {
			t.Fatalf("%s: MintLeaf: %v", alg, err)
		}

		crt, err := ParseCertificate(certPEM)
		if err != nil {
			t.Fatalf("%s: parse: %v", alg, err)
		}

		if crt.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
			t.Errorf("%s: no digital signature usage", alg)
		}

		got := crt.KeyUsage&x509.KeyUsageKeyEncipherment != 0
		if got != wantEncipherment {
			t.Errorf("%s: key encipherment = %v, want %v", alg, got, wantEncipherment)
		}
	}
}

// A certificate with no subject alt name matches nothing anywhere: Go
// has ignored CommonName for hostname matching since 1.15.
func TestALeafWithNoHostIsRefused(t *testing.T) {
	if _, _, err := MintLeaf(LeafRequest{
		Subject: Subject{CommonName: "mail.internal"}, Validity: 90 * day,
	}, nil); err == nil {
		t.Error("a leaf with no host was minted")
	}

	if _, _, err := MintLeaf(LeafRequest{
		Hosts: []string{"  ", ""}, Validity: 90 * day,
	}, nil); err == nil {
		t.Error("a leaf whose only hosts were blank was minted")
	}
}

// An address in DNSNames is an invalid SAN most verifiers reject, so a
// listener reachable only by address would be unreachable.
func TestAnAddressBecomesAnIPSAN(t *testing.T) {
	certPEM, _, err := MintLeaf(LeafRequest{
		Hosts: []string{"Mail.Internal.", "10.0.0.7", "::1"}, Validity: 90 * day,
	}, nil)
	if err != nil {
		t.Fatalf("MintLeaf: %v", err)
	}

	crt, err := ParseCertificate(certPEM)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if len(crt.DNSNames) != 1 || crt.DNSNames[0] != "mail.internal" {
		t.Errorf("dns names = %v, want the normalized name only", crt.DNSNames)
	}

	if len(crt.IPAddresses) != 2 {
		t.Errorf("ip addresses = %v, want both", crt.IPAddresses)
	}
}

// 512 bits mints happily and nothing accepts it, so the floor belongs
// here rather than in each caller's validation.
func TestAnUndersizedRSAKeyIsRefused(t *testing.T) {
	if _, _, err := MintLeaf(LeafRequest{
		Hosts: []string{"mail.internal"}, Algorithm: AlgRSA, RSABits: 512, Validity: 90 * day,
	}, nil); err == nil {
		t.Error("a 512 bit rsa key was minted")
	}
}

func TestLoadIssuerRefusesSomethingThatIsNotAnAuthority(t *testing.T) {
	certPEM, keyPEM, err := MintLeaf(LeafRequest{Hosts: []string{"mail.internal"}, Validity: 90 * day}, nil)
	if err != nil {
		t.Fatalf("MintLeaf: %v", err)
	}

	if _, err := LoadIssuer(certPEM, keyPEM); err == nil {
		t.Error("a plain leaf was accepted as an authority")
	}
}

// Nothing here keeps an issuance log, so a repeat would go unnoticed -
// 128 random bits is what makes that acceptable. Moved here from
// relayca when the serial became a shared primitive.
func TestGeneratedSerialsDoNotRepeat(t *testing.T) {
	seen := map[string]bool{}
	for range 50 {
		n, err := Serial()
		if err != nil {
			t.Fatalf("Serial: %v", err)
		}

		s := n.String()
		if seen[s] {
			t.Fatal("a serial number repeated, and nothing here keeps an issuance log to catch it")
		}

		seen[s] = true
	}
}
