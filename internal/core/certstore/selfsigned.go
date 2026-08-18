// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package certstore

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/certgen"
	certmodel "github.com/yousysadmin/mailyard/internal/models/certificate"
)

// SelfSigned returns the installation's self-signed pair for hosts,
// generating and storing it the first time.
//
// The pair lives in the certificates table rather than on disk, for the
// reason the whole table exists: a certificate in a node's directory
// belongs to that node. Left there, two nodes generate their own and a
// client reaching first one and then the other sees two certificates
// under one hostname. For a self-signed setup, where the whole point is
// pinning the fingerprint you were handed, that is indistinguishable
// from an interception - and a single node does the same on every
// restart whenever its cache directory is not persisted, which is the
// ordinary state of a container.
//
// The key is the host list, so changing server.tls.fqdn generates a new
// pair rather than serving the old one for a name it does not cover.
func SelfSigned(ctx context.Context, store Store, hosts []string, alg string) (tls.Certificate, error) {
	if store == nil {
		return tls.Certificate{}, fmt.Errorf("certstore: no store")
	}

	name := selfSignedName(hosts, alg)

	if rec, err := store.Get(ctx, certmodel.ScopeSelfSigned, name); err != nil {
		return tls.Certificate{}, err
	} else if cert, ok := usable(rec); ok {
		return cert, nil
	}

	certPEM, keyPEM, err := certgen.MintLeaf(certgen.LeafRequest{
		Hosts: hosts,
		// Empty stays RSA-2048 for 180 days, which is what the library
		// this replaced defaulted to, so the listener's own self-signed
		// pair is unchanged apart from the Subject.
		Algorithm: alg,
		Validity:  selfSignedValidity,
	}, nil)
	if err != nil {
		return tls.Certificate{}, err
	}

	// DO NOTHING, not an upsert. Two nodes booting together both
	// generate - only one may store, and the loser re-reads rather than
	// overwriting a pair the winner may already be serving.
	won, err := store.PutIfAbsent(ctx, &certmodel.Certificate{
		Scope:   certmodel.ScopeSelfSigned,
		Name:    name,
		Data:    keyPEM + certPEM,
		CertPEM: certPEM,
	})
	if err != nil {
		return tls.Certificate{}, err
	}

	if !won {
		rec, err := store.Get(ctx, certmodel.ScopeSelfSigned, name)
		if err != nil {
			return tls.Certificate{}, err
		}

		if cert, ok := usable(rec); ok {
			return cert, nil
		}
		// Stored by somebody else and unreadable, or expired the
		// instant it landed. Serving the pair we just made is better
		// than failing to serve at all - it is valid, it is simply not
		// the one the other node has.
	}

	return tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
}

// selfSignedValidity is what go-tlsutils used before this called
// certgen, kept exactly so that replacing the generator does not
// silently shorten or lengthen what an existing installation serves.
const selfSignedValidity = 180 * 24 * time.Hour

// Replace overwrites the stored self-signed pair, so an operator can
// force a new one without finding the row. Returns what was generated.
func Replace(ctx context.Context, store Store, hosts []string, alg string) (tls.Certificate, error) {
	if store == nil {
		return tls.Certificate{}, fmt.Errorf("certstore: no store")
	}

	if err := store.Delete(ctx, certmodel.ScopeSelfSigned, selfSignedName(hosts, alg)); err != nil {
		return tls.Certificate{}, err
	}

	return SelfSigned(ctx, store, hosts, alg)
}

// usable parses a stored entry, rejecting one that has expired.
//
// An expired self-signed certificate is worse than none: the listener
// comes up, every client refuses it, and the log says nothing. Falling
// through to generation puts a working pair in place instead.
func usable(rec *certmodel.Certificate) (tls.Certificate, bool) {
	if rec == nil || rec.Data == "" {
		return tls.Certificate{}, false
	}

	cert, err := tls.X509KeyPair([]byte(rec.Data), []byte(rec.Data))
	if err != nil {
		return tls.Certificate{}, false
	}

	if cert.Leaf == nil {
		return tls.Certificate{}, false
	}

	if time.Now().After(cert.Leaf.NotAfter) {
		return tls.Certificate{}, false
	}

	return cert, true
}

// selfSignedName keys the entry by what it covers. Two installations
// sharing a database would still each get their own row per host set,
// and changing the FQDN mints a new pair rather than silently serving
// one that does not name the host.
func selfSignedName(hosts []string, alg string) string {
	if alg == "" {
		alg = "default"
	}

	return strings.Join(hosts, ",") + "|" + alg
}
