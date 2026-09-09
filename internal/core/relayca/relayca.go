// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relayca

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/certgen"
)

// Lifetimes. The CA outlives the fleet, a leaf does not.
//
// A leaf year would mean a compromised node key is usable for a year,
// since nothing here publishes a revocation list. Ninety days makes
// renewal a routine the node already performs rather than an event
// nobody has rehearsed - the same reasoning Let's Encrypt applies, and
// for the same reason.
const (
	CAValidity   = 10 * 365 * 24 * time.Hour
	LeafValidity = 90 * 24 * time.Hour

	// RenewBefore is how early a node should ask for a new
	// certificate. A third of the lifetime leaves room for a node that
	// is offline, or whose control plane is unreachable, to try many
	// times before anything expires.
	RenewBefore = 30 * 24 * time.Hour
)

// CA is a loaded certificate authority.
type CA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	// certPEM is kept so callers can hand the bundle to a node
	// without re-encoding it.
	certPEM string
}

// Generate mints a new authority. The caller stores both halves - the
// key sealed - and never writes it to a file.
func Generate(commonName string, now time.Time) (certPEM, keyPEM string, err error) {
	signer, err := certgen.GenerateKey(certgen.AlgECDSA, 0)
	if err != nil {
		return "", "", fmt.Errorf("generate ca key: %w", err)
	}
	key, ok := signer.(*ecdsa.PrivateKey)
	if !ok {
		return "", "", fmt.Errorf("generate ca key: got %T, want ecdsa", signer)
	}
	serial, err := certgen.Serial()
	if err != nil {
		return "", "", err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName, Organization: []string{"Mailyard"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(CAValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		// The chain is CA -> leaf and nothing else. Allowing a longer
		// one would let a leaf sign further certificates if its key
		// usages were ever loosened by mistake.
		MaxPathLen:     0,
		MaxPathLenZero: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return "", "", fmt.Errorf("create ca certificate: %w", err)
	}

	return certgen.EncodePair(der, key)
}

// Load rebuilds a CA from its stored halves.
func Load(certPEM, keyPEM string) (*CA, error) {
	cert, err := certgen.ParseCertificate(certPEM)
	if err != nil {
		return nil, err
	}

	if !cert.IsCA {
		return nil, errors.New("stored relay ca certificate is not a ca")
	}
	block, _ := pem.Decode([]byte(keyPEM))
	if block == nil {
		return nil, errors.New("relay ca key is not pem")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse relay ca key: %w", err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("relay ca key is %T, want an ecdsa key", parsed)
	}

	return &CA{cert: cert, key: key, certPEM: certPEM}, nil
}

// CertPEM is the authority certificate, which is what a node and a
// worker each need as their trust root.
func (c *CA) CertPEM() string { return c.certPEM }

// NotAfter is when the authority itself expires.
func (c *CA) NotAfter() time.Time { return c.cert.NotAfter }

// Role decides what a certificate may be used for.
type Role string

const (
	// RoleNode is a relay node: it terminates TLS, so it needs server
	// auth and its SANs are the names workers will dial.
	RoleNode Role = "node"
	// RoleClient is a delivery worker: it only ever connects out.
	RoleClient Role = "client"
)

// SignRequest signs a certificate signing request produced by a node.
//
// A CSR rather than a generated pair, so the private key never
// crosses the network and is not sitting in our database waiting to
// be stolen. What comes back to us is a public key and a claim about
// which names it wants.
//
// The names in the CSR are IGNORED. The caller passes the hosts it
// has decided this identity may claim, because a CSR is written by
// the party being authenticated and letting it choose its own SANs
// would let one node request a certificate for another node's name -
// which, with AllowedPeerNames narrowing on exactly those names, is
// how it would connect where it should not.
func (c *CA) SignRequest(csrPEM string, role Role, cn string, hosts []string, now time.Time) (string, error) {
	block, _ := pem.Decode([]byte(csrPEM))
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return "", errors.New("not a pem certificate request")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse certificate request: %w", err)
	}

	// A CSR is self-signed by the key it names. Skipping this would
	// let a caller present somebody else's public key and receive a
	// certificate for it.
	if err := csr.CheckSignature(); err != nil {
		return "", fmt.Errorf("certificate request signature: %w", err)
	}

	serial, err := certgen.Serial()
	if err != nil {
		return "", err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn, Organization: []string{"Mailyard"}},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(LeafValidity),
		// Per key TYPE: RSA key exchange is the only thing that needs
		// KeyEncipherment, and asserting it on an ECDSA key is
		// meaningless.
		KeyUsage: certgen.KeyUsageForPublic(csr.PublicKey),
		// Not a CA, and said so explicitly rather than by omission.
		BasicConstraintsValid: true,
		IsCA:                  false,
	}
	switch role {
	case RoleNode:
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		certgen.ApplyHosts(tmpl, hosts)
	case RoleClient:
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
		certgen.ApplyHosts(tmpl, hosts)
	default:
		return "", fmt.Errorf("unknown certificate role %q", role)
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, c.cert, csr.PublicKey, c.key)
	if err != nil {
		return "", fmt.Errorf("sign certificate: %w", err)
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), nil
}

// IssuePair mints a keypair and certificate together, for the client
// identity the workers share. Unlike a node, that key has to live in
// our own database anyway - there is nobody else to generate it.
func (c *CA) IssuePair(role Role, cn string, hosts []string, now time.Time) (certPEM, keyPEM string, err error) {
	key, err := certgen.GenerateKey(certgen.AlgECDSA, 0)
	if err != nil {
		return "", "", fmt.Errorf("generate key: %w", err)
	}
	csrTmpl := &x509.CertificateRequest{Subject: pkix.Name{CommonName: cn}}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, csrTmpl, key)
	if err != nil {
		return "", "", fmt.Errorf("create certificate request: %w", err)
	}
	csrPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}))

	certPEM, err = c.SignRequest(csrPEM, role, cn, hosts, now)
	if err != nil {
		return "", "", err
	}
	keyPEM, err = certgen.EncodeKey(key)
	if err != nil {
		return "", "", err
	}

	return certPEM, keyPEM, nil
}

// ParseCertificate decodes a single PEM certificate.
//
// A name in this package because the relay agent and the node admin
// both ask an issued certificate when it expires. The work is certgen's.
func ParseCertificate(certPEM string) (*x509.Certificate, error) {
	return certgen.ParseCertificate(certPEM)
}
