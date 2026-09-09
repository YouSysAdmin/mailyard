// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relayca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"net"
	"slices"
	"strings"
	"testing"
	"time"
)

func newCA(t *testing.T) *CA {
	t.Helper()
	certPEM, keyPEM, err := Generate("Mailyard Relay CA", time.Now())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	ca, err := Load(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	return ca
}

// makeCSR builds a request the way a node would, optionally claiming
// SANs of its own.
func makeCSR(t *testing.T, cn string, dnsNames []string) (csrPEM string, key *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: cn},
		DNSNames: dnsNames,
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, tmpl, key)
	if err != nil {
		t.Fatalf("create csr: %v", err)
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})), key
}

func parseLeaf(t *testing.T, certPEM string) *x509.Certificate {
	t.Helper()
	crt, err := ParseCertificate(certPEM)
	if err != nil {
		t.Fatalf("parse issued certificate: %v", err)
	}

	return crt
}

func TestGenerateAndLoadRoundTrip(t *testing.T) {
	certPEM, keyPEM, err := Generate("Mailyard Relay CA", time.Now())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	ca, err := Load(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if ca.CertPEM() != certPEM {
		t.Error("CertPEM does not match what was generated")
	}

	if got := ca.NotAfter(); time.Until(got) < 9*365*24*time.Hour {
		t.Errorf("ca expires at %v, sooner than the fleet it signs for", got)
	}
}

func TestLoadRejectsACertificateThatIsNotACA(t *testing.T) {
	ca := newCA(t)
	leafPEM, leafKeyPEM, err := ca.IssuePair(RoleNode, "node1.example.com",
		[]string{"node1.example.com"}, time.Now())
	if err != nil {
		t.Fatalf("IssuePair: %v", err)
	}

	if _, err := Load(leafPEM, leafKeyPEM); err == nil {
		t.Error("a leaf certificate was accepted as an authority")
	}
}

// The load-bearing rule of the whole package.
//
// A CSR is written by the party being authenticated. If it could
// choose its own SubjectAltNames, one node would request a
// certificate carrying another node's name - and since AllowedPeerNames
// narrows access by exactly those names, that is how it would connect
// where it must not.
func TestTheNamesInTheRequestAreIgnored(t *testing.T) {
	ca := newCA(t)
	csrPEM, _ := makeCSR(t, "node1.example.com",
		[]string{"node2.example.com", "mail.example.com", "*.example.com"})

	certPEM, err := ca.SignRequest(csrPEM, RoleNode, "node1.example.com",
		[]string{"node1.example.com"}, time.Now())
	if err != nil {
		t.Fatalf("SignRequest: %v", err)
	}

	leaf := parseLeaf(t, certPEM)
	if len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != "node1.example.com" {
		t.Fatalf("issued certificate carries %v - the request chose its own names", leaf.DNSNames)
	}
	for _, bad := range []string{"node2.example.com", "mail.example.com", "*.example.com"} {
		if leaf.VerifyHostname(bad) == nil {
			t.Errorf("the issued certificate is valid for %q", bad)
		}
	}
}

// A CSR is self-signed by the key it names. Without this check a
// caller could present somebody else's public key and be handed a
// certificate for it.
func TestARequestWithABrokenSignatureIsRefused(t *testing.T) {
	ca := newCA(t)
	csrPEM, _ := makeCSR(t, "node1.example.com", nil)

	// Corrupt the signature by flipping bytes inside the DER.
	block, _ := pem.Decode([]byte(csrPEM))
	der := block.Bytes
	der[len(der)-1] ^= 0xff
	der[len(der)-2] ^= 0xff
	tampered := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))

	if _, err := ca.SignRequest(tampered, RoleNode, "node1.example.com",
		[]string{"node1.example.com"}, time.Now()); err == nil {
		t.Error("a request with an invalid signature was signed")
	}
}

func TestGarbageIsNotARequest(t *testing.T) {
	ca := newCA(t)
	for name, in := range map[string]string{
		"empty":       "",
		"not pem":     "hello",
		"wrong block": "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n",
	} {
		if _, err := ca.SignRequest(in, RoleNode, "n", []string{"n"}, time.Now()); err == nil {
			t.Errorf("%s was accepted as a certificate request", name)
		}
	}
}

