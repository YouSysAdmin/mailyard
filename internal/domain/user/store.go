package user

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/yousysadmin/mailyard/internal/database"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
)

// Store persists accounts. Not project scoped - these belong to the
// installation, not to a tenant.
type Store struct {
	database.Base
}

// NewStore binds the user store to db.
func NewStore(db *sql.DB) *Store {
	return &Store{Base: database.NewBase(db)}
}

const userSelect = `
SELECT id, email, password_hash, account_type, admin, disabled,
       email_verified, totp_secret, totp_enabled, created_at, last_login_at,
       (SELECT COUNT(*) FROM user_passkeys p WHERE p.user_id = users.id)
FROM users`

// Get fetches a user by email (the login key). Returns (nil, nil)
// when the row is missing so the auth handler can return a uniform
// "invalid credentials" without leaking which half was wrong.
func (s *Store) Get(ctx context.Context, email string) (*usermodel.User, error) {
	row := s.QueryRow(ctx, userSelect+` WHERE email = ?`, email)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return u, err
}

// GetByID returns one account by id, or nil when there is no such row.
func (s *Store) GetByID(ctx context.Context, id string) (*usermodel.User, error) {
	row := s.QueryRow(ctx, userSelect+` WHERE id = ?`, id)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return u, err
}

// Put upserts the user keyed by id. An empty PasswordHash is stored
// as SQL NULL rather than an empty string, so an OIDC-only account
// holds no value that a comparison could ever match.
func (s *Store) Put(ctx context.Context, u *usermodel.User) error {
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now().UTC()
	}

	if u.AccountType == 0 {
		u.AccountType = usermodel.AccountLocal
	}

	_, err := s.Exec(ctx, `
        INSERT INTO users (
            id, email, password_hash, account_type, admin, disabled,
            email_verified, totp_secret, totp_enabled, created_at, last_login_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            email           = excluded.email,
            password_hash   = excluded.password_hash,
            account_type    = excluded.account_type,
            admin           = excluded.admin,
            disabled        = excluded.disabled,
            email_verified  = excluded.email_verified,
            totp_secret     = excluded.totp_secret,
            totp_enabled    = excluded.totp_enabled,
            last_login_at   = excluded.last_login_at
    `,
		u.ID, u.Email,
		database.NullStr(u.PasswordHash),
		int(u.AccountType), u.Admin, u.Disabled, u.EmailVerified,
		u.TOTPSecret, u.TOTPEnabled, u.CreatedAt, database.NullTime(u.LastLoginAt),
	)

	return err
}

// MarkEmailVerified flips the verification flag in place. A dedicated
// UPDATE rather than a Put so the confirm handler cannot lose a
// concurrent change to the rest of the row.
func (s *Store) MarkEmailVerified(ctx context.Context, userID string) error {
	_, err := s.Exec(ctx, `UPDATE users SET email_verified = TRUE WHERE id = ?`, userID)

	return err
}

// SetPassword writes the hash and nothing else.
//
// Setting the field on a User that was read and calling Put would write
// the whole row, so an administrator disabling the account between that
// read and that write has the change undone and a locked-out account
// walks back in through its own password reset. `admin` and
// `email_verified` travel the same way.
// Same reasoning as MarkEmailVerified above, which is the precedent this
// follows.
func (s *Store) SetPassword(ctx context.Context, userID, hash string) error {
	_, err := s.Exec(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`,
		database.NullStr(hash), userID)

	return err
}

// SetTOTP writes the second-factor columns and nothing else, for the
// reason on SetPassword: enrolling or removing a second factor is not a
// statement about whether the account is disabled or an administrator,
// and a full-row write makes it one.
func (s *Store) SetTOTP(ctx context.Context, userID, secret string, enabled bool) error {
	_, err := s.Exec(ctx, `UPDATE users SET totp_secret = ?, totp_enabled = ? WHERE id = ?`,
		secret, enabled, userID)

	return err
}

// Delete removes one account by id.
func (s *Store) Delete(ctx context.Context, id string) error {
	_, err := s.Exec(ctx, `DELETE FROM users WHERE id = ?`, id)

	return err
}

// ClaimTOTPStep records step as spent for this user and reports
// whether the claim succeeded.
//
// The guard is the WHERE clause, not a read-then-write in Go: two
// requests presenting the same code at the same instant would both
// pass a check-then-set, and the whole point is that a code works
// once. The engine settles it - exactly one UPDATE matches, the
// other affects no rows and is refused.
//
// The comparison is strictly-less-than, so replaying a code is
// refused and so is going backwards to an earlier step within the
// skew window.
func (s *Store) ClaimTOTPStep(ctx context.Context, userID string, step uint64) (bool, error) {
	res, err := s.Exec(ctx, `
        UPDATE users SET totp_last_step = ?
        WHERE id = ? AND totp_last_step < ?
    `, step, userID, step)
	if err != nil {
		return false, err
	}

	n, err := res.RowsAffected()
	if err != nil {
		// A driver that cannot report this cannot support the guard.
		// Refuse rather than silently accepting every replay.
		return false, err
	}

	return n > 0, nil
}

// List returns every account.
func (s *Store) List(ctx context.Context) ([]*usermodel.User, error) {
	rows, err := s.Query(ctx, userSelect+` ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*usermodel.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, u)
	}

	return out, rows.Err()
}

