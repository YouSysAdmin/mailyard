// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package domains is the persistence and handler surface for inbound
// routing domains: a project claims a recipient domain, proves
// ownership with a DNS TXT record, and the MX listener then accepts
// mail for it. Routes live behind requireAuth + requireProject in
// server/routes.go.
package domains

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/dkim"
	"github.com/yousysadmin/mailyard/internal/core/dnsname"
	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/quota"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/domain"
	dmodel "github.com/yousysadmin/mailyard/internal/models/domain"
)

// Store persists claimed recipient domains. Project scoped: a method
// taking projID answers nothing for a row another project owns.
type Store struct {
	database.Base
	crypto *crypto.Service
}

// NewStore binds the domain store to db. The crypto service seals the
// DKIM private key on write and unseals it on read, so callers always
// see PEM and the database never does - the same contract as the smtp
// server and oauth provider stores.
func NewStore(db *sql.DB, cr *crypto.Service) *Store {
	return &Store{Base: database.NewBase(db), crypto: cr}
}

const domainSelect = `
SELECT id, project_id, created_by, domain, verification_token, verified, verified_at, created_at,
       dkim_selector, dkim_private_key, dkim_public_key,
       spf_verified, dkim_verified, dmarc_verified, checked_at
FROM domains`

// Get returns one domain within projID, or nil when there is no such
// row.
func (s *Store) Get(ctx context.Context, projID, id string) (*dmodel.Domain, error) {
	row := s.QueryRow(ctx, domainSelect+` WHERE project_id = ? AND id = ?`, projID, id)
	d, err := s.scanDomain(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return d, err
}

// GetVerifiedByName is the MX listener's routing lookup. Not
// project scoped: the domain row decides the project.
func (s *Store) GetVerifiedByName(ctx context.Context, name string) (*dmodel.Domain, error) {
	row := s.QueryRow(ctx, domainSelect+` WHERE domain = ? AND verified = TRUE`, strings.ToLower(name))
	d, err := s.scanDomain(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return d, err
}

// GetVerifiedCovering answers "which verified domain covers this
// name", which is the question every ownership check actually has:
// an exact match, or failing that the closest verified ancestor.
//
// A verified example.com covers mail.example.com, because controlling
// a zone controls every name under it, and every provider assumes the
// same. The usual bounce pattern depends on it: the envelope sender
// sits on a subdomain with its own MX so the apex keeps receiving real
// mail.
//
// Most specific wins, so a subdomain verified separately by another
// project stays theirs - a parent must not absorb a child somebody
// else proved.
//
// Matching is by whole LABELS, never by string suffix - see
// dnsname.Covering, which a relay node uses for the same question.
func (s *Store) GetVerifiedCovering(ctx context.Context, name string) (*dmodel.Domain, error) {
	for _, candidate := range dnsname.Covering(name) {
		d, err := s.GetVerifiedByName(ctx, candidate)
		if err != nil || d != nil {
			return d, err
		}
	}

	return nil, nil
}

// VerifiedNames lists every verified domain in the installation.
//
// It answers one question, and only for a relay node running an MX of
// its own: which recipient domains may that node accept mail for. A
// node holds no database, so the alternative is a lookup per RCPT
// over the very link whose unreliability is why the MX was moved out
// there in the first place.
//
// NAMES ONLY, deliberately. A node learns what it may accept and
// nothing about who owns it - the project is resolved here, when the
// message arrives, by the same GetVerifiedCovering every other
// ownership check goes through. So a node cannot be read as a
// directory of who this installation hosts.
//
// Sorted, because the caller fingerprints the list to decide whether
// it has to travel. An unstable order would resend it every time.
func (s *Store) VerifiedNames(ctx context.Context) ([]string, error) {
	rows, err := s.Query(ctx, `SELECT domain FROM domains WHERE verified = TRUE ORDER BY domain ASC`)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}

		out = append(out, name)
	}

	return out, rows.Err()
}

