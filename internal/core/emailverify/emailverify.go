// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package emailverify judges whether an address is worth sending to.
//
// What it does not do matters as much as what it does: there is no SMTP
// RCPT probe. Probing mailboxes gets a sender's IP blocklisted, many
// providers accept and then bounce so the answer is a lie anyway, and it
// tells the address's operator that you were interested in it. So
// `mailbox_verified` is permanently false and `checks.smtp` permanently
// "skipped". A valid verdict means the address is well formed and its
// domain accepts mail, not that the mailbox exists.
//
// The intrinsic checks (syntax, disposable, role, MX) depend only on
// the address, so they are cached. Per-project facts (suppressed,
// previously bounced) are layered on fresh every call - a cached
// verdict must never claim an address is fine after you suppressed
// it a second ago.
package emailverify

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"time"
)

// Verdicts.
const (
	StatusValid      = "valid"
	StatusInvalid    = "invalid"
	StatusRisky      = "risky"
	StatusDisposable = "disposable"
	StatusUnknown    = "unknown"
)

// Checks is the per-check breakdown.
type Checks struct {
	Syntax      bool `json:"syntax"`
	MX          bool `json:"mx"`
	Disposable  bool `json:"disposable"`
	RoleAccount bool `json:"role_account"`

	// SMTP is always "skipped" - see the package comment.
	SMTP string `json:"smtp"`
}

// Result is one verification outcome.
type Result struct {
	Email  string `json:"email"`
	Status string `json:"status"`
	Score  int    `json:"score"`
	Checks Checks `json:"checks"`
	Reason string `json:"reason,omitempty"`

	// MailboxVerified is always false. Kept in the response so a
	// client cannot mistake a valid verdict for mailbox existence.
	MailboxVerified   bool      `json:"mailbox_verified"`
	Suppressed        bool      `json:"suppressed"`
	PreviouslyBounced bool      `json:"previously_bounced"`
	Cached            bool      `json:"cached"`
	CheckedAt         time.Time `json:"checked_at"`
}

// Resolver looks up a domain's mail exchangers. Swapped in tests.
type Resolver interface {
	LookupMX(ctx context.Context, domain string) ([]*net.MX, error)
	LookupHost(ctx context.Context, host string) ([]string, error)
}

type netResolver struct{ r *net.Resolver }

// LookupMX resolves the MX records for d. Split out so a test can
// answer without touching DNS.
func (n netResolver) LookupMX(ctx context.Context, d string) ([]*net.MX, error) {
	return n.r.LookupMX(ctx, d)
}

// LookupHost resolves h to addresses, which is the A/AAAA fallback RFC
// 5321 allows when a domain publishes no MX.
func (n netResolver) LookupHost(ctx context.Context, h string) ([]string, error) {
	return n.r.LookupHost(ctx, h)
}

// Config tunes the verifier.
type Config struct {
	// CacheTTL bounds how long an intrinsic result is reused.
	CacheTTL time.Duration

	// MXCacheTTL bounds the per-domain MX answer.
	MXCacheTTL time.Duration

	// LookupTimeout bounds one DNS resolution.
	LookupTimeout time.Duration
}

// Verifier runs the checks.
type Verifier struct {
	cfg      Config
	resolver Resolver

	mu       sync.RWMutex
	results  map[string]cachedResult
	mxAnswer map[string]cachedMX
}

type cachedResult struct {
	res Result
	at  time.Time
}

type cachedMX struct {
	ok bool
	at time.Time
}

// New builds a Verifier with sane defaults for any unset field.
func New(cfg Config) *Verifier {
	if cfg.CacheTTL <= 0 {
		cfg.CacheTTL = 24 * time.Hour
	}

	if cfg.MXCacheTTL <= 0 {
		cfg.MXCacheTTL = time.Hour
	}

	if cfg.LookupTimeout <= 0 {
		cfg.LookupTimeout = 5 * time.Second
	}

	return &Verifier{
		cfg:      cfg,
		resolver: netResolver{r: net.DefaultResolver},
		results:  map[string]cachedResult{},
		mxAnswer: map[string]cachedMX{},
	}
}

