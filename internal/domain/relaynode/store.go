// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package relaynode persists the identity and liveness of enrolled
// relay nodes.
//
// It owns FreshJoin and FreshClause, which is the point of the
// package existing at all: a node can attach to either delivery
// table, and both pick queries have to agree on what "alive" means.
// Written out twice, those two would drift, and the failure mode is
// mail handed to a node that is gone.
package relaynode

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"github.com/yousysadmin/mailyard/internal/database"
	nodemodel "github.com/yousysadmin/mailyard/internal/models/relaynode"
)

// FreshJoin and FreshClause are the one definition of node liveness,
// spliced into whichever pick query is running.
//
// They are constants so TestNoDynamicSQL can see them, and they are
// here rather than in either delivery store so neither can quietly
// grow its own version. A row with no matching node is not a node at
// all - an ordinary server somebody typed in - and passes.
//
// The clause takes one parameter: the instant before which a node is
// considered gone.
const (
	FreshJoin   = ` LEFT JOIN relay_nodes rn ON rn.server_id = `
	FreshClause = ` AND (rn.id IS NULL OR (rn.last_seen_at IS NOT NULL AND rn.last_seen_at > ?))`
)

// FreshSince is the cutoff to pass alongside FreshClause.
func FreshSince(now time.Time) time.Time { return now.UTC().Add(-nodemodel.StaleAfter) }

// HashToken is how a control token is stored and compared. Only the
// hash is ever written, so a database read yields nothing replayable.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))

	return hex.EncodeToString(sum[:])
}

// Store persists nodes. It also owns FreshJoin / FreshClause /
// FreshSince, which the smtpserver stores splice into their own
// SELECTs - so a change to the freshness rule here reaches the
// delivery path, and tests/schemaguard resolves them across the
// package boundary for exactly that reason.
type Store struct {
	database.Base
}

// NewStore builds the store on db, wiring the shared query helpers
// through database.Base.
func NewStore(db *sql.DB) *Store {
	return &Store{Base: database.NewBase(db)}
}

const nodeSelect = `
SELECT id, project_id, server_id, token_hash, name, version, public_ip,
       last_seen_at, created_at, inbound_enabled, inbound_queued, last_inbound_at
FROM relay_nodes`

// Get returns one relay node by id, or nil when there is no such row.
func (s *Store) Get(ctx context.Context, id string) (*nodemodel.Node, error) {
	row := s.QueryRow(ctx, nodeSelect+` WHERE id = ?`, id)
	n, err := scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return n, err
}

// GetByServer finds the node behind a delivery row, or nil when that
// row is an ordinary server.
func (s *Store) GetByServer(ctx context.Context, serverID string) (*nodemodel.Node, error) {
	row := s.QueryRow(ctx, nodeSelect+` WHERE server_id = ?`, serverID)
	n, err := scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return n, err
}

// List returns the nodes belonging to projID. The empty string is the
// platform's own, which is a real scope here and not a wildcard.
func (s *Store) List(ctx context.Context, projID string) ([]*nodemodel.Node, error) {
	rows, err := s.Query(ctx,
		nodeSelect+` WHERE project_id IS NOT DISTINCT FROM ?::uuid ORDER BY created_at ASC`,
		database.NullStr(projID))
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	var out []*nodemodel.Node
	for rows.Next() {
		n, err := scan(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, n)
	}

	return out, rows.Err()
}

// ListAll returns every node, platform and project alike. Admin only.
func (s *Store) ListAll(ctx context.Context) ([]*nodemodel.Node, error) {
	rows, err := s.Query(ctx, nodeSelect+` ORDER BY project_id ASC, created_at ASC`)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	var out []*nodemodel.Node
	for rows.Next() {
		n, err := scan(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, n)
	}

	return out, rows.Err()
}

// Put inserts the relay node, or updates the row when its id already
// exists.
func (s *Store) Put(ctx context.Context, n *nodemodel.Node) error {
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now().UTC()
	}

	_, err := s.Exec(ctx, `
		INSERT INTO relay_nodes (
			id, project_id, server_id, token_hash, name, version, public_ip,
			last_seen_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			token_hash   = EXCLUDED.token_hash,
			name         = EXCLUDED.name,
			version      = EXCLUDED.version,
			public_ip    = EXCLUDED.public_ip,
			last_seen_at = EXCLUDED.last_seen_at`,
		n.ID, database.NullStr(n.ProjectID), n.ServerID, n.TokenHash, n.Name, n.Version, n.PublicIP,
		database.NullTime(n.LastSeenAt), n.CreatedAt)

	return err
}

// Heartbeat stamps liveness and what the node reports about itself.
//
// It deliberately cannot change status - that lives on the server row
// and approval is the operator's decision. A node must never be able
// to promote itself by saying hello.
func (s *Store) Heartbeat(ctx context.Context, id, publicIP string, at time.Time, b nodemodel.Beat) error {
	_, err := s.Exec(ctx, `
		UPDATE relay_nodes
		SET last_seen_at = ?, version = ?, public_ip = ?,
		    inbound_enabled = ?, inbound_queued = ?
		WHERE id = ?`,
		at.UTC(), b.Version, publicIP, b.InboundEnabled, b.InboundQueued, id)

	return err
}

// TouchInbound records that this node successfully handed us mail.
//
// Observed, not reported. It is the answer to the question an
// operator actually has about a quiet MX - "is anything still coming
// through it" - and a node reporting its own last success would be
// grading its own work.
func (s *Store) TouchInbound(ctx context.Context, id string, at time.Time) error {
	_, err := s.Exec(ctx, `UPDATE relay_nodes SET last_inbound_at = ? WHERE id = ?`, at.UTC(), id)

	return err
}

// Delete removes one relay node by id.
func (s *Store) Delete(ctx context.Context, id string) error {
	_, err := s.Exec(ctx, `DELETE FROM relay_nodes WHERE id = ?`, id)

	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scan(r scanner) (*nodemodel.Node, error) {
	var n nodemodel.Node
	var lastSeen sql.NullTime
	var lastInbound sql.NullTime
	if err := r.Scan(&n.ID, database.Str(&n.ProjectID), &n.ServerID, &n.TokenHash, &n.Name,
		&n.Version, &n.PublicIP, &lastSeen, &n.CreatedAt,
		&n.InboundEnabled, &n.InboundQueued, &lastInbound); err != nil {
		return nil, err
	}

	if lastSeen.Valid {
		n.LastSeenAt = new(lastSeen.Time)
	}

	if lastInbound.Valid {
		n.LastInboundAt = new(lastInbound.Time)
	}

	return &n, nil
}
