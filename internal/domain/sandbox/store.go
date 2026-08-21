// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package sandbox

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"time"

	"github.com/yousysadmin/mailyard/internal/database"
	sbmodel "github.com/yousysadmin/mailyard/internal/models/sandbox"
)

// Store persists mail captured instead of delivered. Project scoped: a
// method taking projID answers nothing for a row another project owns.
type Store struct {
	database.Base
}

// NewStore takes optional read replicas, used by the message list and its count
// (database.replica_reads.sandbox). The group most worth turning off:
// a developer sends a test and looks immediately, so lag reads as the
// message never arriving.
//
// Whether they arrive at all is the operator's call - see
// env.ReplicaReadsConfig.
func NewStore(db *sql.DB, replicas ...*sql.DB) *Store {
	return &Store{Base: database.NewBase(db, replicas...)}
}

// sandboxColumns is split out of the select because the list query
// deliberately does not read raw: a page of fifty messages would drag
// fifty full MIME trees through the driver to render a table of
// subjects.
const sandboxColumns = `id, project_id, source, credential_id, api_key_id,
       sender, recipients, subject, text_body, html_body, headers, attachments,
       size, client_ip, expires_at, received_at, created_at`

const sandboxSelect = `SELECT ` + sandboxColumns + ` FROM sandbox_emails`

const sandboxSelectRaw = `SELECT raw FROM sandbox_emails WHERE project_id = ? AND id = ?`

// Get returns one captured message within projID, or nil when there is
// no such row.
func (s *Store) Get(ctx context.Context, projID, id string) (*sbmodel.Email, error) {
	row := s.QueryRow(ctx, sandboxSelect+` WHERE project_id = ? AND id = ?`, projID, id)
	e, err := scanSandbox(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return e, err
}

// Raw returns the wire bytes on their own. Separate from Get because
// they are only ever wanted one message at a time.
func (s *Store) Raw(ctx context.Context, projID, id string) ([]byte, error) {
	var raw []byte
	err := s.QueryRow(ctx, sandboxSelectRaw, projID, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return raw, err
}

// List returns one page, newest first.
func (s *Store) List(ctx context.Context, projID string, limit, offset int) ([]*sbmodel.Email, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	rows, err := s.ReadQuery(ctx,
		sandboxSelect+` WHERE project_id = ? ORDER BY received_at DESC LIMIT ? OFFSET ?`,
		projID, limit, offset)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	out := []*sbmodel.Email{} // we need empty slice here
	for rows.Next() {
		e, err := scanSandbox(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, e)
	}

	return out, rows.Err()
}

// Count returns how many captured messages projID holds.
func (s *Store) Count(ctx context.Context, projID string) (int, error) {
	var n int
	err := s.ReadQueryRow(ctx, `SELECT COUNT(*) FROM sandbox_emails WHERE project_id = ?`, projID).Scan(&n)

	return n, err
}

// Put inserts the captured message, or updates the row when its id
// already exists.
func (s *Store) Put(ctx context.Context, e *sbmodel.Email) error {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}

	if e.ReceivedAt.IsZero() {
		e.ReceivedAt = e.CreatedAt
	}

	// MustJSON rather than a plain marshal: it writes [] and {} for a
	// nil slice or map (the column defaults), keeps the bytes
	// deterministic, and coerces invalid UTF-8 the way headers taken
	// from received mail need - the same policy every other store
	// column gets.
	recipients := database.MustJSON(e.Recipients)
	headers := database.MustJSON(e.Headers)
	attachments := database.MustJSON(e.Attachments)

	_, err := s.Exec(ctx, `
        INSERT INTO sandbox_emails (id, project_id, source, credential_id, api_key_id,
            sender, recipients, subject, text_body, html_body, headers, attachments,
            raw, size, client_ip, expires_at, received_at, created_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.ProjectID, e.Source, database.NullStr(e.CredentialID), database.NullStr(e.APIKeyID),
		e.Sender, string(recipients), e.Subject, e.TextBody, e.HTMLBody,
		string(headers), string(attachments), e.Raw, e.Size, e.ClientIP,
		e.ExpiresAt, e.ReceivedAt, e.CreatedAt)

	return err
}

// Delete removes one captured message from projID.
func (s *Store) Delete(ctx context.Context, projID, id string) error {
	_, err := s.Exec(ctx, `DELETE FROM sandbox_emails WHERE project_id = ? AND id = ?`, projID, id)

	return err
}

// Clear empties one project's sandbox. The button a developer reaches
// for after a confusing test run, and the reason nothing here is worth
// a confirmation beyond the one in the console.
func (s *Store) Clear(ctx context.Context, projID string) (int64, error) {
	res, err := s.Exec(ctx, `DELETE FROM sandbox_emails WHERE project_id = ?`, projID)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

// Trim keeps at most keep messages for a project, dropping the oldest.
//
// This, not the expiry, is what actually bounds the table. A CI job
// can write ten thousand messages in a morning, and a seven day window
// does nothing about that until day seven. Called on every capture, so
// it has to stay one statement.
func (s *Store) Trim(ctx context.Context, projID string, keep int) (int64, error) {
	if keep <= 0 {
		return 0, nil
	}

	res, err := s.Exec(ctx, `
        DELETE FROM sandbox_emails
         WHERE project_id = ?
           AND id NOT IN (
               SELECT id FROM sandbox_emails
                WHERE project_id = ?
                ORDER BY received_at DESC, id DESC
                LIMIT ?
           )`, projID, projID, keep)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

// PurgeExpired removes messages whose expiry has passed, across every
// project. Rows with no expiry are never touched.
func (s *Store) PurgeExpired(ctx context.Context, now time.Time) (int64, error) {
	res, err := s.Exec(ctx,
		`DELETE FROM sandbox_emails WHERE expires_at IS NOT NULL AND expires_at < ?`, now.UTC())
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanSandbox(sc scanner) (*sbmodel.Email, error) {
	var (
		e           sbmodel.Email
		recipients  string
		headers     string
		attachments string
		expires     sql.NullTime
	)
	err := sc.Scan(&e.ID, &e.ProjectID, &e.Source, database.Str(&e.CredentialID), database.Str(&e.APIKeyID),
		&e.Sender, &recipients, &e.Subject, &e.TextBody, &e.HTMLBody,
		&headers, &attachments, &e.Size, &e.ClientIP, &expires,
		&e.ReceivedAt, &e.CreatedAt)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(recipients), &e.Recipients); err != nil {
		e.Recipients = nil
	}

	if err := json.Unmarshal([]byte(headers), &e.Headers); err != nil {
		e.Headers = nil
	}

	if err := json.Unmarshal([]byte(attachments), &e.Attachments); err != nil {
		e.Attachments = nil
	}

	if expires.Valid {
		t := expires.Time.UTC()
		e.ExpiresAt = &t
	}

	return &e, nil
}
