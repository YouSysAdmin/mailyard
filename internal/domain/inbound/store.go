// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package inbound

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"strings"
	"time"

	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	imodel "github.com/yousysadmin/mailyard/internal/models/inbound"
)

// Store persists mail received by the MX listener. Project scoped: a
// method taking projID answers nothing for a row another project owns.
type Store struct {
	database.Base
}

// NewStore takes optional read replicas, used by the log listing and the status counts
// (database.replica_reads.inbound_log). The Message-ID and dedup-hash
// lookups at ingest stay on the primary - they decide whether to
// insert.
//
// Whether they arrive at all is the operator's call - see
// env.ReplicaReadsConfig.
func NewStore(db *sql.DB, replicas ...*sql.DB) *Store {
	return &Store{Base: database.NewBase(db, replicas...)}
}

const inboundSelect = `
SELECT id, project_id, domain_id, message_id, dedup_hash, sender, recipients,
       subject, text_body, html_body, headers, attachments, raw, size,
       status, error_message, auth, received_at, created_at
FROM inbound_emails`

// Get returns one received message within projID, or nil when there is
// no such row.
func (s *Store) Get(ctx context.Context, projID, id string) (*imodel.Email, error) {
	row := s.QueryRow(ctx, inboundSelect+` WHERE project_id = ? AND id = ?`, projID, id)
	e, err := scanInbound(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return e, err
}

// List returns every received message in projID.
func (s *Store) List(ctx context.Context, projID string, f store.InboundFilter) ([]*imodel.Email, error) {
	var sb strings.Builder
	sb.WriteString(inboundSelect)
	sb.WriteString(` WHERE project_id = ?`)
	args := []any{projID}
	if f.Status != "" {
		sb.WriteString(` AND status = ?`)
		args = append(args, f.Status)
	}

	// `(received_at, id)`, one row-value comparison. received_at alone
	// SKIPS every row tied with the last one on the page - see
	// store.InboundFilter. BeforeID may be absent, which leaves the older
	// `?before=` contract working as it did.
	if f.Before != nil && f.BeforeID != "" {
		sb.WriteString(` AND (received_at, id) < (?, ?)`)
		args = append(args, f.Before.UTC(), f.BeforeID)
	} else if f.Before != nil {
		sb.WriteString(` AND received_at < ?`)
		args = append(args, f.Before.UTC())
	}

	sb.WriteString(` ORDER BY received_at DESC, id DESC LIMIT ?`)
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	args = append(args, limit)

	rows, err := s.ReadQuery(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*imodel.Email
	for rows.Next() {
		e, err := scanInbound(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, e)
	}

	return out, rows.Err()
}

// Put inserts the received message, or updates the row when its id
// already exists.
func (s *Store) Put(ctx context.Context, e *imodel.Email) error {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}

	// MustJSON rather than a plain marshal: it writes [] and {} for a
	// nil slice or map (the column defaults), keeps the bytes
	// deterministic, and coerces invalid UTF-8 the way headers taken
	// from received mail need - the same policy every other store
	// column gets.
	recipients := database.MustJSON(e.Recipients)
	headers := database.MustJSON(e.Headers)
	attachments := database.MustJSON(e.Attachments)

	// Nil Auth stores as an empty string, not "null": the read side
	// treats empty as "never checked", which is the honest state for a
	// row written before authentication existed.
	auth := []byte("")
	if e.Auth != nil {
		auth = database.MustJSON(e.Auth)
	}

	_, err := s.Exec(ctx, `
        INSERT INTO inbound_emails (id, project_id, domain_id, message_id, dedup_hash,
            sender, recipients, subject, text_body, html_body, headers, attachments,
            raw, size, status, error_message, auth, received_at, created_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            status        = excluded.status,
            error_message = excluded.error_message,
            auth          = excluded.auth
    `, e.ID, e.ProjectID, database.NullStr(e.DomainID), e.MessageID, e.DedupHash,
		e.Sender, string(recipients), e.Subject, e.TextBody, e.HTMLBody,
		string(headers), string(attachments), e.Raw, e.Size,
		e.Status, e.ErrorMessage, string(auth), e.ReceivedAt, e.CreatedAt)

	return err
}

// Delete removes one received message from projID.
func (s *Store) Delete(ctx context.Context, projID, id string) error {
	_, err := s.Exec(ctx, `DELETE FROM inbound_emails WHERE project_id = ? AND id = ?`, projID, id)

	return err
}

// FindByDedupHash looks a message up by content fingerprint within
// projID, used when it carries no Message-ID.
func (s *Store) FindByDedupHash(ctx context.Context, projID, hash string) (*imodel.Email, error) {
	if hash == "" {
		return nil, nil
	}

	row := s.QueryRow(ctx, inboundSelect+` WHERE project_id = ? AND dedup_hash = ? LIMIT 1`, projID, hash)
	e, err := scanInbound(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return e, err
}

// CountByStatus returns how many received messages projID has in each
// status.
func (s *Store) CountByStatus(ctx context.Context, projID string) (map[string]int, error) {
	rows, err := s.ReadQuery(ctx, `SELECT status, COUNT(*) FROM inbound_emails WHERE project_id = ? GROUP BY status`, projID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	out := map[string]int{}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}

		out[status] = n
	}

	return out, rows.Err()
}

func scanInbound(r interface{ Scan(...any) error }) (*imodel.Email, error) {
	var (
		e           imodel.Email
		recipients  string
		headers     string
		attachments string
		raw         []byte
	)
	var auth string
	if err := r.Scan(&e.ID, &e.ProjectID, database.Str(&e.DomainID), &e.MessageID, &e.DedupHash,
		&e.Sender, &recipients, &e.Subject, &e.TextBody, &e.HTMLBody,
		&headers, &attachments, &raw, &e.Size,
		&e.Status, &e.ErrorMessage, &auth, &e.ReceivedAt, &e.CreatedAt); err != nil {
		return nil, err
	}

	// Rows predating sender authentication have an empty column, which
	// is correctly represented by a nil Auth: unknown, not "failed".
	if auth != "" {
		if err := json.Unmarshal([]byte(auth), &e.Auth); err != nil {
			return nil, err
		}
	}

	if err := json.Unmarshal([]byte(recipients), &e.Recipients); err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(headers), &e.Headers); err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(attachments), &e.Attachments); err != nil {
		return nil, err
	}

	e.Raw = raw

	return &e, nil
}

