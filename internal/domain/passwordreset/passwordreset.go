// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package passwordreset persists the single-use tokens behind the
// forgot-password flow. The HTTP surface lives in domain/auth
// (the routes hang off /api/auth) - this package is storage only.
package passwordreset

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/yousysadmin/mailyard/internal/database"
	prmodel "github.com/yousysadmin/mailyard/internal/models/passwordreset"
)

// Store persists single-use password reset tokens. Not project scoped
// - these belong to the installation, not to a tenant.
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
FROM password_resets`

// GetByHash is the redemption lookup. Not user scoped - the token IS
// the claim of identity.
func (s *Store) GetByHash(ctx context.Context, hash string) (*prmodel.Token, error) {
	row := s.QueryRow(ctx, tokenSelect+` WHERE token_hash = ?`, hash)
	t, err := scanToken(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return t, err
}

// Put inserts the reset token, or updates the row when its id already
// exists.
func (s *Store) Put(ctx context.Context, t *prmodel.Token) error {
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}

	_, err := s.Exec(ctx, `
        INSERT INTO password_resets (
            id, user_id, token_hash, expires_at, used_at, created_at, request_ip
        ) VALUES (?, ?, ?, ?, ?, ?, ?)
    `, t.ID, t.UserID, t.TokenHash, t.ExpiresAt,
		database.NullTime(t.UsedAt), t.CreatedAt, t.RequestIP)

	return err
}

// MarkUsed burns one token and reports whether this call is the one
// that burned it.
//
// The bool is the single-use guarantee. `used_at IS NULL` makes the
// statement conditional, so of two simultaneous redemptions of the
// same link only one can match a row - but only if the caller checks.
// Discarding the result turns an atomic claim back into a
// read-then-write, and lets a failed write pass for success, leaving
// the token live for the rest of its TTL. Same reasoning as
// UserStore.ClaimTOTPStep.
func (s *Store) MarkUsed(ctx context.Context, id string, at time.Time) (bool, error) {
	res, err := s.Exec(ctx, `UPDATE password_resets SET used_at = ? WHERE id = ? AND used_at IS NULL`, at, id)
	if err != nil {
		return false, err
	}

	n, err := res.RowsAffected()

	return n > 0, err
}

// InvalidateForUser burns every outstanding token for a user. Called
// after a successful reset so a second link mailed earlier (or a
// stolen one) is dead, and whenever the password changes by any other
// route.
func (s *Store) InvalidateForUser(ctx context.Context, userID string, at time.Time) error {
	_, err := s.Exec(ctx, `UPDATE password_resets SET used_at = ? WHERE user_id = ? AND used_at IS NULL`, at, userID)

	return err
}

// CountRecentForUser reports how many tokens a user has been issued
// since t. Caps how often one account can be mailed a reset link.
func (s *Store) CountRecentForUser(ctx context.Context, userID string, since time.Time) (int, error) {
	var n int
	err := s.QueryRow(ctx, `SELECT COUNT(*) FROM password_resets WHERE user_id = ? AND created_at >= ?`,
		userID, since).Scan(&n)

	return n, err
}

// DeleteExpired purges spent and stale rows. Called by the retention
// job.
func (s *Store) DeleteExpired(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.Exec(ctx, `DELETE FROM password_resets WHERE expires_at < ? OR used_at IS NOT NULL`, before)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

func scanToken(r interface{ Scan(...any) error }) (*prmodel.Token, error) {
	var t prmodel.Token
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
