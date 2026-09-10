// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package sandbox

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/yousysadmin/mailyard/internal/database"
	sbmodel "github.com/yousysadmin/mailyard/internal/models/sandbox"
)

// InboxStore persists sandbox inboxes, the saved sender filters over
// captured mail. Project scoped: a method taking projID answers nothing
// for a row another project owns.
//
// Primary reads only. The console writes an inbox and re-reads the list
// in the same breath, which is exactly what a replica must not serve.
type InboxStore struct {
	database.Base
}

// NewInboxStore builds the store on db, wiring the shared query helpers
// through database.Base.
func NewInboxStore(db *sql.DB) *InboxStore {
	return &InboxStore{Base: database.NewBase(db)}
}

const inboxSelect = `
SELECT id, project_id, name, description, addresses, created_at, updated_at
FROM sandbox_inboxes`

// Get returns one inbox within projID, or nil when there is no such row.
func (s *InboxStore) Get(ctx context.Context, projID, id string) (*sbmodel.Inbox, error) {
	row := s.QueryRow(ctx, inboxSelect+` WHERE project_id = ? AND id = ?`, projID, id)
	in, err := scanInbox(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return in, err
}

// GetByName returns one inbox by name within projID, or nil when there
// is no such row.
func (s *InboxStore) GetByName(ctx context.Context, projID, name string) (*sbmodel.Inbox, error) {
	row := s.QueryRow(ctx, inboxSelect+` WHERE project_id = ? AND name = ?`, projID, name)
	in, err := scanInbox(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return in, err
}

// List returns every inbox in projID, by name.
func (s *InboxStore) List(ctx context.Context, projID string) ([]*sbmodel.Inbox, error) {
	rows, err := s.Query(ctx, inboxSelect+` WHERE project_id = ? ORDER BY name ASC`, projID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	out := []*sbmodel.Inbox{}
	for rows.Next() {
		in, err := scanInbox(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, in)
	}

	return out, rows.Err()
}

// Put inserts the inbox, or updates the row when its id already exists.
func (s *InboxStore) Put(ctx context.Context, in *sbmodel.Inbox) error {
	if in.CreatedAt.IsZero() {
		in.CreatedAt = time.Now().UTC()
	}

	addresses := database.MustJSON(in.Addresses)
	_, err := s.Exec(ctx, `
        INSERT INTO sandbox_inboxes (
            id, project_id, name, description, addresses, created_at, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            name        = excluded.name,
            description = excluded.description,
            addresses   = excluded.addresses,
            updated_at  = excluded.updated_at
        WHERE sandbox_inboxes.project_id = excluded.project_id
    `, in.ID, in.ProjectID, in.Name, in.Description, string(addresses),
		in.CreatedAt, database.NullTime(in.UpdatedAt))

	return err
}

// Delete removes one inbox from projID. The captures it filtered are
// untouched, since none of them names it.
func (s *InboxStore) Delete(ctx context.Context, projID, id string) error {
	_, err := s.Exec(ctx, `DELETE FROM sandbox_inboxes WHERE project_id = ? AND id = ?`, projID, id)

	return err
}

func scanInbox(sc scanner) (*sbmodel.Inbox, error) {
	var (
		in        sbmodel.Inbox
		addresses string
		updated   sql.NullTime
	)
	if err := sc.Scan(&in.ID, &in.ProjectID, &in.Name, &in.Description, &addresses,
		&in.CreatedAt, &updated); err != nil {
		return nil, err
	}

	database.MustUnmarshalJSON(addresses, &in.Addresses)
	if in.Addresses == nil {
		in.Addresses = []string{}
	}

	if updated.Valid {
		in.UpdatedAt = new(updated.Time)
	}

	return &in, nil
}