// ----------------------------------------------------------------------------
// Retention
// ----------------------------------------------------------------------------

// StorageKeysOlderThan collects blob keys referenced by inbound mail
// received before the cutoff, so the objects can be dropped before
// the rows pointing at them.
func (s *Store) StorageKeysOlderThan(ctx context.Context, before time.Time) ([]string, error) {
	rows, err := s.Query(ctx, `
        SELECT attachments FROM inbound_emails
        WHERE received_at < ? AND attachments <> '[]' AND attachments <> ''`, before)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var keys []string
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}

		var atts []imodel.Attachment
		if err := json.Unmarshal([]byte(raw), &atts); err != nil {
			// A malformed row must not abort the whole sweep - the
			// worst case is an orphaned blob, which the operator can
			// reclaim out of band.
			continue
		}

		for _, a := range atts {
			if a.StorageKey != "" {
				keys = append(keys, a.StorageKey)
			}
		}
	}

	return keys, rows.Err()
}

// StorageKeysForProject collects every offloaded blob key the project's
// received mail owns.
//
// For project DELETION, where the rows go by cascade rather than by a
// statement of ours. It was missing: the project delete handler collected
// keys from `emails` only, and the comment there claimed a blob is named
// only by that table - which is not true of this one or of template
// attachments. Every inbound attachment a deleted project had offloaded
// stayed in the object store with nothing left naming it.
func (s *Store) StorageKeysForProject(ctx context.Context, projID string) ([]string, error) {
	rows, err := s.Query(ctx, `
        SELECT attachments FROM inbound_emails
        WHERE project_id = ? AND attachments <> '[]' AND attachments <> ''`, projID)
	if err != nil {
		return nil, err
	}

	return collectAttachmentKeys(rows)
}

// collectAttachmentKeys reads blob keys out of a cursor over an
// attachments column, shared so the two key queries cannot disagree
// about what a key is.
//
// A malformed row is skipped rather than failing the caller: the worst
// case is one orphaned blob, and refusing would strand the whole sweep
// or the whole delete.
func collectAttachmentKeys(rows *sql.Rows) ([]string, error) {
	defer func() { _ = rows.Close() }()
	var keys []string
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}

		var atts []imodel.Attachment
		if err := json.Unmarshal([]byte(raw), &atts); err != nil {
			continue
		}

		for _, a := range atts {
			if a.StorageKey != "" {
				keys = append(keys, a.StorageKey)
			}
		}
	}

	return keys, rows.Err()
}

// PurgeOlderThan deletes received mail older than the cutoff.
func (s *Store) PurgeOlderThan(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.Exec(ctx, `DELETE FROM inbound_emails WHERE received_at < ?`, before)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

// ClearContentOlderThan drops bodies, attachments, and the raw
// message while keeping the envelope record.
func (s *Store) ClearContentOlderThan(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.Exec(ctx, `
        UPDATE inbound_emails
        SET text_body = '', html_body = '', attachments = '[]', raw = ''
        WHERE received_at < ?
          AND (text_body <> '' OR html_body <> '' OR raw <> '' OR attachments <> '[]')`, before)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}
