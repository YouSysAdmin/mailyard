// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package oauthprovider

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/database"
	opmodel "github.com/yousysadmin/mailyard/internal/models/oauthprovider"
)

// Store persists runtime-configured identity providers. Not project
// scoped - these belong to the installation, not to a tenant.
type Store struct {
	database.Base
	crypto *crypto.Service
}

// NewStore binds the provider store to db. The crypto service
// encrypts client secrets on Put and decrypts them on read, so
// callers always see plaintext and the database never does - the
// same contract as the smtp server store.
func NewStore(db *sql.DB, cr *crypto.Service) *Store {
	return &Store{Base: database.NewBase(db), crypto: cr}
}

const providerSelect = `
SELECT id, name, slug, type, client_id, client_secret, issuer,
       auth_url, token_url, userinfo_url, scopes, enabled, hidden,
       auto_register, require_email_verified, allowed_domains,
       allowed_emails, groups_claim, allowed_groups, created_at, updated_at
FROM oauth_providers`

// Get returns one identity provider by id, or nil when there is no
// such row.
func (s *Store) Get(ctx context.Context, id string) (*opmodel.Provider, error) {
	row := s.QueryRow(ctx, providerSelect+` WHERE id = ?`, id)
	p, err := s.scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return p, err
}

// GetBySlug is the lookup the sign-in flow uses. The slug is the
// path segment in /api/auth/oauth/<slug>/start.
func (s *Store) GetBySlug(ctx context.Context, slug string) (*opmodel.Provider, error) {
	row := s.QueryRow(ctx, providerSelect+` WHERE slug = ?`, slug)
	p, err := s.scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return p, err
}