// WithResolver swaps the DNS resolver, for tests.
func (v *Verifier) WithResolver(r Resolver) *Verifier {
	v.resolver = r

	return v
}

// Check runs the intrinsic checks for one address. Per-project
// facts are the caller's job to layer on - see the package comment.
//
// Checks run cheapest-first and stop as soon as the verdict is
// settled, so a malformed address or a known disposable domain never
// costs a DNS lookup.
func (v *Verifier) Check(ctx context.Context, email string, fresh bool) Result {
	addr := strings.ToLower(strings.TrimSpace(email))
	now := time.Now().UTC()

	if !fresh {
		if hit, ok := v.cachedResult(addr, now); ok {
			hit.Cached = true

			return hit
		}
	}

	res := Result{
		Email:     addr,
		Checks:    Checks{SMTP: "skipped"},
		CheckedAt: now,
	}

	local, domain, ok := splitAddress(addr)
	if !ok {
		res.Status = StatusInvalid
		res.Score = 0
		res.Reason = "the address is not syntactically valid"
		v.store(addr, res, now)

		return res
	}

	res.Checks.Syntax = true

	if isDisposable(domain) {
		res.Status = StatusDisposable
		res.Score = 10
		res.Checks.Disposable = true
		res.Reason = "the domain is a known disposable mail provider"
		v.store(addr, res, now)

		return res
	}

	res.Checks.RoleAccount = isRoleAccount(local)

	hasMX, resolved := v.domainAcceptsMail(ctx, domain, now, fresh)
	res.Checks.MX = hasMX
	switch {
	case !resolved:
		// The lookup itself failed (timeout, server failure). That is
		// not evidence the domain is bad, so it must not be reported
		// as invalid.
		res.Status = StatusUnknown
		res.Score = 50
		res.Reason = "the domain's mail servers could not be resolved right now"

		// Deliberately not cached: a transient DNS failure should not
		// stick to the address for a day.
		return res
	case !hasMX:
		res.Status = StatusInvalid
		res.Score = 0
		res.Reason = "the domain has no mail servers"
	case res.Checks.RoleAccount:
		res.Status = StatusRisky
		res.Score = 60
		res.Reason = "this looks like a role account rather than a person"
	default:
		res.Status = StatusValid
		res.Score = 90
	}

	v.store(addr, res, now)

	return res
}

func (v *Verifier) cachedResult(addr string, now time.Time) (Result, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	hit, ok := v.results[addr]
	if !ok || now.Sub(hit.at) > v.cfg.CacheTTL {
		return Result{}, false
	}

	return hit.res, true
}

func (v *Verifier) store(addr string, res Result, now time.Time) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.results[addr] = cachedResult{res: res, at: now}
	if len(v.results) > maxCacheEntries {
		v.evictLocked(now)
	}
}

// maxCacheEntries bounds memory, for BOTH caches. They are an
// optimization, so dropping one wholesale under pressure is acceptable -
// the next call simply re-checks.
//
// The MX cache needs the bound as much as the result cache: its keys
// are domains, verify is a read-tier route, and a wildcard DNS zone
// hands a caller as many resolvable domains as they care to send.
const maxCacheEntries = 10000

func (v *Verifier) evictLocked(now time.Time) {
	for k, e := range v.results {
		if now.Sub(e.at) > v.cfg.CacheTTL {
			delete(v.results, k)
		}
	}

	// Still oversized after dropping the stale entries: clear it
	// rather than grow without bound.
	if len(v.results) > maxCacheEntries {
		v.results = map[string]cachedResult{}
	}

	for k, e := range v.mxAnswer {
		if now.Sub(e.at) > v.cfg.MXCacheTTL {
			delete(v.mxAnswer, k)
		}
	}

	if len(v.mxAnswer) > maxCacheEntries {
		v.mxAnswer = map[string]cachedMX{}
	}
}

