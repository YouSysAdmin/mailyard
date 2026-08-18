// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package certificate

import (
	"context"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"errors"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/database"
	certmodel "github.com/yousysadmin/mailyard/internal/models/certificate"
)

// Store persists certificates and their sealed private halves.
type Store struct {
	database.Base
	crypto *crypto.Service
}

// NewStore builds the store on db, wiring the shared query helpers
// through database.Base.
func NewStore(db *sql.DB, cr *crypto.Service) *Store {
	return &Store{Base: database.NewBase(db), crypto: cr}
}

const certSelect = `
SELECT scope, name, data, cert_pem, not_after, created_at, updated_at
FROM certificates`

// Get returns one entry with Data already decrypted, or (nil, nil).
func (s *Store) Get(ctx context.Context, scope, name string) (*certmodel.Certificate, error) {
	row := s.QueryRow(ctx, certSelect+` WHERE scope = ? AND name = ?`, scope, name)
	c, err := s.scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return c, err
}

// GetPublic is Get without unsealing the private half.
//
// For a caller that wants only what is already public - publishing an
// authority so a client can trust it, above all. It goes through
// scanPublic, which is the same split ListScope relies on: a path that
// never decrypts cannot leak a key however it is later changed.
func (s *Store) GetPublic(ctx context.Context, scope, name string) (*certmodel.Certificate, error) {
	row := s.QueryRow(ctx, certSelect+` WHERE scope = ? AND name = ?`, scope, name)
	c, err := s.scanPublic(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	// Cleared rather than returned sealed. A caller handed ciphertext
	// in a field named Data is a caller that may pass it on as if it
	// were the key.
	c.Data = ""

	return c, nil
}

// ListScope returns every entry in a scope, without decrypting the
// sealed half. For the console, which shows what exists and when it
// expires and has no business holding private keys.
func (s *Store) ListScope(ctx context.Context, scope string) ([]*certmodel.Certificate, error) {
	rows, err := s.Query(ctx,
		`SELECT scope, name, '', cert_pem, not_after, created_at, updated_at
		 FROM certificates WHERE scope = ? ORDER BY name ASC`, scope)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	var out []*certmodel.Certificate
	for rows.Next() {
		c, err := s.scanPublic(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, c)
	}

	return out, rows.Err()
}

// Put writes or replaces an entry. not_after is derived from CertPEM
// here rather than being passed in, so a caller cannot store an
// expiry that disagrees with the certificate it belongs to.
func (s *Store) Put(ctx context.Context, c *certmodel.Certificate) error {
	sealed, err := s.crypto.Encrypt(c.Data)
	if err != nil {
		return err
	}

	notAfter := notAfterOf(c.CertPEM)
	now := time.Now().UTC()

	_, err = s.Exec(ctx, `
		INSERT INTO certificates (scope, name, data, cert_pem, not_after, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (scope, name) DO UPDATE SET
			data = EXCLUDED.data,
			cert_pem = EXCLUDED.cert_pem,
			not_after = EXCLUDED.not_after,
			updated_at = EXCLUDED.updated_at`,
		c.Scope, c.Name, sealed, c.CertPEM, notAfter, now, now)

	return err
}

// PutIfAbsent writes an entry only when the key is free, and reports
// whether it was the one that wrote it.
//
// This is what makes generating a shared secret safe on more than one
// node. Put replaces, so two nodes starting together would each
// generate a self-signed pair and the second would overwrite the
// first - after which the two nodes serve different certificates
// until something restarts them, and any peer that pinned one sees a
// mismatch from the other. With DO NOTHING the loser simply re-reads
// and uses what the winner stored.
func (s *Store) PutIfAbsent(ctx context.Context, c *certmodel.Certificate) (bool, error) {
	sealed, err := s.crypto.Encrypt(c.Data)
	if err != nil {
		return false, err
	}

	now := time.Now().UTC()

	res, err := s.Exec(ctx, `
		INSERT INTO certificates (scope, name, data, cert_pem, not_after, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (scope, name) DO NOTHING`,
		c.Scope, c.Name, sealed, c.CertPEM, notAfterOf(c.CertPEM), now, now)
	if err != nil {
		return false, err
	}

	n, err := res.RowsAffected()

	return n > 0, err
}

// Delete removes one certificate by id.
func (s *Store) Delete(ctx context.Context, scope, name string) error {
	_, err := s.Exec(ctx, `DELETE FROM certificates WHERE scope = ? AND name = ?`, scope, name)

	return err
}

// ExpiringBefore lists entries that run out before t, across scopes.
// The sealed half is left alone - this answers "what needs renewing",
// not "give me the key".
func (s *Store) ExpiringBefore(ctx context.Context, t time.Time) ([]*certmodel.Certificate, error) {
	rows, err := s.Query(ctx,
		`SELECT scope, name, '', cert_pem, not_after, created_at, updated_at
		 FROM certificates WHERE not_after IS NOT NULL AND not_after < ?
		 ORDER BY not_after ASC`, t)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	var out []*certmodel.Certificate
	for rows.Next() {
		c, err := s.scanPublic(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, c)
	}

	return out, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func (s *Store) scan(row scanner) (*certmodel.Certificate, error) {
	c, err := s.scanPublic(row)
	if err != nil {
		return nil, err
	}

	plain, err := s.crypto.Decrypt(c.Data)
	if err != nil {
		return nil, err
	}

	c.Data = plain

	return c, nil
}

// scanPublic reads the row and leaves Data exactly as stored. Callers
// that want the plaintext go through scan - keeping the two apart is
// what lets the console list certificates without ever touching the
// encryption key.
func (s *Store) scanPublic(row scanner) (*certmodel.Certificate, error) {
	var c certmodel.Certificate
	var notAfter sql.NullTime
	if err := row.Scan(&c.Scope, &c.Name, &c.Data, &c.CertPEM, &notAfter,
		&c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}

	if notAfter.Valid {
		t := notAfter.Time
		c.NotAfter = &t
	}

	return &c, nil
}

// notAfterOf finds the earliest expiry among the certificates in a
// PEM bundle.
//
// Earliest, not the leaf's: a bundle is only usable while every
// certificate in it is valid, and an intermediate that expires first
// breaks the chain just as thoroughly. Unparseable input yields no
// expiry rather than an error - an ACME cache blob is opaque by
// design and must still be storable.
func notAfterOf(certPEM string) *time.Time {
	rest := []byte(certPEM)
	var earliest *time.Time
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}

		if block.Type != "CERTIFICATE" {
			continue
		}

		crt, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}

		if earliest == nil || crt.NotAfter.Before(*earliest) {
			t := crt.NotAfter.UTC()
			earliest = &t
		}
	}

	return earliest
}
