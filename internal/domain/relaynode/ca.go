// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relaynode

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/relayca"
	certmodel "github.com/yousysadmin/mailyard/internal/models/certificate"
)

// CertStore is the persistence the authority needs.
type CertStore interface {
	Get(ctx context.Context, scope, name string) (*certmodel.Certificate, error)
	Put(ctx context.Context, c *certmodel.Certificate) error
	Delete(ctx context.Context, scope, name string) error
	ListScope(ctx context.Context, scope string) ([]*certmodel.Certificate, error)
}

// Authority loads the relay CA, minting it the first time anything
// asks.
//
// Minted on first use rather than at boot, because an installation
// that never runs a node should never generate one. A key that exists
// is a key that can leak, and the cheapest way not to leak one is not
// to have it.
type Authority struct {
	Store      CertStore
	CommonName string
	ClientCN   string
	// Log carries the one thing here worth saying out loud: a
	// certificate that was issued and not recorded.
	Log          *slog.Logger
	mu           sync.Mutex
	cached       *relayca.CA
	cachedClient *ClientPair
}

// ClientPair is the certificate every delivery worker presents when
// connecting to a node.
type ClientPair struct {
	CertPEM string
	KeyPEM  string
	CAPEM   string
}

func (a *Authority) commonName() string {
	if a.CommonName == "" {
		return "Mailyard Relay CA"
	}

	return a.CommonName
}

func (a *Authority) clientCN() string {
	if a.ClientCN == "" {
		return "mailyard-worker"
	}

	return a.ClientCN
}

