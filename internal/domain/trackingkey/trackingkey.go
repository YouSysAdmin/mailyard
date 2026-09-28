// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package trackingkey persists the tracking keys a rekey retired, which
// go on verifying unsubscribe links. Written only by `mailyard rekey`,
// read once at boot.
package trackingkey

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/database"
)

type Store struct {
	database.Base
	crypto *crypto.Service
}

// NewStore builds the store on db. cr opens the sealed keys.
func NewStore(db *sql.DB, cr *crypto.Service) *Store {
	return &Store{Base: database.NewBase(db), crypto: cr}
}

// Retired returns every retired key, oldest first, unsealed.
func (s *Store) Retired(ctx context.Context) ([]string, error) {
	rows, err := s.Query(ctx, `SELECT key FROM tracking_keys ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var sealed string
		if err := rows.Scan(&sealed); err != nil {
			return nil, err
		}

		plain, err := s.crypto.Decrypt(sealed)
		if err != nil {
			return nil, fmt.Errorf("trackingkey: unseal: %w", err)
		}

		out = append(out, plain)
	}

	return out, rows.Err()
}
