// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package passkey

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/yousysadmin/mailyard/internal/database"
	pkmodel "github.com/yousysadmin/mailyard/internal/models/passkey"
)

// Store persists enrolled WebAuthn credentials. Not project scoped -
// these belong to the installation, not to a tenant.
type Store struct {
	database.Base
}

// NewStore builds the store on db, wiring the shared query helpers
// through database.Base.
func NewStore(db *sql.DB) *Store {
	return &Store{Base: database.NewBase(db)}
}

const passkeySelect = `
SELECT id, user_id, credential_id, name, credential, created_at, last_used_at
FROM user_passkeys`

// ListForUser returns the account's enrolled passkeys, newest first.
func (s *Store) ListForUser(ctx context.Context, userID string) ([]*pkmodel.Passkey, error) {
	rows, err := s.Query(ctx, passkeySelect+` WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*pkmodel.Passkey
	for rows.Next() {
		m, err := scanPasskey(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, m)
	}

	return out, rows.Err()
}

// GetByCredential resolves the credential the authenticator asserted.
//
// Deliberately not user scoped: at this point in a discoverable login
// nobody has claimed an identity yet, the credential id IS the claim,
// and the signature over it is what makes it true. Every other lookup
// in this store scopes on the user.
func (s *Store) GetByCredential(ctx context.Context, credentialID string) (*pkmodel.Passkey, error) {
	row := s.QueryRow(ctx, passkeySelect+` WHERE credential_id = ?`, credentialID)
	m, err := scanPasskey(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return m, err
}

// Put inserts a newly enrolled passkey.
func (s *Store) Put(ctx context.Context, m *pkmodel.Passkey) error {
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}

	_, err := s.Exec(ctx, `
        INSERT INTO user_passkeys (
            id, user_id, credential_id, name, credential, created_at, last_used_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?)
    `, m.ID, m.UserID, m.CredentialID, m.Name, m.Credential,
		m.CreatedAt, database.NullTime(m.LastUsedAt))

	return err
}

// RecordUse saves the credential after a successful assertion and
// stamps last_used_at.
//
// The credential is rewritten because the sign counter advances on
// every use, and a stale counter is what makes the clone check on the
// NEXT login meaningless.
func (s *Store) RecordUse(ctx context.Context, id, credential string, at time.Time) error {
	_, err := s.Exec(ctx, `
        UPDATE user_passkeys SET credential = ?, last_used_at = ? WHERE id = ?
    `, credential, at, id)

	return err
}

// Rename changes the label. Scoped by user, so somebody else's
// passkey reads as missing rather than refused.
func (s *Store) Rename(ctx context.Context, userID, id, name string) (bool, error) {
	res, err := s.Exec(ctx, `UPDATE user_passkeys SET name = ? WHERE user_id = ? AND id = ?`, name, userID, id)
	if err != nil {
		return false, err
	}

	n, err := res.RowsAffected()

	return n > 0, err
}

// Delete removes one passkey. Scoped by user for the same reason.
func (s *Store) Delete(ctx context.Context, userID, id string) (bool, error) {
	res, err := s.Exec(ctx, `DELETE FROM user_passkeys WHERE user_id = ? AND id = ?`, userID, id)
	if err != nil {
		return false, err
	}

	n, err := res.RowsAffected()

	return n > 0, err
}

// DeleteAllForUser removes every passkey the account holds, for the
// admin whose user lost the only device that could sign in. Returns
// how many went, so the caller can say nothing was there rather than
// reporting a reset that did not happen.
func (s *Store) DeleteAllForUser(ctx context.Context, userID string) (int, error) {
	res, err := s.Exec(ctx, `DELETE FROM user_passkeys WHERE user_id = ?`, userID)
	if err != nil {
		return 0, err
	}

	n, err := res.RowsAffected()

	return int(n), err
}

// CountForUser reports how many the account holds.
func (s *Store) CountForUser(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.QueryRow(ctx, `SELECT COUNT(*) FROM user_passkeys WHERE user_id = ?`, userID).Scan(&n)

	return n, err
}

func scanPasskey(r interface{ Scan(...any) error }) (*pkmodel.Passkey, error) {
	var m pkmodel.Passkey
	var lastUsed sql.NullTime
	if err := r.Scan(&m.ID, &m.UserID, &m.CredentialID, &m.Name, &m.Credential,
		&m.CreatedAt, &lastUsed); err != nil {
		return nil, err
	}

	if lastUsed.Valid {
		m.LastUsedAt = new(lastUsed.Time)
	}

	return &m, nil
}