func TestRolesGetTheirOwnKeyUsage(t *testing.T) {
	ca := newCA(t)
	now := time.Now()

	nodeCSR, _ := makeCSR(t, "node1.example.com", nil)
	nodePEM, err := ca.SignRequest(nodeCSR, RoleNode, "node1.example.com",
		[]string{"node1.example.com"}, now)
	if err != nil {
		t.Fatalf("SignRequest node: %v", err)
	}
	node := parseLeaf(t, nodePEM)
	if !hasEKU(node, x509.ExtKeyUsageServerAuth) {
		t.Error("a node certificate cannot do server auth")
	}

	if hasEKU(node, x509.ExtKeyUsageClientAuth) {
		t.Error("a node certificate can also be used as a client, so a stolen one could connect into another node")
	}

	clientCSR, _ := makeCSR(t, "mailyard-worker", nil)
	clientPEM, err := ca.SignRequest(clientCSR, RoleClient, "mailyard-worker", nil, now)
	if err != nil {
		t.Fatalf("SignRequest client: %v", err)
	}
	client := parseLeaf(t, clientPEM)
	if !hasEKU(client, x509.ExtKeyUsageClientAuth) {
		t.Error("a client certificate cannot do client auth")
	}

	if hasEKU(client, x509.ExtKeyUsageServerAuth) {
		t.Error("a client certificate can serve, which it never needs to")
	}
}

func TestAnUnknownRoleIsRefused(t *testing.T) {
	ca := newCA(t)
	csrPEM, _ := makeCSR(t, "node1.example.com", nil)
	if _, err := ca.SignRequest(csrPEM, "admin", "node1.example.com", nil, time.Now()); err == nil {
		t.Error("an unknown role was signed")
	}
}

func TestALeafIsNotAnAuthority(t *testing.T) {
	ca := newCA(t)
	csrPEM, _ := makeCSR(t, "node1.example.com", nil)
	certPEM, err := ca.SignRequest(csrPEM, RoleNode, "node1.example.com",
		[]string{"node1.example.com"}, time.Now())
	if err != nil {
		t.Fatalf("SignRequest: %v", err)
	}

	if leaf := parseLeaf(t, certPEM); leaf.IsCA {
		t.Error("an issued node certificate is a CA and could sign further certificates")
	}
}

// A node identified by address is an ordinary way to run one. An IP
// in DNSNames is an invalid SAN that verifiers reject, so it has to
// land in IPAddresses or that node is simply unreachable.
func TestAnAddressBecomesAnIPSAN(t *testing.T) {
	ca := newCA(t)
	csrPEM, _ := makeCSR(t, "node1", nil)
	certPEM, err := ca.SignRequest(csrPEM, RoleNode, "node1",
		[]string{"203.0.113.10", "node1.example.com"}, time.Now())
	if err != nil {
		t.Fatalf("SignRequest: %v", err)
	}
	leaf := parseLeaf(t, certPEM)
	if len(leaf.IPAddresses) != 1 || leaf.IPAddresses[0].String() != "203.0.113.10" {
		t.Errorf("IP SANs are %v", leaf.IPAddresses)
	}

	if len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != "node1.example.com" {
		t.Errorf("DNS SANs are %v", leaf.DNSNames)
	}
}

func TestALeafExpiresLongBeforeTheAuthority(t *testing.T) {
	ca := newCA(t)
	now := time.Now()
	csrPEM, _ := makeCSR(t, "node1.example.com", nil)
	certPEM, err := ca.SignRequest(csrPEM, RoleNode, "node1.example.com",
		[]string{"node1.example.com"}, now)
	if err != nil {
		t.Fatalf("SignRequest: %v", err)
	}
	leaf := parseLeaf(t, certPEM)
	if leaf.NotAfter.After(ca.NotAfter()) {
		t.Error("a leaf outlives the authority that signed it")
	}

	if d := leaf.NotAfter.Sub(now); d > LeafValidity+time.Hour || d < LeafValidity-time.Hour {
		t.Errorf("leaf lifetime is %v, want %v", d, LeafValidity)
	}
}