// Count returns how many accounts exist.
func (s *Store) Count(ctx context.Context) (int, error) {
	var n int
	err := s.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)

	return n, err
}

// TouchLastLogin stamps a successful sign-in. Keyed by email because
// that is what the caller just authenticated with.
func (s *Store) TouchLastLogin(ctx context.Context, email string) error {
	_, err := s.Exec(ctx, `UPDATE users SET last_login_at = ? WHERE email = ?`, time.Now().UTC(), email)

	return err
}

func scanUser(r interface{ Scan(...any) error }) (*usermodel.User, error) {
	var u usermodel.User
	var passwordHash sql.NullString
	var lastLogin sql.NullTime
	// Admin / Disabled scan straight into bool over Postgres' native
	// BOOLEAN.
	var accountType int
	if err := r.Scan(&u.ID, &u.Email, &passwordHash, &accountType,
		&u.Admin, &u.Disabled, &u.EmailVerified, &u.TOTPSecret, &u.TOTPEnabled,
		&u.CreatedAt, &lastLogin, &u.PasskeyCount); err != nil {
		return nil, err
	}

	u.PasswordHash = passwordHash.String
	u.AccountType = usermodel.AccountType(accountType)
	if lastLogin.Valid {
		u.LastLoginAt = new(lastLogin.Time)
	}

	return &u, nil
}

// TOTPLockedUntil returns the end of the current lockout, or nil.
func (s *Store) TOTPLockedUntil(ctx context.Context, userID string) (*time.Time, error) {
	var until sql.NullTime
	err := s.QueryRow(ctx, `SELECT totp_locked_until FROM users WHERE id = ?`, userID).Scan(&until)
	if errors.Is(err, sql.ErrNoRows) || !until.Valid {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return &until.Time, nil
}

// RecordTOTPFailure counts one wrong code. On reaching limit it locks
// the factor for lock and resets the count, so the next window starts
// clean rather than re-locking on the first wrong code after it. One
// statement, so two nodes counting the same attempt agree.
func (s *Store) RecordTOTPFailure(ctx context.Context, userID string, limit int, lock time.Duration) (bool, error) {
	var until sql.NullTime
	err := s.QueryRow(ctx, `
        UPDATE users SET
            totp_locked_until = CASE WHEN totp_failures + 1 >= ?
                THEN now() + make_interval(secs => ?) ELSE totp_locked_until END,
            totp_failures = CASE WHEN totp_failures + 1 >= ? THEN 0 ELSE totp_failures + 1 END
        WHERE id = ?
        RETURNING totp_locked_until
    `, limit, lock.Seconds(), limit, userID).Scan(&until)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	return until.Valid && until.Time.After(time.Now()), nil
}

// ClearTOTPFailures forgets the count and the lock after a right code.
func (s *Store) ClearTOTPFailures(ctx context.Context, userID string) error {
	_, err := s.Exec(ctx, `UPDATE users SET totp_failures = 0, totp_locked_until = NULL WHERE id = ?`, userID)

	return err
}