// VerifiedNamesIn is the same accept list for a project's own node.
//
// Separate from VerifiedNames rather than one method with an empty
// means-everything parameter, because the two answers are not on one
// scale: domains.project_id is NOT NULL, so an empty string is not a
// scope here the way it is for relay_nodes.List. One method would
// have "" meaning the whole installation on one table and the
// platform's own rows on another, which is exactly the kind of
// overload that ends in a tenant node being handed every name.
func (s *Store) VerifiedNamesIn(ctx context.Context, projID string) ([]string, error) {
	rows, err := s.Query(ctx,
		`SELECT domain FROM domains WHERE project_id = ? AND verified = TRUE ORDER BY domain ASC`, projID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}

		out = append(out, name)
	}

	return out, rows.Err()
}

// GetByName finds a domain regardless of verification state, used by
// the claim flow to detect names already taken by any project.
func (s *Store) GetByName(ctx context.Context, name string) (*dmodel.Domain, error) {
	row := s.QueryRow(ctx, domainSelect+` WHERE domain = ?`, strings.ToLower(name))
	d, err := s.scanDomain(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return d, err
}

// List returns every domain in projID.
func (s *Store) List(ctx context.Context, projID string) ([]*dmodel.Domain, error) {
	rows, err := s.Query(ctx, domainSelect+` WHERE project_id = ? ORDER BY domain ASC`, projID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	var out []*dmodel.Domain
	for rows.Next() {
		d, err := s.scanDomain(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, d)
	}

	return out, rows.Err()
}

// Put inserts the domain, or updates the row when its id already
// exists.
func (s *Store) Put(ctx context.Context, d *dmodel.Domain) error {
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now().UTC()
	}

	// Seal the signing key before it reaches the database. An empty
	// key stays empty rather than becoming the encryption of "" -
	// otherwise CanSign would see a non-empty column for a domain that
	// has no key at all.
	sealed := ""
	if d.DKIMPrivateKey != "" {
		var err error
		sealed, err = s.crypto.Encrypt(d.DKIMPrivateKey)
		if err != nil {
			return fmt.Errorf("domains: seal dkim key: %w", err)
		}
	}

	_, err := s.Exec(ctx, `
        INSERT INTO domains (id, project_id, created_by, domain, verification_token, verified, verified_at, created_at,
                             dkim_selector, dkim_private_key, dkim_public_key,
                             spf_verified, dkim_verified, dmarc_verified, checked_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            domain           = excluded.domain,
            verified         = excluded.verified,
            verified_at      = excluded.verified_at,
            dkim_selector    = excluded.dkim_selector,
            dkim_private_key = excluded.dkim_private_key,
            dkim_public_key  = excluded.dkim_public_key,
            spf_verified     = excluded.spf_verified,
            dkim_verified    = excluded.dkim_verified,
            dmarc_verified   = excluded.dmarc_verified,
            checked_at       = excluded.checked_at
    `, d.ID, d.ProjectID, d.CreatedBy, strings.ToLower(d.Domain),
		d.VerificationToken, d.Verified, database.NullTime(d.VerifiedAt), d.CreatedAt,
		d.DKIMSelector, sealed, d.DKIMPublicKey,
		d.SPFVerified, d.DKIMVerified, d.DMARCVerified, database.NullTime(d.CheckedAt))

	return err
}

// SetVerified flips the verification flag and stamps when. The only
// writer of that pair, so a domain cannot be marked verified by an
// ordinary Put.
func (s *Store) SetVerified(ctx context.Context, projID, id string, verified bool, at time.Time) error {
	_, err := s.Exec(ctx, `
        UPDATE domains SET verified = ?, verified_at = ?
        WHERE project_id = ? AND id = ?`, verified, at, projID, id)

	return err
}

// Delete removes one domain from projID.
func (s *Store) Delete(ctx context.Context, projID, id string) error {
	_, err := s.Exec(ctx, `DELETE FROM domains WHERE project_id = ? AND id = ?`, projID, id)

	return err
}

// Count reports how many rows the project holds, for plan caps.
func (s *Store) Count(ctx context.Context, projID string) (int, error) {
	var n int
	err := s.QueryRow(ctx, `SELECT COUNT(*) FROM domains WHERE project_id = ?`, projID).Scan(&n)

	return n, err
}

// scanDomain is a method rather than a free function because it has
// to unseal the DKIM key, and the crypto service lives on the store.
// Callers therefore always receive PEM and never ciphertext.
func (s *Store) scanDomain(r interface{ Scan(...any) error }) (*dmodel.Domain, error) {
	var d dmodel.Domain
	var sealed string
	if err := r.Scan(&d.ID, &d.ProjectID, &d.CreatedBy, &d.Domain,
		&d.VerificationToken, &d.Verified, &d.VerifiedAt, &d.CreatedAt,
		&d.DKIMSelector, &sealed, &d.DKIMPublicKey,
		&d.SPFVerified, &d.DKIMVerified, &d.DMARCVerified, &d.CheckedAt); err != nil {
		return nil, err
	}

	if sealed != "" {
		plain, err := s.crypto.Decrypt(sealed)
		if err != nil {
			// Loud, not silent. Returning the row with an empty key
			// would leave CanSign false and mail would quietly go out
			// unsigned, which is the failure mode hardest to notice.
			return nil, fmt.Errorf("domains: unseal dkim key for %q: %w", d.Domain, err)
		}

		d.DKIMPrivateKey = plain
	}

	return &d, nil
}

// LookupTXT is the DNS dependency of the verify endpoint, swappable
// in tests. The default uses the system resolver.
type LookupTXT func(ctx context.Context, name string) ([]string, error)

// DefaultLookupTXT resolves with the system resolver.
func DefaultLookupTXT(ctx context.Context, name string) ([]string, error) {
	return net.DefaultResolver.LookupTXT(ctx, name)
}

// CheckOwnership reports whether the domain's apex TXT records
// contain the expected mailyard-verification token.
func CheckOwnership(ctx context.Context, lookup LookupTXT, d *dmodel.Domain) bool {
	records, err := lookup(ctx, d.Domain)
	if err != nil {
		return false
	}

	want := d.TXTRecordValue()
	for _, txt := range records {
		if strings.TrimSpace(txt) == want {
			return true
		}
	}

	return false
}

// Handler owns the /api/domains surface.
type Handler struct {
	Runtime *env.Runtime
	Lookup  LookupTXT // defaults to DefaultLookupTXT when nil.
}

func (h *Handler) lookup() LookupTXT {
	if h.Lookup != nil {
		return h.Lookup
	}

	return DefaultLookupTXT
}

// List serves GET /api/v1/domains.
func (h *Handler) List(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	out, err := h.Runtime.Store.Domain.List(c.UserContext(), rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if out == nil {
		out = []*dmodel.Domain{}
	}

	return response.Success(c, ListResponse{Domains: out})
}

// Get serves GET /api/v1/domains/:id.
func (h *Handler) Get(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	d, err := h.Runtime.Store.Domain.Get(c.UserContext(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if d == nil {
		return response.NotFound(c, "domain not found")
	}

	return response.Success(c, h.domainPayload(d))
}

// Create serves POST /api/v1/domains.
func (h *Handler) Create(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[createInput](c)
	if !ok {
		return resp
	}

	if err := quota.CheckResource(c.UserContext(), h.Runtime.Store, rc.Project.ID, quota.ResDomains, 1); err != nil {
		if qe, ok := errors.AsType[*quota.Error](err); ok {
			return response.TooManyRequests(c, qe.Error())
		}

		return response.Internal(c, err)
	}

	// Names are globally unique - a domain claimed by another
	// project must look taken, not missing.
	existing, err := h.Runtime.Store.Domain.GetByName(c.UserContext(), in.Domain)
	if err != nil {
		return response.Internal(c, err)
	}

	if existing != nil {
		return response.Conflict(c, "this domain is already claimed")
	}

	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return response.Internal(c, err)
	}

	d := &dmodel.Domain{
		ID:                ids.New(),
		ProjectID:         rc.Project.ID,
		CreatedBy:         userID(rc),
		Domain:            strings.ToLower(in.Domain),
		VerificationToken: hex.EncodeToString(token),
	}
	if err := h.Runtime.Store.Domain.Put(c.UserContext(), d); err != nil {
		return response.Internal(c, err)
	}

	return response.Created(c, h.domainPayload(d))
}

// Verify runs the live DNS TXT check and stores the outcome. Safe to
// call repeatedly - a lost record un-verifies the domain again.
func (h *Handler) Verify(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	d, err := h.Runtime.Store.Domain.Get(c.UserContext(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if d == nil {
		return response.NotFound(c, "domain not found")
	}

	// Four lookups, so a longer budget than the single-record check
	// this replaced. Still bounded: an unreachable resolver must fail
	// the request, not hold a console connection open.
	ctx, cancel := context.WithTimeout(c.UserContext(), 20*time.Second)
	defer cancel()

	res := CheckAll(ctx, h.lookup(), d)
	now := time.Now().UTC()

	// Mint the signing key the moment ownership is proven, not when
	// the operator asks for it. The public half is useless to anyone
	// else and the private half never leaves the database, so there is
	// nothing to weigh against having the record ready to publish on
	// the same screen that just went green.
	minted := false
	if res.Ownership && d.DKIMPrivateKey == "" {
		priv, pub, err := dkim.GenerateKey()
		if err != nil {
			return response.Internal(c, err)
		}

		d.DKIMSelector = dkim.DefaultSelector
		d.DKIMPrivateKey = priv
		d.DKIMPublicKey = pub
		minted = true
		// The record cannot possibly be published yet, so do not let
		// the check that just ran say otherwise.
		res.DKIM = false
	}

	d.Verified = res.Ownership
	if res.Ownership {
		d.VerifiedAt = &now
	}

	d.SPFVerified = res.SPF
	d.DKIMVerified = res.DKIM
	d.DMARCVerified = res.DMARC
	d.CheckedAt = &now

	if err := h.Runtime.Store.Domain.Put(c.UserContext(), d); err != nil {
		return response.Internal(c, err)
	}

	if minted {
		slog.Info("domains: dkim key generated",
			"domain", d.Domain, "project_id", d.ProjectID, "selector", d.DKIMSelector)
	}

	return response.Success(c, h.domainPayload(d))
}

// Delete serves DELETE /api/v1/domains/:id.
func (h *Handler) Delete(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	d, err := h.Runtime.Store.Domain.Get(c.UserContext(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if d == nil {
		return response.NotFound(c, "domain not found")
	}

	if err := h.Runtime.Store.Domain.Delete(c.UserContext(), rc.Project.ID, d.ID); err != nil {
		return response.Internal(c, err)
	}

	return response.NoContent(c)
}

// domainPayload is the response shape for a single domain: the row plus
// every DNS record the operator needs, each with its current state.
//
// dns_records replaces the old single dns_record, which only ever
// carried the ownership TXT. The docs have described the plural shape
// with spf/dkim/dmarc entries since before the rewrite - this is the
// API catching up rather than a new invention.
func (h *Handler) domainPayload(d *dmodel.Domain) DetailResponse {
	return DetailResponse{
		Domain:     d,
		DNSRecords: Records(d, h.Runtime.Config.Sending.SPFInclude),
	}
}

func userID(rc *domain.RequestContext) string {
	if rc.User != nil {
		return rc.User.ID
	}

	return ""
}