// The point of the whole exercise: a real mutually-authenticated
// handshake, with the node serving and the worker connecting.
func TestAWorkerAndANodeCompleteAMutualHandshake(t *testing.T) {
	ca := newCA(t)
	now := time.Now()

	nodeCert, nodeKey, err := ca.IssuePair(RoleNode, "node1.example.com",
		[]string{"node1.example.com"}, now)
	if err != nil {
		t.Fatalf("IssuePair node: %v", err)
	}
	clientCert, clientKey, err := ca.IssuePair(RoleClient, "mailyard-worker", nil, now)
	if err != nil {
		t.Fatalf("IssuePair client: %v", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(ca.CertPEM())) {
		t.Fatal("the ca certificate did not load into a pool")
	}
	serverPair, err := tls.X509KeyPair([]byte(nodeCert), []byte(nodeKey))
	if err != nil {
		t.Fatalf("node keypair: %v", err)
	}
	clientPair, err := tls.X509KeyPair([]byte(clientCert), []byte(clientKey))
	if err != nil {
		t.Fatalf("client keypair: %v", err)
	}

	got := handshake(t,
		&tls.Config{
			Certificates: []tls.Certificate{serverPair},
			ClientAuth:   tls.RequireAndVerifyClientCert,
			ClientCAs:    pool,
			MinVersion:   tls.VersionTLS13,
		},
		&tls.Config{
			Certificates: []tls.Certificate{clientPair},
			RootCAs:      pool,
			ServerName:   "node1.example.com",
			MinVersion:   tls.VersionTLS13,
		})
	if got != nil {
		t.Fatalf("handshake between our own node and worker failed: %v", got)
	}
}

// Without a client certificate there is no way in at all - which is
// the property that replaces the password.
func TestANodeRefusesAConnectionWithNoClientCertificate(t *testing.T) {
	ca := newCA(t)
	nodeCert, nodeKey, err := ca.IssuePair(RoleNode, "node1.example.com",
		[]string{"node1.example.com"}, time.Now())
	if err != nil {
		t.Fatalf("IssuePair: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM([]byte(ca.CertPEM()))
	serverPair, _ := tls.X509KeyPair([]byte(nodeCert), []byte(nodeKey))

	err = handshake(t,
		&tls.Config{
			Certificates: []tls.Certificate{serverPair},
			ClientAuth:   tls.RequireAndVerifyClientCert,
			ClientCAs:    pool,
			MinVersion:   tls.VersionTLS13,
		},
		&tls.Config{RootCAs: pool, ServerName: "node1.example.com", MinVersion: tls.VersionTLS13})
	if err == nil {
		t.Error("a client with no certificate was let in")
	}
}

// A certificate from a different authority is not ours, however
// well-formed it is.
func TestANodeRefusesACertificateFromAnotherAuthority(t *testing.T) {
	ours := newCA(t)
	theirs := newCA(t)

	nodeCert, nodeKey, err := ours.IssuePair(RoleNode, "node1.example.com",
		[]string{"node1.example.com"}, time.Now())
	if err != nil {
		t.Fatalf("IssuePair: %v", err)
	}
	intruderCert, intruderKey, err := theirs.IssuePair(RoleClient, "mailyard-worker", nil, time.Now())
	if err != nil {
		t.Fatalf("IssuePair intruder: %v", err)
	}

	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM([]byte(ours.CertPEM()))
	serverPair, _ := tls.X509KeyPair([]byte(nodeCert), []byte(nodeKey))
	intruderPair, _ := tls.X509KeyPair([]byte(intruderCert), []byte(intruderKey))

	theirPool := x509.NewCertPool()
	theirPool.AppendCertsFromPEM([]byte(theirs.CertPEM()))

	err = handshake(t,
		&tls.Config{
			Certificates: []tls.Certificate{serverPair},
			ClientAuth:   tls.RequireAndVerifyClientCert,
			ClientCAs:    pool,
			MinVersion:   tls.VersionTLS13,
		},
		&tls.Config{
			Certificates: []tls.Certificate{intruderPair},
			// The intruder trusts its own CA, so the failure under
			// test is ours refusing them, not them refusing us.
			RootCAs:            theirPool,
			InsecureSkipVerify: true, //nolint:gosec // exercising the server side
			ServerName:         "node1.example.com",
			MinVersion:         tls.VersionTLS13,
		})
	if err == nil {
		t.Error("a certificate from an unrelated authority was accepted")
	}
}

// handshake runs one TLS connection and returns the client-side
// error, or the server's when the client saw none.
func handshake(t *testing.T, serverCfg, clientCfg *tls.Config) error {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	serverErr := make(chan error, 1)
	go func() {
		conn, aerr := ln.Accept()
		if aerr != nil {
			serverErr <- aerr

			return
		}
		defer func() { _ = conn.Close() }()
		tconn := tls.Server(conn, serverCfg)
		if herr := tconn.Handshake(); herr != nil {
			serverErr <- herr

			return
		}
		_, _ = io.WriteString(tconn, "ok")
		serverErr <- nil
	}()

	conn, err := tls.Dial("tcp", ln.Addr().String(), clientCfg)
	if err != nil {
		<-serverErr

		return err
	}
	defer func() { _ = conn.Close() }()

	buf := make([]byte, 2)
	if _, rerr := io.ReadFull(conn, buf); rerr != nil {
		<-serverErr

		return rerr
	}

	return <-serverErr
}

func hasEKU(c *x509.Certificate, want x509.ExtKeyUsage) bool {
	return slices.Contains(c.ExtKeyUsage, want)
}

func TestTheAuthorityCannotBeChainedFurther(t *testing.T) {
	ca := newCA(t)
	crt, err := ParseCertificate(ca.CertPEM())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if !crt.MaxPathLenZero || crt.MaxPathLen != 0 {
		t.Error("the authority allows intermediate CAs beneath it")
	}

	if !strings.Contains(crt.Subject.CommonName, "Relay") {
		t.Errorf("subject is %q", crt.Subject.CommonName)
	}
}
