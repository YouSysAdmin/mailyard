// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpserver

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/domain/relaynode"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

// Store persists per-project SMTP servers and their groups. Project
// scoped: a method taking projID answers nothing for a row another
// project owns.
type Store struct {
	database.Base
	crypto *crypto.Service
}

// NewStore binds the smtp server store to db. The crypto service
// encrypts passwords on Put and decrypts them on every read, so
// callers always see plaintext and the database never does.
func NewStore(db *sql.DB, cr *crypto.Service) *Store {
	return &Store{Base: database.NewBase(db), crypto: cr}
}

// serverSelect joins relay_nodes for the same reason the shared pool
// does: a project's own node is an smtp_servers row, and delivery has
// to know it is one. The token hash is not selected - the delivery
// path reads this row on every send and has no use for a credential.
const serverSelect = `
SELECT s.id, s.project_id, s.created_by, s.name, s.host, s.port, s.username,
       s.password, s.encryption, s.skip_dkim, s.allowed_emails, s.allowed_domains, s.group_id,
       s.priority, s.status, s.validation_error, s.validated_at, s.created_at,
       s.ses_topic_arn, s.provider, s.provider_config, rn.id, rn.last_seen_at
FROM smtp_servers s` + relaynode.FreshJoin + `s.id`

// Get returns one SMTP server within projID, or nil when there is no
// such row.
func (s *Store) Get(ctx context.Context, projID, id string) (*ssmodel.Server, error) {
	row := s.QueryRow(ctx, serverSelect+` WHERE s.project_id = ? AND s.id = ?`, projID, id)
	srv, err := s.scanServer(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return srv, err
}

// GetAny finds a server by id without a project scope.
//
// The one unscoped read in this store, and it exists for a caller
// that legitimately does not know the project: the SES receiver has a
// server id off an email row and needs the topic configured on it.
// Deliberately not exported to any tenant-facing path - every one of
// those goes through Get, which scopes first.
func (s *Store) GetAny(ctx context.Context, id string) (*ssmodel.Server, error) {
	row := s.QueryRow(ctx, serverSelect+` WHERE s.id = ?`, id)
	srv, err := s.scanServer(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return srv, err
}

// List returns every SMTP server in projID.
func (s *Store) List(ctx context.Context, projID string) ([]*ssmodel.Server, error) {
	rows, err := s.Query(ctx, serverSelect+` WHERE s.project_id = ? ORDER BY s.priority ASC, s.created_at ASC`, projID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*ssmodel.Server
	for rows.Next() {
		srv, err := s.scanServer(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, srv)
	}

	return out, rows.Err()
}

// groupServerSelect is the delivery-side read, so the freshness rule
// applies here too: a project's own node that stopped reporting must
// not be handed mail, exactly as in the shared pool. Assembled as one
// constant because TestNoDynamicSQL judges the expression at the call
// site and cannot follow a constant folded across packages.
const groupServerSelect = serverSelect + `
        WHERE s.project_id = ? AND s.group_id = ?` + relaynode.FreshClause + `
        ORDER BY s.priority ASC, s.created_at ASC`

// ListInGroup returns one group's servers in pick order: priority
// first, then age so the order is total and failover is
// deterministic across nodes.
func (s *Store) ListInGroup(ctx context.Context, projID, groupID string) ([]*ssmodel.Server, error) {
	rows, err := s.Query(ctx, groupServerSelect, projID, groupID,
		relaynode.FreshSince(time.Now()))
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*ssmodel.Server
	for rows.Next() {
		srv, err := s.scanServer(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, srv)
	}

	return out, rows.Err()
}

// Put upserts the server keyed by id, encrypting the password. The
// caller passes the plaintext password (or the value a previous read
// returned, which is also plaintext).
func (s *Store) Put(ctx context.Context, srv *ssmodel.Server) error {
	if srv.CreatedAt.IsZero() {
		srv.CreatedAt = time.Now().UTC()
	}

	stored, err := s.crypto.Encrypt(srv.Password)
	if err != nil {
		return err
	}

	// Derived fields settled before the write, and through the pointer, so
	// the row and the object the handler returns agree. Without it the
	// database said skip_dkim = true and the API answered false about the
	// same row.
	srv.Normalize()
	_, err = s.Exec(ctx, `
        INSERT INTO smtp_servers (
            id, project_id, created_by, name, host, port, username, password,
            encryption, skip_dkim, allowed_emails, allowed_domains, group_id, priority,
            status, validation_error, validated_at, created_at, ses_topic_arn,
            provider, provider_config
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            name             = excluded.name,
            host             = excluded.host,
            port             = excluded.port,
            username         = excluded.username,
            password         = excluded.password,
            encryption       = excluded.encryption,
            skip_dkim        = excluded.skip_dkim,
            allowed_emails   = excluded.allowed_emails,
            allowed_domains  = excluded.allowed_domains,
            group_id         = excluded.group_id,
            priority         = excluded.priority,
            status           = excluded.status,
            validation_error = excluded.validation_error,
            validated_at     = excluded.validated_at,
            ses_topic_arn    = excluded.ses_topic_arn,
            provider         = excluded.provider,
            provider_config  = excluded.provider_config
    `,
		srv.ID, srv.ProjectID, srv.CreatedBy, srv.Name, srv.Host, srv.Port,
		srv.Username, stored, srv.Encryption, srv.SkipDKIM, database.MustJSON(srv.AllowedEmails),
		database.MustJSON(srv.AllowedDomains),
		database.NullStr(srv.GroupID), srv.Priority,
		srv.Status, srv.ValidationError, database.NullTime(srv.ValidatedAt), srv.CreatedAt,
		srv.SESTopicARN, srv.Provider, database.MustJSON(srv.ProviderConfig),
	)

	return err
}

// Delete removes one SMTP server from projID.
func (s *Store) Delete(ctx context.Context, projID, id string) error {
	_, err := s.Exec(ctx, `DELETE FROM smtp_servers WHERE project_id = ? AND id = ?`, projID, id)

	return err
}

// SetStatus updates the status columns without touching credentials
// (used by test-connection and enable / disable).
func (s *Store) SetStatus(ctx context.Context, projID, id, status, validationErr string, validatedAt *time.Time) error {
	_, err := s.Exec(ctx, `
        UPDATE smtp_servers
        SET status = ?, validation_error = ?, validated_at = ?
        WHERE project_id = ? AND id = ?
    `, status, validationErr, database.NullTime(validatedAt), projID, id)

	return err
}

// PickEnabled returns the first enabled server whose sender rules
// admit the sender, or (nil, nil) when none qualifies. Oldest first so
// the pick is stable.
//
// Nothing in the tree calls this - email.ResolveCandidates is the one
// answer to which server carries a message. It asks both rules, so
// this asks both too: a second path that disagreed would be worse
// than a second path.
func (s *Store) PickEnabled(ctx context.Context, projID, senderEmail string) (*ssmodel.Server, error) {
	servers, err := s.List(ctx, projID)
	if err != nil {
		return nil, err
	}

	for _, srv := range servers {
		if srv.Status == ssmodel.StatusEnabled && srv.AllowsSender(senderEmail) &&
			srv.AllowsDomain(senderEmail) {
			return srv, nil
		}
	}

	return nil, nil
}

func (s *Store) scanServer(r interface{ Scan(...any) error }) (*ssmodel.Server, error) {
	var srv ssmodel.Server
	var allowed, allowedDomains, providerConfig string
	var validatedAt, lastSeenAt sql.NullTime
	if err := r.Scan(&srv.ID, &srv.ProjectID, &srv.CreatedBy, &srv.Name, &srv.Host,
		&srv.Port, &srv.Username, &srv.Password, &srv.Encryption, &srv.SkipDKIM, &allowed, &allowedDomains,
		database.Str(&srv.GroupID), &srv.Priority,
		&srv.Status, &srv.ValidationError, &validatedAt, &srv.CreatedAt,
		&srv.SESTopicARN, &srv.Provider, &providerConfig,
		database.Str(&srv.NodeID), &lastSeenAt); err != nil {
		return nil, err
	}

	database.MustUnmarshalJSON(allowed, &srv.AllowedEmails)
	database.MustUnmarshalJSON(allowedDomains, &srv.AllowedDomains)
	database.MustUnmarshalJSON(providerConfig, &srv.ProviderConfig)
	if validatedAt.Valid {
		srv.ValidatedAt = new(validatedAt.Time)
	}

	if lastSeenAt.Valid {
		srv.LastSeenAt = new(lastSeenAt.Time)
	}

	plain, err := s.crypto.Decrypt(srv.Password)
	if err != nil {
		return nil, err
	}

	srv.Password = plain

	return &srv, nil
}

// Count reports how many rows the project holds, for plan caps.
func (s *Store) Count(ctx context.Context, projID string) (int, error) {
	var n int
	err := s.QueryRow(ctx, `SELECT COUNT(*) FROM smtp_servers WHERE project_id = ?`, projID).Scan(&n)

	return n, err
}
