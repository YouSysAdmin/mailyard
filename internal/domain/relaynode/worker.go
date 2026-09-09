// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relaynode

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"sync"
	"time"
)

// WorkerIdentity is the delivery worker's side of the relay
// transport: our client certificate, and the authority a node's
// certificate is checked against.
//
// One identity shared by every worker, deliberately. A node narrows
// inbound peers to this name, so what it authenticates is "a Mailyard
// delivery worker" - which is the only question it has to answer.
// Per-worker identities would mean re-enrolling every node whenever a
// worker was added.
type WorkerIdentity struct {
	Authority *Authority

	mu     sync.Mutex
	pair   *tls.Certificate
	roots  *x509.CertPool
	issued map[[sha256.Size]byte]bool
	loaded time.Time
}

// reloadAfter bounds how long a loaded pair is reused.
//
// The certificate is renewed by Authority.Client when it nears
// expiry, but a worker that cached it at boot would keep presenting
// the old one until restarted. Ten minutes is far shorter than the
// renewal window and costs one query.
const reloadAfter = 10 * time.Minute

// WorkerTLS builds the config for dialling one node.
//
// ServerName is the node's host, so the node's certificate is checked
// against the name we actually dialled - which is what stops a node
// certificate being usable for any node other than the one it names.
func (w *WorkerIdentity) WorkerTLS(ctx context.Context, host string) (*tls.Config, error) {
	pair, roots, issued, err := w.load(ctx)
	if err != nil {
		return nil, err
	}

	return &tls.Config{
		Certificates: []tls.Certificate{*pair},
		RootCAs:      roots,
		ServerName:   host,
		// TLS 1.3 on a link whose only participants are our own
		// processes. There is nothing old to be compatible with.
		MinVersion: tls.VersionTLS13,
		// Runs AFTER the chain and the name were verified, and asks the
		// one thing those cannot: is this certificate still on the
		// authority's record. A removed node keeps a leaf that verifies
		// for up to 90 days, and this is what makes removal mean
		// something before then - see Authority.Issued.
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 || !issued[sha256.Sum256(rawCerts[0])] {
				return errors.New("relay node certificate is not on the authority's record - the node was removed")
			}

			return nil
		},
	}, nil
}

func (w *WorkerIdentity) load(ctx context.Context) (*tls.Certificate, *x509.CertPool, map[[sha256.Size]byte]bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.pair != nil && time.Since(w.loaded) < reloadAfter {
		return w.pair, w.roots, w.issued, nil
	}

	if w.Authority == nil {
		return nil, nil, nil, errors.New("relay nodes are not configured on this process")
	}

	client, err := w.Authority.Client(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	pair, err := tls.X509KeyPair([]byte(client.CertPEM), []byte(client.KeyPEM))
	if err != nil {
		return nil, nil, nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(client.CAPEM)) {
		return nil, nil, nil, errors.New("relay ca bundle did not parse")
	}

	issued, err := w.Authority.Issued(ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	w.pair, w.roots, w.issued, w.loaded = &pair, roots, issued, time.Now()

	return w.pair, w.roots, w.issued, nil
}
