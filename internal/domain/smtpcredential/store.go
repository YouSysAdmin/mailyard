// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpcredential

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/yousysadmin/mailyard/internal/database"
	scmodel "github.com/yousysadmin/mailyard/internal/models/smtpcredential"
)

// Store persists SMTP relay logins. Project scoped: a method taking
// projID answers nothing for a row another project owns.
type Store struct {
	database.Base
}

// NewStore builds the store on db, wiring the shared query helpers
// through database.Base.
func NewStore(db *sql.DB) *Store {
	return &Store{Base: database.NewBase(db)}
}

const credSelect = `
SELECT id, project_id, created_by, name, username, password_hash,
       allowed_ips, smtp_group_id, sandbox, revoked, last_used_at, created_at
FROM smtp_credentials`

// Get returns one SMTP credential within projID, or nil when there is
// no such row.
func (s *Store) Get(ctx context.Context, projID, id string) (*scmodel.Credential, error) {
	row := s.QueryRow(ctx, credSelect+` WHERE project_id = ? AND id = ?`, projID, id)
	c, err := scanCred(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return c, err
}

// GetByUsername is the relay auth lookup and deliberately not
// project scoped - the credential itself decides the project.
func (s *Store) GetByUsername(ctx context.Context, username string) (*scmodel.Credential, error) {
	row := s.QueryRow(ctx, credSelect+` WHERE username = ?`, username)
	c, err := scanCred(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return c, err
}

// List returns every SMTP credential in projID.
func (s *Store) List(ctx context.Context, projID string) ([]*scmodel.Credential, error) {
	rows, err := s.Query(ctx, credSelect+` WHERE project_id = ? ORDER BY created_at ASC`, projID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*scmodel.Credential
	for rows.Next() {
		c, err := scanCred(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, c)
	}

	return out, rows.Err()
}

// Put inserts the SMTP credential, or updates the row when its id
// already exists.
func (s *Store) Put(ctx context.Context, c *scmodel.Credential) error {
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}

	_, err := s.Exec(ctx, `
        INSERT INTO smtp_credentials (
            id, project_id, created_by, name, username, password_hash,
            allowed_ips, smtp_group_id, sandbox, revoked, last_used_at, created_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            name        = excluded.name,
            allowed_ips   = excluded.allowed_ips,
            smtp_group_id = excluded.smtp_group_id,
            sandbox       = excluded.sandbox,
            revoked     = excluded.revoked
    `,
		c.ID, c.ProjectID, c.CreatedBy, c.Name, c.Username, c.PasswordHash,
		database.MustJSON(c.AllowedIPs), database.NullStr(c.SMTPGroupID), c.Sandbox, c.Revoked,
		database.NullTime(c.LastUsedAt), c.CreatedAt,
	)

	return err
}

// Revoke disables one credential within projID without deleting it, so
// the audit trail keeps who had it.
func (s *Store) Revoke(ctx context.Context, projID, id string) error {
	_, err := s.Exec(ctx, `UPDATE smtp_credentials SET revoked = TRUE WHERE project_id = ? AND id = ?`, projID, id)

	return err
}

// Delete removes one SMTP credential from projID.
func (s *Store) Delete(ctx context.Context, projID, id string) error {
	_, err := s.Exec(ctx, `DELETE FROM smtp_credentials WHERE project_id = ? AND id = ?`, projID, id)

	return err
}

// TouchLastUsed stamps when the credential last authenticated.
// Unscoped: the credential decides the project.
func (s *Store) TouchLastUsed(ctx context.Context, id string, t time.Time) error {
	_, err := s.Exec(ctx, `UPDATE smtp_credentials SET last_used_at = ? WHERE id = ?`, t, id)

	return err
}

// Count reports how many rows the project holds, for plan caps.
func (s *Store) Count(ctx context.Context, projID string) (int, error) {
	var n int
	err := s.QueryRow(ctx, `SELECT COUNT(*) FROM smtp_credentials WHERE project_id = ?`, projID).Scan(&n)

	return n, err
}

func scanCred(r interface{ Scan(...any) error }) (*scmodel.Credential, error) {
	var c scmodel.Credential
	var ips string
	var lastUsed sql.NullTime
	if err := r.Scan(&c.ID, &c.ProjectID, &c.CreatedBy, &c.Name, &c.Username,
		&c.PasswordHash, &ips, database.Str(&c.SMTPGroupID), &c.Sandbox, &c.Revoked, &lastUsed, &c.CreatedAt); err != nil {
		return nil, err
	}

	database.MustUnmarshalJSON(ips, &c.AllowedIPs)
	if lastUsed.Valid {
		c.LastUsedAt = new(lastUsed.Time)
	}

	return &c, nil
}
