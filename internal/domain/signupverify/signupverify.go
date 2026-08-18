// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package signupverify persists the single-use tokens behind signup
// email verification. The HTTP surface lives in domain/auth (the
// routes hang off /api/auth) - this package is storage only.
package signupverify

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/yousysadmin/mailyard/internal/database"
	svmodel "github.com/yousysadmin/mailyard/internal/models/signupverify"
)

// Store persists single-use signup verification tokens. Not project
// scoped - these belong to the installation, not to a tenant.
type Store struct {
	database.Base
}

// NewStore builds the store on db, wiring the shared query helpers
// through database.Base.
func NewStore(db *sql.DB) *Store {
	return &Store{Base: database.NewBase(db)}
}

const tokenSelect = `
SELECT id, user_id, token_hash, expires_at, used_at, created_at, request_ip
FROM signup_verifications`

// GetByHash is the redemption lookup. Not user scoped - the token IS
// the claim of identity.
func (s *Store) GetByHash(ctx context.Context, hash string) (*svmodel.Token, error) {
	row := s.QueryRow(ctx, tokenSelect+` WHERE token_hash = ?`, hash)
	t, err := scanToken(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return t, err
}

// Put inserts the verification token, or updates the row when its id
// already exists.
func (s *Store) Put(ctx context.Context, t *svmodel.Token) error {
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}

	_, err := s.Exec(ctx, `
        INSERT INTO signup_verifications (
            id, user_id, token_hash, expires_at, used_at, created_at, request_ip
        ) VALUES (?, ?, ?, ?, ?, ?, ?)
    `, t.ID, t.UserID, t.TokenHash, t.ExpiresAt,
		database.NullTime(t.UsedAt), t.CreatedAt, t.RequestIP)

	return err
}

// MarkUsed burns one token and reports whether this call is the one
// that burned it. Same conditional-UPDATE single-use guarantee as
// passwordreset.MarkUsed - the bool only counts if the caller checks.
func (s *Store) MarkUsed(ctx context.Context, id string, at time.Time) (bool, error) {
	res, err := s.Exec(ctx, `UPDATE signup_verifications SET used_at = ? WHERE id = ? AND used_at IS NULL`, at, id)
	if err != nil {
		return false, err
	}

	n, err := res.RowsAffected()

	return n > 0, err
}

// InvalidateForUser burns every outstanding token for a user. Called
// after a successful verification so an earlier link (or a stolen
// one) is dead, and when the account verifies by another route (OIDC
// sign-in, an admin marking it verified).
func (s *Store) InvalidateForUser(ctx context.Context, userID string, at time.Time) error {
	_, err := s.Exec(ctx, `UPDATE signup_verifications SET used_at = ? WHERE user_id = ? AND used_at IS NULL`, at, userID)

	return err
}

// CountRecentForUser reports how many tokens a user has been issued
// since t. Caps how often one account can be mailed a link.
func (s *Store) CountRecentForUser(ctx context.Context, userID string, since time.Time) (int, error) {
	var n int
	err := s.QueryRow(ctx, `SELECT COUNT(*) FROM signup_verifications WHERE user_id = ? AND created_at >= ?`,
		userID, since).Scan(&n)

	return n, err
}

// DeleteExpired purges spent and stale rows. Called by the retention
// job.
func (s *Store) DeleteExpired(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.Exec(ctx, `DELETE FROM signup_verifications WHERE expires_at < ? OR used_at IS NOT NULL`, before)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

func scanToken(r interface{ Scan(...any) error }) (*svmodel.Token, error) {
	var t svmodel.Token
	var used sql.NullTime
	if err := r.Scan(&t.ID, &t.UserID, &t.TokenHash, &t.ExpiresAt,
		&used, &t.CreatedAt, &t.RequestIP); err != nil {
		return nil, err
	}

	if used.Valid {
		t.UsedAt = new(used.Time)
	}

	return &t, nil
}
