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

	"github.com/gofiber/fiber/v3"
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
       dkim_next_selector, dkim_next_private_key, dkim_next_public_key,
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
	row := s.QueryRow(ctx, domainSelect+` WHERE domain = ? AND verified = TRUE`, normalizeName(name))
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
// Most specific wins. A verified domain holds its zone, so another
// project can no longer claim a name inside it (ZoneTakenByAnother),
// and two projects nested in one zone exist only where the rows
// predate that rule - there the child keeps its own subtree.
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

// GetVerifiedCoveringFor answers whether projID may SEND as name: the
// covering verified row exactly as GetVerifiedCovering finds it, when
// projID owns it or the owner shared it with projID, and nil otherwise.
//
// A grant covers the owner's whole zone. When the owner verified
// auth.example.com beside example.com, a project the apex was shared
// with may send from auth.example.com too: the grant is asked of every
// row on the way up that belongs to the same owner. The most specific
// row is what is returned, so its key signs.
func (s *Store) GetVerifiedCoveringFor(ctx context.Context, name, projID string) (*dmodel.Domain, error) {
	var chain []*dmodel.Domain
	for _, candidate := range dnsname.Covering(name) {
		d, err := s.GetVerifiedByName(ctx, candidate)
		if err != nil {
			return nil, err
		}

		if d != nil {
			chain = append(chain, d)
		}
	}

	if len(chain) == 0 {
		return nil, nil
	}

	d := chain[0]
	if d.ProjectID == projID {
		return d, nil
	}

	owned := make([]string, 0, len(chain))
	for _, row := range chain {
		if row.ProjectID == d.ProjectID {
			owned = append(owned, row.ID)
		}
	}

	var granted bool
	if err := s.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM domain_grants WHERE project_id = ? AND domain_id = ANY(?::uuid[]))`,
		projID, owned).Scan(&granted); err != nil {
		return nil, err
	}

	if !granted {
		return nil, nil
	}

	return d, nil
}

// ZoneTakenByAnother reports whether another project verified name, a
// domain above it or a domain below it. A verified domain holds its
// whole zone: whoever controls example.com controls every name under
// it, so nobody else may claim auth.example.com, and a project may not
// verify example.com while another holds a name inside it. A subdomain
// somebody else needs is verified by the owner and shared.
func (s *Store) ZoneTakenByAnother(ctx context.Context, name, projID string) (bool, error) {
	name = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
	var taken bool
	err := s.QueryRow(ctx, `
        SELECT EXISTS (
            SELECT 1 FROM domains
            WHERE verified = TRUE AND project_id <> ?
              AND (domain = ANY(?::text[]) OR domain LIKE ?)
        )`, projID, dnsname.Covering(name), "%."+database.EscapeLike(name)).Scan(&taken)

	return taken, err
}

// zoneTaken is the refusal for a name inside, or above, a zone another
// project verified. One wording for both directions.
const zoneTaken = "another project verified a domain in this zone - ask its owner to verify the name and share it with this project"

// Grant shares a domain with another project. A repeat is a no-op,
// so the caller does not have to ask first.
func (s *Store) Grant(ctx context.Context, g *dmodel.Grant) error {
	if g.CreatedAt.IsZero() {
		g.CreatedAt = time.Now().UTC()
	}

	_, err := s.Exec(ctx, `
        INSERT INTO domain_grants (domain_id, project_id, granted_by, created_at)
        VALUES (?, ?, ?, ?)
        ON CONFLICT (domain_id, project_id) DO NOTHING`,
		g.DomainID, g.ProjectID, g.GrantedBy, g.CreatedAt)

	return err
}

// Revoke takes a grant back and reports whether there was one.
func (s *Store) Revoke(ctx context.Context, domainID, projID string) (bool, error) {
	res, err := s.Exec(ctx, `DELETE FROM domain_grants WHERE domain_id = ? AND project_id = ?`, domainID, projID)
	if err != nil {
		return false, err
	}

	n, err := res.RowsAffected()

	return n > 0, err
}

// ListGrants lists who one domain is shared with, scoped to its OWNER:
// a domain id another project owns answers nothing.
func (s *Store) ListGrants(ctx context.Context, ownerProjID, domainID string) ([]*dmodel.Grant, error) {
	rows, err := s.Query(ctx, `
        SELECT g.domain_id, g.project_id, p.name, p.slug, g.granted_by, g.created_at
        FROM domain_grants g
        JOIN domains d ON d.id = g.domain_id
        JOIN projects p ON p.id = g.project_id
        WHERE d.project_id = ? AND g.domain_id = ?
        ORDER BY g.created_at ASC`, ownerProjID, domainID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	out := []*dmodel.Grant{}
	for rows.Next() {
		var g dmodel.Grant
		if err := rows.Scan(&g.DomainID, &g.ProjectID, &g.ProjectName, &g.ProjectSlug, &g.GrantedBy, &g.CreatedAt); err != nil {
			return nil, err
		}

		out = append(out, &g)
	}

	return out, rows.Err()
}

// ListShared lists the verified domains other projects shared with
// projID, with the owner's name and nothing of the records.
func (s *Store) ListShared(ctx context.Context, projID string) ([]*dmodel.Shared, error) {
	rows, err := s.Query(ctx, `
        SELECT d.id, d.domain, p.name, g.created_at
        FROM domain_grants g
        JOIN domains d ON d.id = g.domain_id
        JOIN projects p ON p.id = d.project_id
        WHERE g.project_id = ? AND d.verified = TRUE
        ORDER BY d.domain ASC`, projID)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()
	out := []*dmodel.Shared{}
	for rows.Next() {
		var sh dmodel.Shared
		if err := rows.Scan(&sh.ID, &sh.Domain, &sh.OwnerName, &sh.CreatedAt); err != nil {
			return nil, err
		}

		out = append(out, &sh)
	}

	return out, rows.Err()
}

// VerifiedNames lists every verified domain in the installation.
//
// It answers one question, and only for a relay node running an MX of
// its own: which recipient domains may that node accept mail for. A
// node holds no database, so the alternative is a lookup per RCPT
// over the very link whose unreliability is why the MX sits out there.
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

// GetByNameIn finds projID's own claim of a name, verified or not.
// Another project's unverified claim of the same name is not an
// obstacle - see DropStaleClaims.
func (s *Store) GetByNameIn(ctx context.Context, projID, name string) (*dmodel.Domain, error) {
	row := s.QueryRow(ctx, domainSelect+` WHERE project_id = ? AND domain = ?`, projID, normalizeName(name))
	d, err := s.scanDomain(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return d, err
}

// DropStaleClaims removes other projects' UNVERIFIED claims of name and
// of every name below it, called once projID verified name. The zone is
// projID's now, so none of them could ever verify.
func (s *Store) DropStaleClaims(ctx context.Context, name, projID string) (int64, error) {
	name = normalizeName(name)
	res, err := s.Exec(ctx, `
        DELETE FROM domains
        WHERE verified = FALSE AND project_id <> ?
          AND (domain = ? OR domain LIKE ?)`, projID, name, "%."+database.EscapeLike(name))
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

// normalizeName is the stored spelling of a domain: trimmed, lowercase,
// no trailing dot.
func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
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
	sealed, err := s.seal(d.DKIMPrivateKey)
	if err != nil {
		return err
	}

	sealedNext, err := s.seal(d.DKIMNextPrivateKey)
	if err != nil {
		return err
	}

	_, err = s.Exec(ctx, `
        INSERT INTO domains (id, project_id, created_by, domain, verification_token, verified, verified_at, created_at,
                             dkim_selector, dkim_private_key, dkim_public_key,
                             dkim_next_selector, dkim_next_private_key, dkim_next_public_key,
                             spf_verified, dkim_verified, dmarc_verified, checked_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            domain                = excluded.domain,
            verified              = excluded.verified,
            verified_at           = excluded.verified_at,
            dkim_selector         = excluded.dkim_selector,
            dkim_private_key      = excluded.dkim_private_key,
            dkim_public_key       = excluded.dkim_public_key,
            dkim_next_selector    = excluded.dkim_next_selector,
            dkim_next_private_key = excluded.dkim_next_private_key,
            dkim_next_public_key  = excluded.dkim_next_public_key,
            spf_verified          = excluded.spf_verified,
            dkim_verified         = excluded.dkim_verified,
            dmarc_verified        = excluded.dmarc_verified,
            checked_at            = excluded.checked_at
    `, d.ID, d.ProjectID, d.CreatedBy, normalizeName(d.Domain),
		d.VerificationToken, d.Verified, database.NullTime(d.VerifiedAt), d.CreatedAt,
		d.DKIMSelector, sealed, d.DKIMPublicKey,
		d.DKIMNextSelector, sealedNext, d.DKIMNextPublicKey,
		d.SPFVerified, d.DKIMVerified, d.DMARCVerified, database.NullTime(d.CheckedAt))

	return err
}

// seal encrypts a private key for the row. An empty key stays empty
// rather than becoming the encryption of "", or CanSign would see a
// key where there is none.
func (s *Store) seal(pem string) (string, error) {
	if pem == "" {
		return "", nil
	}

	sealed, err := s.crypto.Encrypt(pem)
	if err != nil {
		return "", fmt.Errorf("domains: seal dkim key: %w", err)
	}

	return sealed, nil
}

// unseal is the reverse, loud on failure: a row read with an empty
// key would send mail unsigned, which is the failure hardest to notice.
func (s *Store) unseal(name, sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}

	plain, err := s.crypto.Decrypt(sealed)
	if err != nil {
		return "", fmt.Errorf("domains: unseal dkim key for %q: %w", name, err)
	}

	return plain, nil
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
	var sealed, sealedNext string
	if err := r.Scan(&d.ID, &d.ProjectID, &d.CreatedBy, &d.Domain,
		&d.VerificationToken, &d.Verified, &d.VerifiedAt, &d.CreatedAt,
		&d.DKIMSelector, &sealed, &d.DKIMPublicKey,
		&d.DKIMNextSelector, &sealedNext, &d.DKIMNextPublicKey,
		&d.SPFVerified, &d.DKIMVerified, &d.DMARCVerified, &d.CheckedAt); err != nil {
		return nil, err
	}

	var err error
	if d.DKIMPrivateKey, err = s.unseal(d.Domain, sealed); err != nil {
		return nil, err
	}

	if d.DKIMNextPrivateKey, err = s.unseal(d.Domain, sealedNext); err != nil {
		return nil, err
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
func (h *Handler) List(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	out, err := h.Runtime.Store.Domain.List(c.Context(), rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if out == nil {
		out = []*dmodel.Domain{}
	}

	shared, err := h.Runtime.Store.Domain.ListShared(c.Context(), rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, ListResponse{Domains: out, Shared: shared})
}

// Get serves GET /api/v1/domains/:id.
func (h *Handler) Get(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	d, err := h.Runtime.Store.Domain.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if d == nil {
		return response.NotFound(c, "domain not found")
	}

	return response.Success(c, h.domainPayload(d))
}

// Create serves POST /api/v1/domains.
func (h *Handler) Create(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[createInput](c)
	if !ok {
		return resp
	}

	release, err := quota.HoldResource(c.Context(), h.Runtime.Store, rc.Project.ID, quota.ResDomains, 1)
	if err != nil {
		if qe, ok := errors.AsType[*quota.Error](err); ok {
			return response.TooManyRequests(c, qe.Error())
		}

		return response.Internal(c, err)
	}

	defer release()

	// A claim is per project until it verifies, so only this project's
	// own row is a duplicate. A name another project VERIFIED is refused
	// by the zone check below.
	existing, err := h.Runtime.Store.Domain.GetByNameIn(c.Context(), rc.Project.ID, in.Domain)
	if err != nil {
		return response.Internal(c, err)
	}

	if existing != nil {
		return response.Conflict(c, "this project already claimed this domain")
	}

	taken, err := h.Runtime.Store.Domain.ZoneTakenByAnother(c.Context(), in.Domain, rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if taken {
		return response.Conflict(c, zoneTaken)
	}

	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return response.Internal(c, err)
	}

	d := &dmodel.Domain{
		ID:                ids.New(),
		ProjectID:         rc.Project.ID,
		CreatedBy:         userID(rc),
		Domain:            normalizeName(in.Domain),
		VerificationToken: hex.EncodeToString(token),
	}
	if err := h.Runtime.Store.Domain.Put(c.Context(), d); err != nil {
		return response.Internal(c, err)
	}

	return response.Created(c, h.domainPayload(d))
}

// Verify runs the live DNS TXT check and stores the outcome. Safe to
// call repeatedly - a lost record un-verifies the domain again.
func (h *Handler) Verify(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	d, err := h.Runtime.Store.Domain.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if d == nil {
		return response.NotFound(c, "domain not found")
	}

	// Checked again on the way to verified, because the zone can be
	// taken between the claim and this call. A domain that is already
	// verified is only re-checking its records and is left alone.
	if !d.Verified {
		taken, err := h.Runtime.Store.Domain.ZoneTakenByAnother(c.Context(), d.Domain, rc.Project.ID)
		if err != nil {
			return response.Internal(c, err)
		}

		if taken {
			return response.Conflict(c, zoneTaken)
		}
	}

	// Several lookups, so a longer budget than a single-record check.
	// Still bounded: an unreachable resolver must fail the request,
	// not hold a console connection open.
	ctx, cancel := context.WithTimeout(c.Context(), 20*time.Second)
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

	wasVerified := d.Verified
	d.Verified = res.Ownership
	if res.Ownership {
		d.VerifiedAt = &now
	}

	rotated := cutOver(d, res)
	d.SPFVerified = res.SPF
	d.DKIMVerified = res.DKIM || rotated
	d.DMARCVerified = res.DMARC
	d.CheckedAt = &now

	// The partial unique index on verified names refuses a second
	// project verifying the same name at the same moment, which answers
	// 409 through response.Internal.
	if err := h.Runtime.Store.Domain.Put(c.Context(), d); err != nil {
		return response.Internal(c, err)
	}

	if d.Verified && !wasVerified {
		dropped, err := h.Runtime.Store.Domain.DropStaleClaims(c.Context(), d.Domain, d.ProjectID)
		if err != nil {
			return response.Internal(c, err)
		}

		if dropped > 0 {
			slog.Info("domains: stale claims of other projects dropped",
				"domain", d.Domain, "project_id", d.ProjectID, "dropped", dropped)
		}
	}

	if minted {
		slog.Info("domains: dkim key generated",
			"domain", d.Domain, "project_id", d.ProjectID, "selector", d.DKIMSelector)
	}

	if rotated {
		slog.Info("domains: dkim key rotated",
			"domain", d.Domain, "project_id", d.ProjectID, "selector", d.DKIMSelector)
	}

	return response.Success(c, h.domainPayload(d))
}

// cutOver makes the pending key the signing key once its record is
// published, and reports whether it did. The old record can come
// down once mail signed under it has been delivered.
func cutOver(d *dmodel.Domain, res CheckResult) bool {
	if !d.Rotating() || !res.DKIMNext {
		return false
	}

	d.DKIMSelector, d.DKIMPrivateKey, d.DKIMPublicKey = d.DKIMNextSelector, d.DKIMNextPrivateKey, d.DKIMNextPublicKey
	d.DKIMNextSelector, d.DKIMNextPrivateKey, d.DKIMNextPublicKey = "", "", ""

	return true
}

// RotateDKIM serves POST /api/v1/domains/:id/dkim/rotate: mints the
// next keypair under the other selector. Signing stays on the current
// key until verify sees the new record. Calling it again replaces a
// pending key that was never published.
func (h *Handler) RotateDKIM(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	d, err := h.Runtime.Store.Domain.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if d == nil {
		return response.NotFound(c, "domain not found")
	}

	if !d.CanSign() {
		return response.Conflict(c, "the domain has no signing key to rotate - verify ownership first")
	}

	priv, pub, err := dkim.GenerateKey()
	if err != nil {
		return response.Internal(c, err)
	}

	d.DKIMNextSelector = dkim.NextSelector(d.DKIMSelector)
	d.DKIMNextPrivateKey = priv
	d.DKIMNextPublicKey = pub
	if err := h.Runtime.Store.Domain.Put(c.Context(), d); err != nil {
		return response.Internal(c, err)
	}

	slog.Info("domains: dkim rotation started",
		"domain", d.Domain, "project_id", d.ProjectID, "selector", d.DKIMNextSelector)

	return response.Success(c, h.domainPayload(d))
}

// CancelDKIMRotation serves DELETE /api/v1/domains/:id/dkim/rotate:
// discards a pending key. The current key is untouched.
func (h *Handler) CancelDKIMRotation(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	d, err := h.Runtime.Store.Domain.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if d == nil {
		return response.NotFound(c, "domain not found")
	}

	if !d.Rotating() {
		return response.Conflict(c, "no rotation is pending")
	}

	d.DKIMNextSelector, d.DKIMNextPrivateKey, d.DKIMNextPublicKey = "", "", ""
	if err := h.Runtime.Store.Domain.Put(c.Context(), d); err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, h.domainPayload(d))
}

// Delete serves DELETE /api/v1/domains/:id.
func (h *Handler) Delete(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	d, err := h.Runtime.Store.Domain.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if d == nil {
		return response.NotFound(c, "domain not found")
	}

	if err := h.Runtime.Store.Domain.Delete(c.Context(), rc.Project.ID, d.ID); err != nil {
		return response.Internal(c, err)
	}

	return response.NoContent(c)
}

// domainPayload is the response shape for a single domain: the row plus
// every DNS record the operator needs, each with its current state.
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
