// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package notification

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/database"
	nmodel "github.com/yousysadmin/mailyard/internal/models/notification"
)

// Store persists in-app notifications. Project scoped: a method taking
// projID answers nothing for a row another project owns.
type Store struct {
	database.Base
}

// NewStore builds the store on db, wiring the shared query helpers
// through database.Base.
func NewStore(db *sql.DB) *Store {
	return &Store{Base: database.NewBase(db)}
}

const notificationSelect = `
SELECT id, project_id, type, severity, title, body, link,
       dedupe_key, read_at, created_at
FROM notifications`

// Get returns one notification within projID, or nil when there is no
// such row.
func (s *Store) Get(ctx context.Context, projID, id string) (*nmodel.Notification, error) {
	row := s.QueryRow(ctx, notificationSelect+` WHERE project_id = ? AND id = ?`, projID, id)
	n, err := scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return n, err
}

// List returns a page of notifications, newest first. unreadOnly
// narrows to what still needs attention.
func (s *Store) List(ctx context.Context, projID string, unreadOnly bool, limit, offset int) ([]*nmodel.Notification, error) {
	query := notificationSelect + ` WHERE project_id = ?`
	args := []any{projID}
	if unreadOnly {
		query += ` AND read_at IS NULL`
	}

	query += ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*nmodel.Notification
	for rows.Next() {
		n, err := scan(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, n)
	}

	return out, rows.Err()
}

// CountUnread powers the badge, which is polled far more often than
// the list is opened, so it stays a single indexed count.
func (s *Store) CountUnread(ctx context.Context, projID string) (int, error) {
	var n int
	err := s.QueryRow(ctx, `SELECT COUNT(*) FROM notifications WHERE project_id = ? AND read_at IS NULL`,
		projID).Scan(&n)

	return n, err
}

// Create files a notification, returning false when a row with the
// same dedupe key already exists.
//
// The caller uses that answer to decide whether anything new
// happened - a job that reports "bounce rate is high" every fifteen
// minutes should publish a live event the first time and stay quiet
// afterwards.
func (s *Store) Create(ctx context.Context, n *nmodel.Notification) (bool, error) {
	if n.ID == "" {
		n.ID = ids.New()
	}

	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now().UTC()
	}

	if n.Severity == "" {
		n.Severity = nmodel.SeverityInfo
	}

	// DO NOTHING rather than an upsert: a repeat is not new
	// information, and refreshing created_at would keep an old alert
	// permanently at the top of the list.
	res, err := s.Exec(ctx, `
        INSERT INTO notifications (
            id, project_id, type, severity, title, body, link,
            dedupe_key, read_at, created_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(project_id, dedupe_key) WHERE dedupe_key <> '' DO NOTHING
    `, n.ID, n.ProjectID, n.Type, n.Severity, n.Title, n.Body, n.Link,
		n.DedupeKey, database.NullTime(n.ReadAt), n.CreatedAt)
	if err != nil {
		return false, err
	}

	affected, err := res.RowsAffected()
	if err != nil {
		// A driver that cannot report this is not a reason to fail the
		// write that already succeeded.
		return true, nil
	}

	return affected > 0, nil
}

// MarkRead marks one notification read. Idempotent: marking an
// already-read row again does not move its timestamp.
func (s *Store) MarkRead(ctx context.Context, projID, id string, at time.Time) error {
	_, err := s.Exec(ctx, `
        UPDATE notifications SET read_at = ?
        WHERE project_id = ? AND id = ? AND read_at IS NULL
    `, at, projID, id)

	return err
}

// MarkAllRead clears the badge in one statement.
func (s *Store) MarkAllRead(ctx context.Context, projID string, at time.Time) (int64, error) {
	res, err := s.Exec(ctx, `
        UPDATE notifications SET read_at = ?
        WHERE project_id = ? AND read_at IS NULL
    `, at, projID)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

// Delete removes one notification from projID.
func (s *Store) Delete(ctx context.Context, projID, id string) error {
	_, err := s.Exec(ctx, `DELETE FROM notifications WHERE project_id = ? AND id = ?`, projID, id)

	return err
}

// PurgeOlderThan is the retention hook. Read notifications are the
// only ones dropped by age here - an unread alert is still trying to
// tell somebody something.
func (s *Store) PurgeOlderThan(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.Exec(ctx, `DELETE FROM notifications WHERE created_at < ? AND read_at IS NOT NULL`, before)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

func scan(r interface{ Scan(...any) error }) (*nmodel.Notification, error) {
	var n nmodel.Notification
	var readAt sql.NullTime
	if err := r.Scan(&n.ID, &n.ProjectID, &n.Type, &n.Severity, &n.Title,
		&n.Body, &n.Link, &n.DedupeKey, &readAt, &n.CreatedAt); err != nil {
		return nil, err
	}

	if readAt.Valid {
		n.ReadAt = new(readAt.Time)
	}

	return &n, nil
}
