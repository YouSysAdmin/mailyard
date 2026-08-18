// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package certgen mints x509 certificates.
//
// go-tlsutils cannot express what an operator needs: a CommonName and
// an Organization and nothing else - no OU, no country, no IsCA, no
// parent to sign with - and its default CommonName is the literal
// "tlsutils self-signed", which is what every certificate this product
// generated carried. Every version in the module cache is the same.
//
// POLICY-FREE on purpose: the caller says what the certificate should
// be. That is why it does not replace internal/ee/relayca, which is
// the opposite - fixed lifetimes, key usages and Subject, so "a node
// certificate that is also a CA" cannot be asked for. They share the
// mechanics below, which existed two and three times over.
package certgen

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"
)

// Supported key algorithms.
const (
	AlgRSA     = "rsa"
	AlgECDSA   = "ecdsa"
	AlgEd25519 = "ed25519"
)

// DefaultRSABits sizes an RSA key when the caller names none.
const DefaultRSABits = 2048

// Subject is the certificate Subject an operator fills in.
//
// A flat struct rather than pkix.Name because pkix.Name carries a
// dozen fields nobody uses and two representations of most of them.
// These six are what certificate forms ask for.
type Subject struct {
	CommonName   string
	Organization string
	Unit         string
	Country      string
	State        string
	Locality     string
}

func (s Subject) pkix() pkix.Name {
	n := pkix.Name{CommonName: s.CommonName}
	// Each is a slice in pkix.Name, and an empty string appended would
	// encode as a present-but-empty attribute rather than an absent
	// one. Some verifiers reject that, and it is a lie either way.
	if s.Organization != "" {
		n.Organization = []string{s.Organization}
	}

	if s.Unit != "" {
		n.OrganizationalUnit = []string{s.Unit}
	}

	if s.Country != "" {
		n.Country = []string{strings.ToUpper(s.Country)}
	}

	if s.State != "" {
		n.Province = []string{s.State}
	}

	if s.Locality != "" {
		n.Locality = []string{s.Locality}
	}

	return n
}

// CARequest asks for an authority.
type CARequest struct {
	Subject Subject

	// Algorithm is rsa or ecdsa. Not ed25519, and that is the one
	// restriction here that is not about correctness: an Ed25519
	// certificate is legal and Go signs and verifies it happily, but
	// several OS trust stores refuse to install one. A root the
	// operator cannot install is a root that defeats the purpose of
	// having one.
	Algorithm string
	RSABits   int
	Validity  time.Duration

	// Now is the clock, so a test can mint something already expired.
	// Zero means time.Now().
	Now time.Time
}

// LeafRequest asks for a certificate that will serve TLS.
type LeafRequest struct {
	Subject Subject

	// Hosts become SubjectAltNames and at least one is required. Go
	// has ignored CommonName for hostname matching since 1.15, so a
	// certificate with no SAN matches nothing anywhere.
	Hosts     []string
	Algorithm string
	RSABits   int
	Validity  time.Duration
	Now       time.Time
}

// Issuer is a loaded authority that can sign.
type Issuer struct {
	Certificate *x509.Certificate
	Key         crypto.Signer
}

// LoadIssuer rebuilds an authority from its stored halves.
func LoadIssuer(certPEM, keyPEM string) (*Issuer, error) {
	crt, err := ParseCertificate(certPEM)
	if err != nil {
		return nil, err
	}

	if !crt.IsCA {
		return nil, errors.New("that certificate is not a certificate authority")
	}

	block, _ := pem.Decode([]byte(keyPEM))
	if block == nil {
		return nil, errors.New("the issuer private key is not PEM")
	}

	parsed, err := parsePrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	signer, ok := parsed.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("the issuer private key is %T, which cannot sign", parsed)
	}

	return &Issuer{Certificate: crt, Key: signer}, nil
}

