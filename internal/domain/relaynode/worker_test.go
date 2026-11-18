// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relaynode

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/relayca"
	certmodel "github.com/yousysadmin/mailyard/internal/models/certificate"
	"github.com/yousysadmin/mailyard/internal/relayagent"
)

// memCerts is a CertStore held in memory, keyed like the real table.
type memCerts map[[2]string]*certmodel.Certificate

func (m memCerts) Get(_ context.Context, scope, name string) (*certmodel.Certificate, error) {
	return m[[2]string{scope, name}], nil
}

func (m memCerts) Put(_ context.Context, c *certmodel.Certificate) error {
	m[[2]string{c.Scope, c.Name}] = c

	return nil
}

func (m memCerts) Delete(_ context.Context, scope, name string) error {
	delete(m, [2]string{scope, name})

	return nil
}

func (m memCerts) ListScope(_ context.Context, scope string) ([]*certmodel.Certificate, error) {
	var out []*certmodel.Certificate
	for k, c := range m {
		if k[0] == scope {
			out = append(out, c)
		}
	}

	return out, nil
}

// enrolNode signs a fresh key for nodeID under host, as Register does.
func enrolNode(t *testing.T, a *Authority, nodeID, host string) tls.Certificate {
	t.Helper()
	csrPEM, keyPEM, err := relayagent.NewKeyAndCSR(host)
	if err != nil {
		t.Fatal(err)
	}

	certPEM, _, err := a.SignNode(context.Background(), nodeID, csrPEM, host, []string{host})
	if err != nil {
		t.Fatal(err)
	}

	pair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		t.Fatal(err)
	}

	return pair
}

// dialNode runs one worker handshake against a node presenting leaf.
func dialNode(t *testing.T, a *Authority, w *WorkerIdentity, nodeID, host string, leaf tls.Certificate) error {
	t.Helper()
	ctx := context.Background()
	ca, err := a.CA(ctx)
	if err != nil {
		t.Fatal(err)
	}

	clients := x509.NewCertPool()
	clients.AppendCertsFromPEM([]byte(ca.CertPEM()))

	cfg, err := w.WorkerTLS(ctx, nodeID, host)
	if err != nil {
		t.Fatal(err)
	}

	cconn, sconn := net.Pipe()
	srv := tls.Server(sconn, &tls.Config{
		Certificates: []tls.Certificate{leaf},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clients,
		MinVersion:   tls.VersionTLS13,
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.Handshake()
		_ = sconn.Close()
	}()

	err = tls.Client(cconn, cfg).Handshake()
	_ = cconn.Close()
	<-done

	return err
}

// A worker dialling node B accepts B's own leaf only. Node A holds a
// leaf whose SAN verifies for B's address, and it is still refused.
func TestAWorkerAcceptsOnlyTheDialledNodesCertificate(t *testing.T) {
	a := &Authority{Store: memCerts{}}
	const host = "10.0.0.5"
	leafA := enrolNode(t, a, "node-a", host)
	leafB := enrolNode(t, a, "node-b", host)
	w := &WorkerIdentity{Authority: a}

	if err := dialNode(t, a, w, "node-b", host, leafA); err == nil {
		t.Fatal("node A's certificate was accepted while dialling node B")
	}

	if err := dialNode(t, a, w, "node-b", host, leafB); err != nil {
		t.Fatalf("node B's own certificate was refused: %v", err)
	}

	if err := dialNode(t, a, w, "node-a", host, leafA); err != nil {
		t.Fatalf("node A's own certificate was refused: %v", err)
	}
}

// A renewal replaces the record, and the renewed leaf is accepted on
// the next dial without waiting for the cached set to expire.
func TestARenewedNodeCertificateIsAccepted(t *testing.T) {
	a := &Authority{Store: memCerts{}}
	const host = "10.0.0.7"
	old := enrolNode(t, a, "node-c", host)
	w := &WorkerIdentity{Authority: a}

	if err := dialNode(t, a, w, "node-c", host, old); err != nil {
		t.Fatalf("the enrolled certificate was refused: %v", err)
	}

	renewed := enrolNode(t, a, "node-c", host)
	if err := dialNode(t, a, w, "node-c", host, renewed); err != nil {
		t.Fatalf("the renewed certificate was refused: %v", err)
	}
}

// The shared worker pair is renewed without anything dialling a node,
// which is the state an installation whose nodes only receive sits in.
func TestTheWorkerPairIsRenewedWithoutADial(t *testing.T) {
	ctx := context.Background()
	caPEM, caKey, err := relayca.Generate("test ca", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	ca, err := relayca.Load(caPEM, caKey)
	if err != nil {
		t.Fatal(err)
	}

	// Issued a hundred days ago, so the ninety day leaf ran out ten
	// days back and nothing asked for a new one.
	certPEM, keyPEM, err := ca.IssuePair(relayca.RoleClient, "mailyard-worker", nil,
		time.Now().Add(-100*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	store := memCerts{
		[2]string{certmodel.ScopeRelayCA, ""}:     {Scope: certmodel.ScopeRelayCA, CertPEM: caPEM, Data: caKey},
		[2]string{certmodel.ScopeRelayClient, ""}: {Scope: certmodel.ScopeRelayClient, CertPEM: certPEM, Data: keyPEM},
	}

	w := &WorkerIdentity{Authority: &Authority{Store: store}}
	if err := w.Renew(ctx); err != nil {
		t.Fatalf("renew: %v", err)
	}

	stored := store[[2]string{certmodel.ScopeRelayClient, ""}]
	if stored.CertPEM == certPEM {
		t.Fatal("the expired pair is still the one on record")
	}

	crt, err := relayca.ParseCertificate(stored.CertPEM)
	if err != nil {
		t.Fatal(err)
	}

	if !crt.NotAfter.After(time.Now()) {
		t.Fatalf("the stored pair still expires at %s", crt.NotAfter)
	}
}

// A pair with most of its life left is left alone, so the job is free
// to run as often as the sweep beside it.
func TestARenewalLeavesAHealthyWorkerPairAlone(t *testing.T) {
	ctx := context.Background()
	store := memCerts{}
	a := &Authority{Store: store}
	first, err := a.Client(ctx)
	if err != nil {
		t.Fatal(err)
	}

	w := &WorkerIdentity{Authority: a}
	if err := w.Renew(ctx); err != nil {
		t.Fatalf("renew: %v", err)
	}

	if store[[2]string{certmodel.ScopeRelayClient, ""}].CertPEM != first.CertPEM {
		t.Fatal("a pair that is nowhere near expiry was reissued")
	}
}
