// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package apikey

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/yousysadmin/mailyard/internal/database"
	akmodel "github.com/yousysadmin/mailyard/internal/models/apikey"
)

// AdminStore persists platform credentials.
//
// No projID on any method, and that absence is the point: these govern
// the installation, so there is nothing to scope them by. The tenant
// store next door scopes every single query on project_id first, and
// mixing the two shapes in one type is how a query eventually forgets.
type AdminStore struct {
	database.Base
}

// NewAdminStore builds a AdminStore on db.
func NewAdminStore(db *sql.DB) *AdminStore {
	return &AdminStore{Base: database.NewBase(db)}
}

const adminKeySelect = `
SELECT id, created_by, name, key_hash, key_prefix, allowed_ips,
       revoked, expires_at, last_used_at, created_at
FROM admin_api_keys`

// Get returns one API key by id, or nil when there is no such row.
func (s *AdminStore) Get(ctx context.Context, id string) (*akmodel.Admin, error) {
	row := s.QueryRow(ctx, adminKeySelect+` WHERE id = ?`, id)
	k, err := scanAdminKey(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return k, err
}

// GetByPrefix is the auth lookup.
func (s *AdminStore) GetByPrefix(ctx context.Context, prefix string) (*akmodel.Admin, error) {
	row := s.QueryRow(ctx, adminKeySelect+` WHERE key_prefix = ?`, prefix)
	k, err := scanAdminKey(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return k, err
}

// List returns every API key.
func (s *AdminStore) List(ctx context.Context) ([]*akmodel.Admin, error) {
	rows, err := s.Query(ctx, adminKeySelect+` ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*akmodel.Admin
	for rows.Next() {
		k, err := scanAdminKey(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, k)
	}

	return out, rows.Err()
}

// Put inserts the API key, or updates the row when its id already
// exists.
func (s *AdminStore) Put(ctx context.Context, k *akmodel.Admin) error {
	if k.CreatedAt.IsZero() {
		k.CreatedAt = time.Now().UTC()
	}

	_, err := s.Exec(ctx, `
        INSERT INTO admin_api_keys (
            id, created_by, name, key_hash, key_prefix, allowed_ips,
            revoked, expires_at, last_used_at, created_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            name        = excluded.name,
            allowed_ips = excluded.allowed_ips,
            revoked     = excluded.revoked,
            expires_at  = excluded.expires_at
    `,
		k.ID, k.CreatedBy, k.Name, k.KeyHash, k.KeyPrefix,
		database.MustJSON(k.AllowedIPs), k.Revoked,
		database.NullTime(k.ExpiresAt), database.NullTime(k.LastUsedAt), k.CreatedAt,
	)

	return err
}

// Revoke disables the key without deleting it, so the audit trail
// keeps who held it.
func (s *AdminStore) Revoke(ctx context.Context, id string) error {
	_, err := s.Exec(ctx, `UPDATE admin_api_keys SET revoked = TRUE WHERE id = ?`, id)

	return err
}

// Delete removes one API key by id.
func (s *AdminStore) Delete(ctx context.Context, id string) error {
	_, err := s.Exec(ctx, `DELETE FROM admin_api_keys WHERE id = ?`, id)

	return err
}

// TouchLastUsed stamps when the key last authenticated. Unscoped: the
// key decides the project.
func (s *AdminStore) TouchLastUsed(ctx context.Context, id string, t time.Time) error {
	_, err := s.Exec(ctx, `UPDATE admin_api_keys SET last_used_at = ? WHERE id = ?`, t, id)

	return err
}

func scanAdminKey(r interface{ Scan(...any) error }) (*akmodel.Admin, error) {
	var k akmodel.Admin
	var ips string
	var expires, lastUsed sql.NullTime
	if err := r.Scan(&k.ID, &k.CreatedBy, &k.Name, &k.KeyHash, &k.KeyPrefix,
		&ips, &k.Revoked, &expires, &lastUsed, &k.CreatedAt); err != nil {
		return nil, err
	}

	database.MustUnmarshalJSON(ips, &k.AllowedIPs)
	if expires.Valid {
		k.ExpiresAt = new(expires.Time)
	}

	if lastUsed.Valid {
		k.LastUsedAt = new(lastUsed.Time)
	}

	return &k, nil
}