// MintCA generates an authority and self-signs it.
//
// No SANs and no ExtKeyUsage: a CA is a trust anchor, not something
// that serves a name. MaxPathLenZero because the only chain this is
// for is root to leaf - permitting a longer one would let an
// intermediate exist, and there is nothing here that wants one.
func MintCA(req CARequest) (certPEM, keyPEM string, err error) {
	alg, err := normalizeAlg(req.Algorithm, AlgECDSA)
	if err != nil {
		return "", "", err
	}

	if alg == AlgEd25519 {
		return "", "", errors.New("a certificate authority may be rsa or ecdsa - several trust stores refuse to install an ed25519 root")
	}

	if req.Subject.CommonName == "" {
		// No fallback to a host, because a CA has none. A nameless
		// authority is one an operator cannot recognise in a trust
		// store listing, which is the only place they will ever meet
		// it again.
		return "", "", errors.New("a certificate authority needs a common name")
	}

	key, err := GenerateKey(alg, req.RSABits)
	if err != nil {
		return "", "", err
	}

	now := clockOf(req.Now)
	tmpl, err := baseTemplate(req.Subject, now, req.Validity)
	if err != nil {
		return "", "", err
	}

	tmpl.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign
	tmpl.IsCA = true
	tmpl.MaxPathLen = 0
	tmpl.MaxPathLenZero = true

	return sign(tmpl, tmpl, key, key)
}

// MintLeaf generates a certificate that serves TLS, self-signed when
// issuer is nil and signed by that authority otherwise.
func MintLeaf(req LeafRequest, issuer *Issuer) (certPEM, keyPEM string, err error) {
	alg, err := normalizeAlg(req.Algorithm, AlgRSA)
	if err != nil {
		return "", "", err
	}

	subject := req.Subject
	if subject.CommonName == "" {
		// The first host, never a constant. This is the whole reported
		// bug: the library's default put its own name here.
		subject.CommonName = firstHost(req.Hosts)
	}
	if subject.CommonName == "" {
		return "", "", errors.New("a certificate needs at least one host")
	}

	key, err := GenerateKey(alg, req.RSABits)
	if err != nil {
		return "", "", err
	}

	now := clockOf(req.Now)
	tmpl, err := baseTemplate(subject, now, req.Validity)
	if err != nil {
		return "", "", err
	}

	tmpl.KeyUsage = KeyUsageFor(key)
	tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	ApplyHosts(tmpl, req.Hosts)
	if len(tmpl.DNSNames) == 0 && len(tmpl.IPAddresses) == 0 {
		return "", "", errors.New("a certificate needs at least one host")
	}

	parent, signWith := tmpl, key
	if issuer != nil {
		parent, signWith = issuer.Certificate, issuer.Key
		// CLAMPED, not refused. x509.CreateCertificate will mint a
		// leaf that outlives its authority without a word, and the
		// certificate then stops verifying on a date nothing in the
		// product warns about - the error a client reports names the
		// leaf, which is the one certificate that is still fine.
		if tmpl.NotAfter.After(parent.NotAfter) {
			tmpl.NotAfter = parent.NotAfter
		}
	}

	return sign(tmpl, parent, key, signWith)
}

// baseTemplate is everything a CA and a leaf agree on.
func baseTemplate(subject Subject, now time.Time, validity time.Duration) (*x509.Certificate, error) {
	if validity <= 0 {
		return nil, errors.New("validity must be positive")
	}

	serial, err := Serial()
	if err != nil {
		return nil, err
	}

	return &x509.Certificate{
		SerialNumber: serial,
		Subject:      subject.pkix(),
		// Backdated an hour. The generator and the verifier are
		// different machines with different ideas of the time, and a
		// certificate that is not valid yet fails exactly like one
		// that is invalid.
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(validity),
		BasicConstraintsValid: true,
	}, nil
}

// sign creates the certificate and PEM-encodes it with the key that
// belongs to it.
//
// The two keys are different things and conflating them is the bug
// waiting here: `subject` is the key of the certificate being minted
// and is what gets returned, `signWith` is the key that vouches for it
// and belongs to the parent. They are the same key only when the
// certificate is self-signed.
func sign(tmpl, parent *x509.Certificate, subject, signWith crypto.Signer) (string, string, error) {
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, subject.Public(), signWith)
	if err != nil {
		return "", "", fmt.Errorf("create certificate: %w", err)
	}

	return EncodePair(der, subject)
}

// ----------------------------------------------------------------------------
// Primitives shared with relayca
// ----------------------------------------------------------------------------

// Serial draws a 128-bit random serial, which is what an authority
// keeping no issuance log has to do to never reuse one.
func Serial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	n, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("serial number: %w", err)
	}

	return n.Add(n, big.NewInt(1)), nil
}