// CA returns the authority, creating it on first call.
//
// The lock is held across the read-check-create sequence on purpose.
// Two api nodes handling two enrolments at the same instant would
// otherwise each see no CA and each mint one, and the second write
// wins - leaving the first node holding a certificate signed by an
// authority nobody trusts any more. Within a process this closes it
// outright - across processes the store write is last-wins, which is
// why minting happens once and early rather than per request.
func (a *Authority) CA(ctx context.Context) (*relayca.CA, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cached != nil {
		return a.cached, nil
	}

	rec, err := a.Store.Get(ctx, certmodel.ScopeRelayCA, "")
	if err != nil {
		return nil, fmt.Errorf("load relay ca: %w", err)
	}

	if rec != nil && rec.CertPEM != "" && rec.Data != "" {
		ca, err := relayca.Load(rec.CertPEM, rec.Data)
		if err != nil {
			return nil, fmt.Errorf("load relay ca: %w", err)
		}
		a.cached = ca

		return ca, nil
	}

	certPEM, keyPEM, err := relayca.Generate(a.commonName(), time.Now())
	if err != nil {
		return nil, err
	}

	if err := a.Store.Put(ctx, &certmodel.Certificate{
		Scope: certmodel.ScopeRelayCA, Data: keyPEM, CertPEM: certPEM,
	}); err != nil {
		return nil, fmt.Errorf("store relay ca: %w", err)
	}
	ca, err := relayca.Load(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	a.cached = ca

	return ca, nil
}

// Client returns the worker client pair, minting it on first call.
//
// One identity for every worker rather than one per node. A node
// narrows inbound peers by this name, so what it authenticates is
// "a Mailyard delivery worker" - which is the whole question it has
// to answer. Per-worker identities would mean re-enrolling every node
// each time a worker is added.
func (a *Authority) Client(ctx context.Context) (*ClientPair, error) {
	ca, err := a.CA(ctx)
	if err != nil {
		return nil, err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cachedClient != nil && !expiringSoon(a.cachedClient.CertPEM) {
		return a.cachedClient, nil
	}

	rec, err := a.Store.Get(ctx, certmodel.ScopeRelayClient, "")
	if err != nil {
		return nil, fmt.Errorf("load relay client certificate: %w", err)
	}

	if rec != nil && rec.CertPEM != "" && rec.Data != "" && !expiringSoon(rec.CertPEM) {
		pair := &ClientPair{CertPEM: rec.CertPEM, KeyPEM: rec.Data, CAPEM: ca.CertPEM()}
		a.cachedClient = pair

		return pair, nil
	}

	certPEM, keyPEM, err := ca.IssuePair(relayca.RoleClient, a.clientCN(), nil, time.Now())
	if err != nil {
		return nil, err
	}

	if err := a.Store.Put(ctx, &certmodel.Certificate{
		Scope: certmodel.ScopeRelayClient, Data: keyPEM, CertPEM: certPEM,
	}); err != nil {
		return nil, fmt.Errorf("store relay client certificate: %w", err)
	}
	pair := &ClientPair{CertPEM: certPEM, KeyPEM: keyPEM, CAPEM: ca.CertPEM()}
	a.cachedClient = pair

	return pair, nil
}

// ClientName is the SAN a node should accept inbound peers on.
func (a *Authority) ClientName() string { return a.clientCN() }

// Reset destroys the authority. Every node falls off the pool.
//
// This is the emergency lever, and it is the reason the relay
// authority is in the certificates admin at all rather than being a
// row that lives its own life: an operator who suspects the key has to
// be able to end it without a shell and a psql prompt.
//
// THREE scopes go, not one. Deleting only the authority leaves the
// shared worker client pair signed by an authority that no longer
// exists, and Client only reissues that when it is EXPIRING - so
// workers would keep presenting a certificate no re-enrolled node can
// verify, and the fleet would never come back. The issued node
// certificates go too: they are records of leaves signed by a key that
// is gone, and a re-enrolling node writes a fresh one.
//
// What comes next needs nothing done to it. CA mints a new authority
// the first time anything asks, and every node re-enrols with the
// token it already has.
func (a *Authority) Reset(ctx context.Context) (nodeCertificates int, err error) {
	issued, err := a.Store.ListScope(ctx, certmodel.ScopeRelayNode)
	if err != nil {
		return 0, fmt.Errorf("list issued node certificates: %w", err)
	}
	for _, rec := range issued {
		if derr := a.Store.Delete(ctx, certmodel.ScopeRelayNode, rec.Name); derr != nil {
			return 0, fmt.Errorf("delete issued node certificate: %w", derr)
		}
	}
	// The client pair before the authority: interrupted between the
	// two, a missing authority is minted again on next use, where a
	// client pair signed by a dead one is kept until it expires.
	if err := a.Store.Delete(ctx, certmodel.ScopeRelayClient, ""); err != nil {
		return 0, fmt.Errorf("delete relay client certificate: %w", err)
	}

	if err := a.Store.Delete(ctx, certmodel.ScopeRelayCA, ""); err != nil {
		return 0, fmt.Errorf("delete relay ca: %w", err)
	}

	a.mu.Lock()
	a.cached, a.cachedClient = nil, nil
	a.mu.Unlock()

	return len(issued), nil
}

// SignNode signs a node's certificate request and RECORDS what it
// issued.
//
// hosts are the names WE decided this node may answer to, never the
// ones the request asked for - see relayca.SignRequest.
//
// The record is the public half only, keyed by node id - the node
// keeps its private key, which is the point of enrolling with a
// request. This scope is the one place in the table with a certificate
// and no key.
//
// A failure to record is logged and not returned - refusing an
// enrolment over a bookkeeping row would take a node offline to keep a
// list tidy.
func (a *Authority) SignNode(ctx context.Context, nodeID, csrPEM, cn string, hosts []string) (certPEM, caPEM string, err error) {
	ca, err := a.CA(ctx)
	if err != nil {
		return "", "", err
	}
	certPEM, err = ca.SignRequest(csrPEM, relayca.RoleNode, cn, hosts, time.Now())
	if err != nil {
		return "", "", err
	}

	if nodeID != "" {
		if perr := a.Store.Put(ctx, &certmodel.Certificate{
			Scope: certmodel.ScopeRelayNode, Name: nodeID, CertPEM: certPEM,
		}); perr != nil {
			a.recordFailed(nodeID, perr)
		}
	}

	return certPEM, ca.CertPEM(), nil
}

// ForgetNode drops the record of what was issued to one node, for when
// that node is un-enrolled. Without it the issued list only ever grows -
// and since Issued is what a worker checks a node's certificate against,
// this is also the revocation: the leaf stays valid for its 90 days, and
// no worker of ours will complete a handshake with it.
func (a *Authority) ForgetNode(ctx context.Context, nodeID string) error {
	return a.Store.Delete(ctx, certmodel.ScopeRelayNode, nodeID)
}

// Issued returns the SHA-256 fingerprints of every node certificate on
// record. A certificate that verifies against the CA but is not here was
// issued to a node that has since been removed, and a worker refuses it.
// There is no CRL and no OCSP on this private authority, so the record of
// issuance IS the revocation list, read fresh by each worker on the same
// schedule it reloads its own pair.
func (a *Authority) Issued(ctx context.Context) (map[[sha256.Size]byte]bool, error) {
	rows, err := a.Store.ListScope(ctx, certmodel.ScopeRelayNode)
	if err != nil {
		return nil, err
	}

	out := make(map[[sha256.Size]byte]bool, len(rows))
	for _, row := range rows {
		crt, err := relayca.ParseCertificate(row.CertPEM)
		if err != nil {
			// An unreadable record cannot be matched, and refusing every
			// node over one bad row would be the wrong failure. Logged,
			// skipped: that node re-enrols if it is ever refused.
			a.recordFailed(row.Name, err)
			continue
		}

		out[sha256.Sum256(crt.Raw)] = true
	}

	return out, nil
}

func (a *Authority) recordFailed(nodeID string, err error) {
	if a.Log != nil {
		a.Log.Warn("relay ca: could not record the certificate issued to a node",
			"node_id", nodeID, "err", err)
	}
}

// expiringSoon reports whether a certificate is close enough to its
// expiry that it should be replaced now.
//
// Unparseable counts as expiring: a certificate we cannot read is one
// we cannot rely on, and reissuing is cheap.
func expiringSoon(certPEM string) bool {
	crt, err := relayca.ParseCertificate(certPEM)
	if err != nil {
		return true
	}

	return time.Until(crt.NotAfter) < relayca.RenewBefore
}
