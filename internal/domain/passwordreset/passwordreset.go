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

// PutWithin inserts the token unless spent refuses it, and reports
// whether it did. spent is judged over the address behind every link
// the account was issued since a moment, under a per-account lock held
// to the insert, so concurrent requests cannot all pass one budget.
func (s *Store) PutWithin(ctx context.Context, t *prmodel.Token, since time.Time,
	spent func(ips []string) bool,
) (bool, error) {
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}

	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}

	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		s.Q(`SELECT pg_advisory_xact_lock(hashtext('password_resets'), hashtext(?::text))`), t.UserID); err != nil {
		return false, err
	}

	rows, err := tx.QueryContext(ctx,
		s.Q(`SELECT request_ip FROM password_resets WHERE user_id = ? AND created_at >= ?`), t.UserID, since)
	if err != nil {
		return false, err
	}

	defer func() { _ = rows.Close() }()

	var ips []string
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err != nil {
			return false, err
		}

		ips = append(ips, ip)
	}

	if err := rows.Err(); err != nil {
		return false, err
	}

	if spent(ips) {
		return false, nil
	}

	if _, err := tx.ExecContext(ctx, s.Q(`
        INSERT INTO password_resets (
            id, user_id, token_hash, expires_at, used_at, created_at, request_ip
        ) VALUES (?, ?, ?, ?, ?, ?, ?)
    `), t.ID, t.UserID, t.TokenHash, t.ExpiresAt,
		database.NullTime(t.UsedAt), t.CreatedAt, t.RequestIP); err != nil {
		return false, err
	}

	return true, tx.Commit()
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