// GenerateKey mints a private key. An empty algorithm is an error
// here rather than a default - the two callers disagree about what
// the default should be, so each states it.
func GenerateKey(alg string, rsaBits int) (crypto.Signer, error) {
	switch alg {
	case AlgECDSA:
		return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	case AlgEd25519:
		_, priv, err := ed25519.GenerateKey(rand.Reader)

		return priv, err
	case AlgRSA:
		if rsaBits <= 0 {
			rsaBits = DefaultRSABits
		}

		if rsaBits < DefaultRSABits {
			// 512 bits mints happily and nothing on the internet
			// accepts it, so the floor is here rather than in each
			// caller's validation.
			return nil, fmt.Errorf("an rsa key must be at least %d bits", DefaultRSABits)
		}

		return rsa.GenerateKey(rand.Reader, rsaBits)
	default:
		return nil, fmt.Errorf("unknown key algorithm %q: want %s, %s or %s", alg, AlgRSA, AlgECDSA, AlgEd25519)
	}
}

// KeyUsageFor returns the bits appropriate to the key type.
//
// RSA key exchange needs KeyEncipherment. A signing-only key does
// not, and asserting it anyway is meaningless - which is what relayca
// did for its ECDSA leaves before this was shared.
func KeyUsageFor(key crypto.Signer) x509.KeyUsage {
	return KeyUsageForPublic(key.Public())
}

// KeyUsageForPublic is the same answer from the public half alone, for
// a caller signing a certificate request - where that is all it has.
func KeyUsageForPublic(pub crypto.PublicKey) x509.KeyUsage {
	if _, ok := pub.(*rsa.PublicKey); ok {
		return x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment
	}

	return x509.KeyUsageDigitalSignature
}

// ApplyHosts fills the SubjectAltName fields, splitting addresses
// from names.
//
// An IP in DNSNames is an invalid SAN that most verifiers reject
// outright, so a host identified only by its address - an ordinary
// way to run an internal listener - would be unreachable. Names are
// lowercased and lose a trailing dot, because that is the form a
// verifier compares against.
func ApplyHosts(tmpl *x509.Certificate, hosts []string) {
	for _, h := range hosts {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}

		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
			continue
		}

		tmpl.DNSNames = append(tmpl.DNSNames, strings.ToLower(strings.TrimSuffix(h, ".")))
	}
}

// EncodePair PEM-encodes a signed certificate and its private key.
func EncodePair(certDER []byte, key crypto.Signer) (certPEM, keyPEM string, err error) {
	keyPEM, err = EncodeKey(key)
	if err != nil {
		return "", "", err
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})), keyPEM, nil
}

// EncodeKey PEM-encodes a private key on its own, for a caller whose
// certificate was signed elsewhere.
//
// PKCS#8, which is the one encoding that carries every key type this
// package can generate - and what tls.X509KeyPair reads back.
func EncodeKey(key crypto.Signer) (string, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", fmt.Errorf("marshal private key: %w", err)
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), nil
}

// ParseCertificate decodes a single PEM certificate.
func ParseCertificate(certPEM string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("not a pem certificate")
	}

	crt, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}

	return crt, nil
}

func parsePrivateKey(der []byte) (any, error) {
	if k, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		return k, nil
	}

	if k, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return k, nil
	}

	if k, err := x509.ParseECPrivateKey(der); err == nil {
		return k, nil
	}

	return nil, errors.New("the private key is not PKCS#8, PKCS#1 or SEC 1")
}

// normalizeAlg resolves an empty algorithm to the caller's default.
func normalizeAlg(alg, fallback string) (string, error) {
	alg = strings.ToLower(strings.TrimSpace(alg))
	if alg == "" {
		alg = fallback
	}

	switch alg {
	case AlgRSA, AlgECDSA, AlgEd25519:
		return alg, nil
	default:
		return "", fmt.Errorf("unknown key algorithm %q: want %s, %s or %s", alg, AlgRSA, AlgECDSA, AlgEd25519)
	}
}

func firstHost(hosts []string) string {
	for _, h := range hosts {
		if h = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(h), ".")); h != "" {
			return h
		}
	}

	return ""
}

func clockOf(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now()
	}

	return t
}
