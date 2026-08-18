// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package apikey is the persistence and handler surface for machine
// API keys. Console routes live behind requireAuth + requireProject
// in server/routes.go. The auth-side lookup (GetByPrefix + hash
// compare) is consumed by machineAuth, guarding /api/v1.
package apikey

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/yousysadmin/mailyard/internal/database"
	akmodel "github.com/yousysadmin/mailyard/internal/models/apikey"
)

// Store persists machine API keys. Project scoped: a method taking
// projID answers nothing for a row another project owns.
type Store struct {
	database.Base
}

// NewStore builds the store on db, wiring the shared query helpers
// through database.Base.
func NewStore(db *sql.DB) *Store {
	return &Store{Base: database.NewBase(db)}
}

const keySelect = `
SELECT id, project_id, created_by, name, key_hash, key_prefix, permissions,
       allowed_ips, sandbox, revoked, expires_at, last_used_at, created_at
FROM api_keys`

// Get returns one API key within projID, or nil when there is no such
// row.
func (s *Store) Get(ctx context.Context, projID, id string) (*akmodel.Key, error) {
	row := s.QueryRow(ctx, keySelect+` WHERE project_id = ? AND id = ?`, projID, id)
	k, err := scanKey(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return k, err
}

// GetByPrefix is the auth lookup and deliberately not project
// scoped - the key itself decides the project.
func (s *Store) GetByPrefix(ctx context.Context, prefix string) (*akmodel.Key, error) {
	row := s.QueryRow(ctx, keySelect+` WHERE key_prefix = ?`, prefix)
	k, err := scanKey(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return k, err
}

// List returns every API key in projID.
func (s *Store) List(ctx context.Context, projID string) ([]*akmodel.Key, error) {
	rows, err := s.Query(ctx, keySelect+` WHERE project_id = ? ORDER BY created_at ASC`, projID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*akmodel.Key
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, k)
	}

	return out, rows.Err()
}

// Put inserts the API key, or updates the row when its id already
// exists.
func (s *Store) Put(ctx context.Context, k *akmodel.Key) error {
	if k.CreatedAt.IsZero() {
		k.CreatedAt = time.Now().UTC()
	}

	_, err := s.Exec(ctx, `
        INSERT INTO api_keys (
            id, project_id, created_by, name, key_hash, key_prefix, permissions,
            allowed_ips, sandbox, revoked, expires_at, last_used_at, created_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            name        = excluded.name,
            permissions = excluded.permissions,
            allowed_ips = excluded.allowed_ips,
            sandbox     = excluded.sandbox,
            revoked     = excluded.revoked,
            expires_at  = excluded.expires_at
    `,
		k.ID, k.ProjectID, k.CreatedBy, k.Name, k.KeyHash, k.KeyPrefix,
		database.MustJSON(k.Permissions), database.MustJSON(k.AllowedIPs), k.Sandbox, k.Revoked,
		database.NullTime(k.ExpiresAt), database.NullTime(k.LastUsedAt), k.CreatedAt,
	)

	return err
}

// Revoke disables the key without deleting it, so the audit trail
// keeps who held it.
func (s *Store) Revoke(ctx context.Context, projID, id string) error {
	_, err := s.Exec(ctx, `UPDATE api_keys SET revoked = TRUE WHERE project_id = ? AND id = ?`, projID, id)

	return err
}

// Delete removes one API key from projID.
func (s *Store) Delete(ctx context.Context, projID, id string) error {
	_, err := s.Exec(ctx, `DELETE FROM api_keys WHERE project_id = ? AND id = ?`, projID, id)

	return err
}

// TouchLastUsed stamps when the key last authenticated. Unscoped: the
// key decides the project.
func (s *Store) TouchLastUsed(ctx context.Context, id string, t time.Time) error {
	_, err := s.Exec(ctx, `UPDATE api_keys SET last_used_at = ? WHERE id = ?`, t, id)

	return err
}

func scanKey(r interface{ Scan(...any) error }) (*akmodel.Key, error) {
	var k akmodel.Key
	var perms, ips string
	var expires, lastUsed sql.NullTime
	if err := r.Scan(&k.ID, &k.ProjectID, &k.CreatedBy, &k.Name, &k.KeyHash,
		&k.KeyPrefix, &perms, &ips, &k.Sandbox, &k.Revoked, &expires, &lastUsed, &k.CreatedAt); err != nil {
		return nil, err
	}

	database.MustUnmarshalJSON(perms, &k.Permissions)
	database.MustUnmarshalJSON(ips, &k.AllowedIPs)
	if expires.Valid {
		k.ExpiresAt = new(expires.Time)
	}

	if lastUsed.Valid {
		k.LastUsedAt = new(lastUsed.Time)
	}

	return &k, nil
}

// Count reports how many rows the project holds, for plan caps.
func (s *Store) Count(ctx context.Context, projID string) (int, error) {
	var n int
	err := s.QueryRow(ctx, `SELECT COUNT(*) FROM api_keys WHERE project_id = ?`, projID).Scan(&n)

	return n, err
}