// domainAcceptsMail reports whether the domain has an MX record, or
// an A/AAAA record which RFC 5321 treats as an implicit MX. The
// second return says whether the lookup succeeded at all.
func (v *Verifier) domainAcceptsMail(ctx context.Context, domain string, now time.Time, fresh bool) (hasMX, resolved bool) {
	if !fresh {
		v.mu.RLock()
		hit, ok := v.mxAnswer[domain]
		v.mu.RUnlock()
		if ok && now.Sub(hit.at) <= v.cfg.MXCacheTTL {
			return hit.ok, true
		}
	}

	ctx, cancel := context.WithTimeout(ctx, v.cfg.LookupTimeout)
	defer cancel()

	mx, err := v.resolver.LookupMX(ctx, domain)
	if err == nil && len(mx) > 0 {
		// A "null MX" is a domain SAYING it accepts no mail, and it looked
		// like a mail server here: RFC 7505 spells that as a single record
		// with an empty target ("MX 0 ."), which Go renders as the host
		// ".". Counting it as deliverable scored such an address valid at
		// 90, when the domain owner has published the opposite.
		//
		// It also must not fall through to the A/AAAA fallback below. That
		// fallback is for a domain with NO MX record, and a null MX is the
		// presence of one - RFC 7505 exists precisely to stop the implicit
		// A lookup.
		if isNullMX(mx) {
			v.storeMX(domain, false, now)

			return false, true
		}

		v.storeMX(domain, true, now)

		return true, true
	}

	// A DNS error that says the name does not exist is a real answer -
	// anything else (timeout, SERVFAIL) is not.
	if err != nil {
		if dnsErr, ok := errors.AsType[*net.DNSError](err); ok && !dnsErr.IsNotFound {
			return false, false
		}
	}

	// No MX at all: fall back to A/AAAA per RFC 5321.
	hosts, herr := v.resolver.LookupHost(ctx, domain)
	if herr == nil && len(hosts) > 0 {
		v.storeMX(domain, true, now)

		return true, true
	}

	if herr != nil {
		if dnsErr, ok := errors.AsType[*net.DNSError](herr); ok && !dnsErr.IsNotFound {
			return false, false
		}
	}

	v.storeMX(domain, false, now)

	return false, true
}

// isNullMX reports whether the answer is RFC 7505's "this domain accepts
// no mail": exactly one record whose target is the root.
//
// Exactly one, because the RFC requires a null MX to be the only record
// - a set containing both it and a real host is a misconfiguration, and
// the deliverable reading of that is the real host.
//
// net.LookupMX renders the root target as ".", and a resolver that hands
// back an empty string for it is covered too.
func isNullMX(mx []*net.MX) bool {
	if len(mx) != 1 {
		return false
	}

	host := strings.TrimSpace(mx[0].Host)

	return host == "." || host == ""
}

func (v *Verifier) storeMX(domain string, ok bool, now time.Time) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.mxAnswer[domain] = cachedMX{ok: ok, at: now}
	if len(v.mxAnswer) > maxCacheEntries {
		v.evictLocked(now)
	}
}

// splitAddress validates the shape and returns the local and domain
// parts. Deliberately stricter than net/mail.ParseAddress, which
// accepts display names and comments that are not usable here.
func splitAddress(addr string) (local, domain string, ok bool) {
	local, domain, found := strings.CutLast(addr, "@")
	if !found || local == "" || domain == "" {
		return "", "", false
	}

	if strings.ContainsAny(addr, " \t\r\n\"<>,;") {
		return "", "", false
	}

	// Split on the LAST "@", so a second one can only be in the local
	// part - which is only legal inside a quoted string, and those are
	// rejected above along with the quote character.
	if strings.Contains(local, "@") {
		return "", "", false
	}

	if len(local) > 64 || len(addr) > 254 {
		return "", "", false
	}

	// A domain needs a dot and must not have empty or malformed labels.
	if !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") ||
		strings.HasSuffix(domain, ".") || strings.Contains(domain, "..") ||
		strings.HasPrefix(domain, "-") || strings.HasSuffix(domain, "-") {
		return "", "", false
	}

	if strings.HasPrefix(local, ".") || strings.HasSuffix(local, ".") || strings.Contains(local, "..") {
		return "", "", false
	}

	return local, domain, true
}
