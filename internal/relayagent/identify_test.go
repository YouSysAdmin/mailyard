// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relayagent

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/certgen"
)

// The reset-authority answer tells the operator to empty the spool
// because a configured token alone does not make a node re-enrol.
func TestAStoredIdentityIsKeptWhateverTokenIsConfigured(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	certPEM, _, err := certgen.MintCA(certgen.CARequest{
		Subject:   certgen.Subject{CommonName: "node"},
		Algorithm: "ecdsa",
		Validity:  365 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("MintCA: %v", err)
	}

	s := testSpool(t)
	for k, v := range map[string]string{
		metaNodeID: "old-node", metaToken: "old-token", metaCert: certPEM,
		metaKey: "key", metaCA: certPEM, metaPeerCN: "worker",
	} {
		if serr := s.SetMeta(k, v); serr != nil {
			t.Fatalf("SetMeta: %v", serr)
		}
	}

	a := &Agent{
		cfg:   Config{ControlURL: srv.URL, EnrollToken: "fresh-token", Hostname: "node.test"},
		log:   quietLog(),
		spool: s,
		ctrl:  &Control{BaseURL: srv.URL},
	}
	if err := a.identify(t.Context()); err != nil {
		t.Fatalf("identify: %v", err)
	}

	if a.nodeID != "old-node" {
		t.Fatalf("node id = %q, want the stored one", a.nodeID)
	}

	if n := calls.Load(); n != 0 {
		t.Fatalf("control plane called %d times, a stored identity must not enrol", n)
	}
}