// List returns every provider, oldest first, for the admin screen.
func (s *Store) List(ctx context.Context) ([]*opmodel.Provider, error) {
	rows, err := s.Query(ctx, providerSelect+` ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*opmodel.Provider
	for rows.Next() {
		p, err := s.scan(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, p)
	}

	return out, rows.Err()
}

// ListLoginable returns the providers the sign-in page may offer:
// enabled, not hidden, and complete enough to actually work. A button
// that can only fail is worse than no button, so Usable is filtered
// here rather than left for the callback to discover.
func (s *Store) ListLoginable(ctx context.Context) ([]*opmodel.Provider, error) {
	all, err := s.List(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]*opmodel.Provider, 0, len(all))
	for _, p := range all {
		if p.Enabled && !p.Hidden && p.Usable() {
			out = append(out, p)
		}
	}

	return out, nil
}

// Put inserts the identity provider, or updates the row when its id
// already exists.
func (s *Store) Put(ctx context.Context, p *opmodel.Provider) error {
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}

	p.UpdatedAt = now
	secret, err := s.crypto.Encrypt(p.ClientSecret)
	if err != nil {
		return err
	}

	_, err = s.Exec(ctx, `
        INSERT INTO oauth_providers (
            id, name, slug, type, client_id, client_secret, issuer,
            auth_url, token_url, userinfo_url, scopes, enabled, hidden,
            auto_register, require_email_verified, allowed_domains,
            allowed_emails, groups_claim, allowed_groups, created_at, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            name                   = excluded.name,
            slug                   = excluded.slug,
            type                   = excluded.type,
            client_id              = excluded.client_id,
            client_secret          = excluded.client_secret,
            issuer                 = excluded.issuer,
            auth_url               = excluded.auth_url,
            token_url              = excluded.token_url,
            userinfo_url           = excluded.userinfo_url,
            scopes                 = excluded.scopes,
            enabled                = excluded.enabled,
            hidden                 = excluded.hidden,
            auto_register          = excluded.auto_register,
            require_email_verified = excluded.require_email_verified,
            allowed_domains        = excluded.allowed_domains,
            allowed_emails         = excluded.allowed_emails,
            groups_claim           = excluded.groups_claim,
            allowed_groups         = excluded.allowed_groups,
            updated_at             = excluded.updated_at
    `,
		p.ID, p.Name, p.Slug, p.Type, p.ClientID, secret, p.Issuer,
		p.AuthURL, p.TokenURL, p.UserInfoURL, database.MustJSON(p.Scopes),
		p.Enabled, p.Hidden, p.AutoRegister, p.RequireEmailVerified,
		database.MustJSON(p.AllowedDomains), database.MustJSON(p.AllowedEmails),
		p.GroupsClaim, database.MustJSON(p.AllowedGroups), p.CreatedAt, p.UpdatedAt,
	)

	return err
}

// Delete removes one identity provider by id.
func (s *Store) Delete(ctx context.Context, id string) error {
	_, err := s.Exec(ctx, `DELETE FROM oauth_providers WHERE id = ?`, id)

	return err
}

// SlugTaken reports whether slug belongs to a different provider,
// so a rename can be rejected with a readable message instead of a
// unique-constraint error.
func (s *Store) SlugTaken(ctx context.Context, slug, exceptID string) (bool, error) {
	var n int
	// IS DISTINCT FROM over a NULL, not a comparison against an empty
	// string. Every caller happens to
	// pass a minted id today, but an empty one against this uuid column
	// is a 22P02 that MalformedID disguises as a 404 - which is exactly
	// how the smtp group version of this shipped broken.
	err := s.QueryRow(ctx, `
        SELECT COUNT(*) FROM oauth_providers
        WHERE slug = ? AND id IS DISTINCT FROM ?::uuid`,
		slug, database.NullStr(exceptID)).Scan(&n)

	return n > 0, err
}

func (s *Store) scan(r interface{ Scan(...any) error }) (*opmodel.Provider, error) {
	var p opmodel.Provider
	var scopes, domains, emails, groups string
	var secret string
	if err := r.Scan(&p.ID, &p.Name, &p.Slug, &p.Type, &p.ClientID, &secret,
		&p.Issuer, &p.AuthURL, &p.TokenURL, &p.UserInfoURL, &scopes,
		&p.Enabled, &p.Hidden, &p.AutoRegister, &p.RequireEmailVerified,
		&domains, &emails, &p.GroupsClaim, &groups,
		&p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}

	plain, err := s.crypto.Decrypt(secret)
	if err != nil {
		return nil, err
	}

	p.ClientSecret = plain
	database.MustUnmarshalJSON(scopes, &p.Scopes)
	database.MustUnmarshalJSON(domains, &p.AllowedDomains)
	database.MustUnmarshalJSON(emails, &p.AllowedEmails)
	database.MustUnmarshalJSON(groups, &p.AllowedGroups)

	return &p, nil
}

// IdentityStore persists the external-identity links. Separate struct
// on the same table pair so the sign-in flow can be handed just this
// half.
type IdentityStore struct {
	database.Base
}

// NewIdentityStore builds a IdentityStore on db.
func NewIdentityStore(db *sql.DB) *IdentityStore {
	return &IdentityStore{Base: database.NewBase(db)}
}

const identitySelect = `
SELECT id, user_id, provider_id, subject, email, name, created_at, last_login_at
FROM oauth_identities`

// GetBySubject is the primary sign-in lookup. Keyed on the pair
// because a subject is only unique within its issuer.
func (s *IdentityStore) GetBySubject(ctx context.Context, providerID, subject string) (*opmodel.Identity, error) {
	row := s.QueryRow(ctx, identitySelect+` WHERE provider_id = ? AND subject = ?`, providerID, subject)
	id, err := scanIdentity(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return id, err
}

// ListForUser powers the profile screen's "connected accounts".
func (s *IdentityStore) ListForUser(ctx context.Context, userID string) ([]*opmodel.Identity, error) {
	rows, err := s.Query(ctx, identitySelect+` WHERE user_id = ? ORDER BY created_at ASC`, userID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*opmodel.Identity
	for rows.Next() {
		id, err := scanIdentity(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, id)
	}

	return out, rows.Err()
}

// Link records the identity, or refreshes it if this pair has signed
// in before. One statement so two concurrent sign-ins cannot both
// insert.
func (s *IdentityStore) Link(ctx context.Context, id *opmodel.Identity) error {
	now := time.Now().UTC()
	if id.ID == "" {
		id.ID = ids.New()
	}

	if id.CreatedAt.IsZero() {
		id.CreatedAt = now
	}

	id.LastLoginAt = now
	_, err := s.Exec(ctx, `
        INSERT INTO oauth_identities (
            id, user_id, provider_id, subject, email, name, created_at, last_login_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(provider_id, subject) DO UPDATE SET
            email         = excluded.email,
            name          = excluded.name,
            last_login_at = excluded.last_login_at
    `, id.ID, id.UserID, id.ProviderID, id.Subject, id.Email, id.Name,
		id.CreatedAt, id.LastLoginAt)

	return err
}

// Unlink removes one connection, for a user disconnecting a provider.
func (s *IdentityStore) Unlink(ctx context.Context, userID, providerID string) error {
	_, err := s.Exec(ctx, `DELETE FROM oauth_identities WHERE user_id = ? AND provider_id = ?`,
		userID, providerID)

	return err
}

func scanIdentity(r interface{ Scan(...any) error }) (*opmodel.Identity, error) {
	var id opmodel.Identity
	if err := r.Scan(&id.ID, &id.UserID, &id.ProviderID, &id.Subject,
		&id.Email, &id.Name, &id.CreatedAt, &id.LastLoginAt); err != nil {
		return nil, err
	}

	return &id, nil
}
