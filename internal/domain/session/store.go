// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package session persists tracked sign-ins and serves the
// list/revoke surface.
package session

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/yousysadmin/mailyard/internal/database"
	smodel "github.com/yousysadmin/mailyard/internal/models/session"
)

// Store persists tracked sign-ins. Not project scoped - these belong
// to the installation, not to a tenant.
type Store struct {
	database.Base
}

// NewStore builds the store on db, wiring the shared query helpers
// through database.Base.
func NewStore(db *sql.DB) *Store {
	return &Store{Base: database.NewBase(db)}
}

const sessionSelect = `
SELECT id, user_id, user_agent, ip, created_at, last_seen_at, expires_at, revoked,
       auth_provider_id
FROM sessions`

// Get is the auth-path lookup, keyed by the token's jti. Not user
// scoped - the token names the session.
func (s *Store) Get(ctx context.Context, id string) (*smodel.Session, error) {
	row := s.QueryRow(ctx, sessionSelect+` WHERE id = ?`, id)
	m, err := scanSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return m, err
}

// ListForUser returns a user's sessions, newest first. Expired rows
// are filtered out here rather than deleted, so the retention job
// stays the only thing that removes data.
func (s *Store) ListForUser(ctx context.Context, userID string, now time.Time) ([]*smodel.Session, error) {
	rows, err := s.Query(ctx, sessionSelect+`
        WHERE user_id = ? AND revoked = FALSE AND expires_at > ?
        ORDER BY last_seen_at DESC`, userID, now)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*smodel.Session
	for rows.Next() {
		m, err := scanSession(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, m)
	}

	return out, rows.Err()
}

// Put inserts the session, or updates the row when its id already
// exists.
func (s *Store) Put(ctx context.Context, m *smodel.Session) error {
	now := time.Now().UTC()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}

	if m.LastSeenAt.IsZero() {
		m.LastSeenAt = now
	}

	_, err := s.Exec(ctx, `
        INSERT INTO sessions (
            id, user_id, user_agent, ip, created_at, last_seen_at, expires_at, revoked,
            auth_provider_id
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            last_seen_at = excluded.last_seen_at,
            revoked      = excluded.revoked
    `, m.ID, m.UserID, m.UserAgent, m.IP, m.CreatedAt, m.LastSeenAt, m.ExpiresAt, m.Revoked,
		database.NullStr(m.AuthProviderID))

	return err
}

// Revoke kills one session. Scoped by user so a caller can only
// revoke their own - the id alone is not authority.
func (s *Store) Revoke(ctx context.Context, userID, id string) (bool, error) {
	res, err := s.Exec(ctx, `UPDATE sessions SET revoked = TRUE WHERE user_id = ? AND id = ?`, userID, id)
	if err != nil {
		return false, err
	}

	n, err := res.RowsAffected()

	return n > 0, err
}

// RevokeOthers kills every session for a user except the one named,
// the "sign out everywhere else" action.
//
// IS DISTINCT FROM over a NULL rather than a comparison against an
// empty string, because keepID CAN
// be empty: a token minted before session tracking carries no jti and is
// deliberately accepted, so rc.SessionID is "" for it. Comparing that to
// the uuid column answered 22P02, which MalformedID turns into a 404 -
// the one caller who most wants this to work would have been told the
// endpoint does not exist. A NULL is distinct from every id, so such a
// caller revokes all their sessions, which is the honest reading of
// "everywhere else" when we cannot tell which one is theirs.
func (s *Store) RevokeOthers(ctx context.Context, userID, keepID string) (int64, error) {
	res, err := s.Exec(ctx, `
        UPDATE sessions SET revoked = TRUE
        WHERE user_id = ? AND id IS DISTINCT FROM ?::uuid AND revoked = FALSE`,
		userID, database.NullStr(keepID))
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

// RevokeAllForUser kills every session for a user. Used when an admin
// disables an account or a password is reset - a credential change
// that leaves old sessions alive is not a credential change.
func (s *Store) RevokeAllForUser(ctx context.Context, userID string) (int64, error) {
	res, err := s.Exec(ctx, `UPDATE sessions SET revoked = TRUE WHERE user_id = ? AND revoked = FALSE`, userID)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

// Touch refreshes last_seen_at. Called at most once per touchInterval
// per session by the auth middleware - writing on every request
// would put a database write in front of every read the console
// makes.
func (s *Store) Touch(ctx context.Context, id string, t time.Time) error {
	_, err := s.Exec(ctx, `UPDATE sessions SET last_seen_at = ? WHERE id = ?`, t, id)

	return err
}

// PurgeExpired drops rows that can no longer authenticate. Revoked
// rows are kept until they would have expired anyway, so a user can
// still see a recent "signed out everywhere" in their list.
func (s *Store) PurgeExpired(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.Exec(ctx, `DELETE FROM sessions WHERE expires_at < ?`, before)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

func scanSession(r interface{ Scan(...any) error }) (*smodel.Session, error) {
	var m smodel.Session
	if err := r.Scan(&m.ID, &m.UserID, &m.UserAgent, &m.IP,
		&m.CreatedAt, &m.LastSeenAt, &m.ExpiresAt, &m.Revoked,
		database.Str(&m.AuthProviderID)); err != nil {
		return nil, err
	}

	return &m, nil
}
