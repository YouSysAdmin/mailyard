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

// SharedStore is the platform-owned SMTP pool.
//
// Lives in this package so it shares the encryption path and the
// connection test with the per-project store, but reads a different
// table and takes no project id anywhere - there is no tenant to
// scope on, which is exactly why it could not live in smtp_servers.
type SharedStore struct {
	database.Base
	crypto *crypto.Service
}

// NewSharedStore builds a SharedStore on db.
func NewSharedStore(db *sql.DB, cr *crypto.Service) *SharedStore {
	return &SharedStore{Base: database.NewBase(db), crypto: cr}
}

// sharedSelect joins relay_nodes so a row knows whether it is a node
// and when that node last reported. The token hash is deliberately
// not selected: the delivery path reads this row on every send and
// has no use for a credential.
const sharedSelect = `
SELECT s.id, s.created_by, s.name, s.host, s.port, s.username, s.password,
       s.encryption, s.skip_dkim, s.allowed_emails, s.allowed_domains,
       s.security_mode, s.priority, s.status, s.validation_error,
       s.validated_at, s.created_at, s.ses_topic_arn, s.platform_only,
       s.provider, s.provider_config, rn.id, rn.last_seen_at
FROM shared_smtp_servers s` + relaynode.FreshJoin + `s.id`

// sharedEnabledSelect is assembled once, here, rather than at the call
// site. Every piece is a constant - including relaynode.FreshClause,
// which is the single definition of node liveness both delivery
// tables splice in - but TestNoDynamicSQL judges the expression it
// finds at the call site, and a constant folded across packages is
// not something it can follow. Naming the whole query keeps the
// guard's rule intact instead of exempting this one.
const sharedEnabledSelect = sharedSelect + ` WHERE s.status = ?` +
	relaynode.FreshClause + ` ORDER BY s.priority ASC, s.created_at ASC`

// Get returns one SMTP server by id, or nil when there is no such row.
func (s *SharedStore) Get(ctx context.Context, id string) (*ssmodel.Shared, error) {
	row := s.QueryRow(ctx, sharedSelect+` WHERE s.id = ?`, id)
	srv, err := s.scanShared(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return srv, err
}

// List returns the whole pool in pick order, enabled or not. The
// admin surface wants the disabled ones too.
func (s *SharedStore) List(ctx context.Context) ([]*ssmodel.Shared, error) {
	rows, err := s.Query(ctx, sharedSelect+` ORDER BY s.priority ASC, s.created_at ASC`)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*ssmodel.Shared
	for rows.Next() {
		srv, err := s.scanShared(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, srv)
	}

	return out, rows.Err()
}

// ListEnabled returns only servers eligible for delivery, in pick
// order. Kept separate from List so the delivery path cannot pick up
// a disabled or invalid server by forgetting to filter.
// The freshness clause lives here, in the query the delivery path
// runs, and not only in the sweep that marks stale nodes invalid.
// The sweep is for the human looking at the console. If it stops
// running - a worker outage, a cron job that panicked - mail must
// still not be handed to a node that vanished, and a filter that only
// exists in a background job cannot promise that.
func (s *SharedStore) ListEnabled(ctx context.Context) ([]*ssmodel.Shared, error) {
	rows, err := s.Query(ctx, sharedEnabledSelect,
		ssmodel.StatusEnabled, relaynode.FreshSince(time.Now()))
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*ssmodel.Shared
	for rows.Next() {
		srv, err := s.scanShared(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, srv)
	}

	return out, rows.Err()
}

// Put inserts the SMTP server, or updates the row when its id already
// exists.
func (s *SharedStore) Put(ctx context.Context, srv *ssmodel.Shared) error {
	if srv.CreatedAt.IsZero() {
		srv.CreatedAt = time.Now().UTC()
	}

	if srv.SecurityMode == "" {
		srv.SecurityMode = ssmodel.SecurityPermissive
	}

	stored, err := s.crypto.Encrypt(srv.Password)
	if err != nil {
		return err
	}

	// Derived fields settled first, for the reason in the project store.
	srv.Normalize()
	_, err = s.Exec(ctx, `
        INSERT INTO shared_smtp_servers (
            id, created_by, name, host, port, username, password, encryption,
            skip_dkim, allowed_emails, allowed_domains, security_mode, priority,
            status, validation_error, validated_at, created_at, ses_topic_arn,
            platform_only, provider, provider_config
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
            security_mode    = excluded.security_mode,
            priority         = excluded.priority,
            status           = excluded.status,
            validation_error = excluded.validation_error,
            validated_at     = excluded.validated_at,
            ses_topic_arn    = excluded.ses_topic_arn,
            platform_only    = excluded.platform_only,
            provider         = excluded.provider,
            provider_config  = excluded.provider_config
    `,
		srv.ID, srv.CreatedBy, srv.Name, srv.Host, srv.Port, srv.Username, stored,
		srv.Encryption, srv.SkipDKIM, database.MustJSON(srv.AllowedEmails),
		database.MustJSON(srv.AllowedDomains), srv.SecurityMode, srv.Priority,
		srv.Status, srv.ValidationError, database.NullTime(srv.ValidatedAt), srv.CreatedAt,
		srv.SESTopicARN, srv.PlatformOnly,
		srv.Provider, database.MustJSON(srv.ProviderConfig),
	)

	return err
}

// Delete removes one SMTP server by id.
func (s *SharedStore) Delete(ctx context.Context, id string) error {
	_, err := s.Exec(ctx, `DELETE FROM shared_smtp_servers WHERE id = ?`, id)

	return err
}

// SetStatus updates the status columns without touching credentials.
func (s *SharedStore) SetStatus(ctx context.Context, id, status, validationErr string, validatedAt *time.Time) error {
	_, err := s.Exec(ctx, `
        UPDATE shared_smtp_servers
        SET status = ?, validation_error = ?, validated_at = ?
        WHERE id = ?
    `, status, validationErr, database.NullTime(validatedAt), id)

	return err
}

// Count reports the pool size, for the admin summary.
func (s *SharedStore) Count(ctx context.Context) (int, error) {
	var n int
	err := s.QueryRow(ctx, `SELECT COUNT(*) FROM shared_smtp_servers`).Scan(&n)

	return n, err
}

func (s *SharedStore) scanShared(r interface{ Scan(...any) error }) (*ssmodel.Shared, error) {
	var srv ssmodel.Shared
	var allowedEmails, allowedDomains, providerConfig string
	var validatedAt, lastSeenAt sql.NullTime
	if err := r.Scan(&srv.ID, &srv.CreatedBy, &srv.Name, &srv.Host, &srv.Port,
		&srv.Username, &srv.Password, &srv.Encryption, &srv.SkipDKIM,
		&allowedEmails, &allowedDomains, &srv.SecurityMode, &srv.Priority,
		&srv.Status, &srv.ValidationError, &validatedAt, &srv.CreatedAt,
		&srv.SESTopicARN, &srv.PlatformOnly, &srv.Provider, &providerConfig,
		database.Str(&srv.NodeID), &lastSeenAt); err != nil {
		return nil, err
	}

	database.MustUnmarshalJSON(allowedEmails, &srv.AllowedEmails)
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
