// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package setting persists and serves platform-wide settings. Not
// project scoped - these are properties of the installation, and
// only a platform admin can read or change them.
package setting

import (
	"context"
	"database/sql"
	"time"

	"github.com/yousysadmin/mailyard/internal/database"
	smodel "github.com/yousysadmin/mailyard/internal/models/setting"
)

// Store persists platform setting overrides. Not project scoped -
// these belong to the installation, not to a tenant.
type Store struct {
	database.Base
}

// NewStore builds the store on db, wiring the shared query helpers
// through database.Base.
func NewStore(db *sql.DB) *Store {
	return &Store{Base: database.NewBase(db)}
}

// All returns every stored override. Keys with no row resolve to the
// registry default and are not returned here.
func (s *Store) All(ctx context.Context) ([]*smodel.Setting, error) {
	rows, err := s.Query(ctx, `
        SELECT key, value, type, updated_at, updated_by FROM settings ORDER BY key ASC`)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*smodel.Setting
	for rows.Next() {
		var m smodel.Setting
		if err := rows.Scan(&m.Key, &m.Value, &m.Type, &m.UpdatedAt, &m.UpdatedBy); err != nil {
			return nil, err
		}

		out = append(out, &m)
	}

	return out, rows.Err()
}

// Put upserts one override.
func (s *Store) Put(ctx context.Context, m *smodel.Setting) error {
	if m.UpdatedAt.IsZero() {
		m.UpdatedAt = time.Now().UTC()
	}

	_, err := s.Exec(ctx, `
        INSERT INTO settings (key, value, type, updated_at, updated_by)
        VALUES (?, ?, ?, ?, ?)
        ON CONFLICT(key) DO UPDATE SET
            value      = excluded.value,
            type       = excluded.type,
            updated_at = excluded.updated_at,
            updated_by = excluded.updated_by
    `, m.Key, m.Value, m.Type, m.UpdatedAt, m.UpdatedBy)

	return err
}

// Delete drops an override, restoring the registry default.
func (s *Store) Delete(ctx context.Context, key string) error {
	_, err := s.Exec(ctx, `DELETE FROM settings WHERE key = ?`, key)

	return err
}
