// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package certstore backs golang.org/x/crypto/acme/autocert with the
// certificates table instead of a directory.
//
// autocert's DirCache is per node, and that is a real problem rather
// than a tidiness one. Several nodes serving the same hostname each
// see an empty cache, so each orders its own certificate, and Let's
// Encrypt allows five duplicate certificates per week for an
// identical name set - the sixth node does not get one and does not
// serve TLS. Worse, the ones that succeeded renew independently
// forever, multiplying every future order by the node count.
//
// One shared cache turns that into a single certificate the whole
// cluster reads, which is also what makes it possible to add a node
// without going anywhere near Let's Encrypt.
package certstore

import (
	"context"
	"encoding/pem"
	"errors"
	"strings"

	"golang.org/x/crypto/acme/autocert"

	certmodel "github.com/yousysadmin/mailyard/internal/models/certificate"
)

// Store is the persistence this package needs. An interface so the
// cache can be tested without a database, and so core/ does not
// import domain/.
type Store interface {
	Get(ctx context.Context, scope, name string) (*certmodel.Certificate, error)
	Put(ctx context.Context, c *certmodel.Certificate) error
	PutIfAbsent(ctx context.Context, c *certmodel.Certificate) (bool, error)
	Delete(ctx context.Context, scope, name string) error
}

// Cache is an autocert.Cache over the certificates table.
type Cache struct {
	Store Store
}

// compile-time proof we still satisfy the interface autocert wants.
var _ autocert.Cache = (*Cache)(nil)

// Get returns a cached entry, or autocert.ErrCacheMiss.
//
// The miss must be exactly that error and nothing else: autocert
// treats any other failure as fatal and gives up on the host, so a
// database blip returned as a miss would silently re-order a
// certificate that already exists.
func (c *Cache) Get(ctx context.Context, key string) ([]byte, error) {
	rec, err := c.Store.Get(ctx, certmodel.ScopeACME, key)
	if err != nil {
		return nil, err
	}

	if rec == nil || rec.Data == "" {
		return nil, autocert.ErrCacheMiss
	}

	return []byte(rec.Data), nil
}

// Put stores an entry.
//
// The blob autocert hands over is a private key followed by the
// certificate chain, all PEM. It goes into the sealed column whole.
// The chain alone is copied into the public column so the console can
// show an expiry - splitting it here is the only reason this is not a
// two-line function, and putting the whole blob there instead would
// publish the key.
func (c *Cache) Put(ctx context.Context, key string, data []byte) error {
	return c.Store.Put(ctx, &certmodel.Certificate{
		Scope:   certmodel.ScopeACME,
		Name:    key,
		Data:    string(data),
		CertPEM: certificatesOnly(data),
	})
}

// Delete removes one certstore by id.
func (c *Cache) Delete(ctx context.Context, key string) error {
	return c.Store.Delete(ctx, certmodel.ScopeACME, key)
}

// certificatesOnly keeps the CERTIFICATE blocks of a PEM bundle and
// drops everything else, which in an autocert entry means the private
// key.
func certificatesOnly(data []byte) string {
	var out strings.Builder
	rest := data
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}

		if block.Type != "CERTIFICATE" {
			continue
		}

		if err := pem.Encode(&out, block); err != nil {
			// Encoding into a strings.Builder does not fail, but
			// swallowing the error silently would be a lie if it ever
			// did - an empty public half is the safe answer.
			return ""
		}
	}

	return out.String()
}

// IsMiss reports the sentinel autocert uses for "not cached". Exposed
// so callers do not have to import autocert to tell a miss from a
// failure.
func IsMiss(err error) bool { return errors.Is(err, autocert.ErrCacheMiss) }
