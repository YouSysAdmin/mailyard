// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relayagent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-smtp"

	"github.com/yousysadmin/mailyard/internal/core/relayca"
)

const (
	testNodeHost   = "node.test"
	testWorkerName = "mailyard-worker"
)

func testAuthority(t *testing.T) *relayca.CA {
	t.Helper()

	certPEM, keyPEM, err := relayca.Generate("Mailyard Relay CA", time.Now())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	ca, err := relayca.Load(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	return ca
}

func issue(t *testing.T, ca *relayca.CA, role relayca.Role, name string) (certPEM, keyPEM string) {
	t.Helper()

	csrPEM, keyPEM, err := NewKeyAndCSR(name)
	if err != nil {
		t.Fatalf("NewKeyAndCSR: %v", err)
	}

	certPEM, err = ca.SignRequest(csrPEM, role, name, []string{name}, time.Now())
	if err != nil {
		t.Fatalf("SignRequest: %v", err)
	}

	return certPEM, keyPEM
}

// enrolUnder writes a node identity issued by ca into the spool and
// rebuilds TLS, the way enrolment and renewal leave it.
func enrolUnder(t *testing.T, a *Agent, ca *relayca.CA) {
	t.Helper()

	cert, key := issue(t, ca, relayca.RoleNode, testNodeHost)
	for k, v := range map[string]string{
		metaCert: cert, metaKey: key, metaCA: ca.CertPEM(), metaPeerCN: testWorkerName,
	} {
		if err := a.spool.SetMeta(k, v); err != nil {
			t.Fatalf("SetMeta: %v", err)
		}
	}

	if err := a.buildTLS(); err != nil {
		t.Fatalf("buildTLS: %v", err)
	}
}

// serveWorkers runs the worker listener the way runRelay does.
func serveWorkers(t *testing.T, a *Agent) string {
	t.Helper()

	_, tlsCfg, backend := a.Listener()
	srv := NewServer(backend, "127.0.0.1:0", testNodeHost, tlsCfg)

	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	go func() { _ = srv.Serve(tls.NewListener(raw, tlsCfg)) }()
	t.Cleanup(func() { _ = srv.Close() })

	return raw.Addr().String()
}

// sendAsWorker hands one message over with a worker certificate from
// clientCA, trusting serverCAs for the node.
func sendAsWorker(t *testing.T, addr string, clientCA *relayca.CA, serverCAs ...*relayca.CA) error {
	t.Helper()

	certPEM, keyPEM := issue(t, clientCA, relayca.RoleClient, testWorkerName)
	pair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		t.Fatalf("client pair: %v", err)
	}

	roots := x509.NewCertPool()
	for _, ca := range serverCAs {
		roots.AppendCertsFromPEM([]byte(ca.CertPEM()))
	}

	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", addr, &tls.Config{
		Certificates: []tls.Certificate{pair},
		RootCAs:      roots,
		ServerName:   testNodeHost,
		MinVersion:   tls.VersionTLS13,
	})
	if err != nil {
		return err
	}

	c := smtp.NewClient(conn)
	defer func() { _ = c.Close() }()

	return c.SendMail("sender@example.test", []string{"user@example.org"},
		strings.NewReader("Subject: hi\r\n\r\nbody\r\n"))
}

func spooled(t *testing.T, a *Agent) int {
	t.Helper()

	all, err := a.spool.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}

	return len(all)
}

// controlPlane answers heartbeats until refuse is set, then answers
// 401 the way it does for a removed node or after an authority reset.
func controlPlane(t *testing.T, refuse *atomic.Bool) *Control {
	t.Helper()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if refuse.Load() {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"node id or token is not valid"}`))

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"enabled","stale_after_seconds":300}`))
	}))
	t.Cleanup(ts.Close)

	return &Control{BaseURL: ts.URL}
}

func testAgent(t *testing.T, ctrl *Control) *Agent {
	t.Helper()

	return &Agent{
		cfg: Config{
			Hostname: testNodeHost, MaxMessageSize: 1 << 20,
			HeartbeatInterval: 20 * time.Millisecond,
		},
		log:       quietLog(),
		spool:     testSpool(t),
		ctrl:      ctrl,
		accept:    &acceptList{},
		Deliverer: &Deliverer{},
		nodeID:    "node-1",
		token:     "token-1",
	}
}

// eventually retries a send until it is accepted, since the listener
// opens on the first heartbeat.
func eventually(t *testing.T, send func() error) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for {
		err := send()
		if err == nil {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("worker never got through: %v", err)
		}

		time.Sleep(20 * time.Millisecond)
	}
}

// The worker listener follows the node's identity as it is now. A
// rotation must stop a client of the retired authority at once and
// admit the new one, and a node the control plane no longer accepts
// takes nothing new.
func TestTheWorkerListenerFollowsTheCurrentIdentity(t *testing.T) {
	var refuse atomic.Bool

	a := testAgent(t, controlPlane(t, &refuse))
	ca1 := testAuthority(t)
	enrolUnder(t, a, ca1)
	addr := serveWorkers(t, a)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	beats := make(chan struct{})
	go func() {
		a.heartbeatLoop(ctx)
		close(beats)
	}()

	eventually(t, func() error { return sendAsWorker(t, addr, ca1, ca1) })

	ca2 := testAuthority(t)
	enrolUnder(t, a, ca2)

	before := spooled(t, a)
	// Trusting both authorities, so only the node can refuse it.
	if err := sendAsWorker(t, addr, ca1, ca1, ca2); err == nil {
		t.Fatal("a worker of the retired authority was admitted after rotation")
	}

	if got := spooled(t, a); got != before {
		t.Fatalf("spool grew from %d to %d on a retired client", before, got)
	}

	if err := sendAsWorker(t, addr, ca2, ca2); err != nil {
		t.Fatalf("a worker of the current authority was refused: %v", err)
	}

	refuse.Store(true)
	select {
	case <-beats:
	case <-time.After(5 * time.Second):
		t.Fatal("the heartbeat did not stop on a fatal refusal")
	}

	before = spooled(t, a)
	if err := sendAsWorker(t, addr, ca2, ca2); err == nil {
		t.Fatal("a node the control plane refused took a new session")
	}

	if got := spooled(t, a); got != before {
		t.Fatalf("spool grew from %d to %d after a fatal heartbeat", before, got)
	}
}

// A node restarting on a stored identity takes no worker session
// until the control plane has accepted that identity, so a retired one
// never reopens the listener.
func TestARestartedNodeWaitsForTheControlPlane(t *testing.T) {
	var refuse atomic.Bool
	refuse.Store(true)

	a := testAgent(t, controlPlane(t, &refuse))
	ca := testAuthority(t)
	enrolUnder(t, a, ca)
	addr := serveWorkers(t, a)

	if err := sendAsWorker(t, addr, ca, ca); err == nil {
		t.Fatal("a session was taken before any heartbeat")
	}

	a.heartbeatLoop(context.Background())

	if err := sendAsWorker(t, addr, ca, ca); err == nil {
		t.Fatal("a node the control plane refused took a session")
	}

	if got := spooled(t, a); got != 0 {
		t.Fatalf("spooled %d messages, want none", got)
	}
}
