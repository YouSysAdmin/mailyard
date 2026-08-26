// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package mx answers "where does mail for this domain go", which is
// the question a relay node has to ask and the rest of Mailyard never
// did.
//
// Every other delivery path in this codebase hands a message to a
// CONFIGURED host - a tenant's postfix, SES, a row in the shared pool.
// A relay node has no such host: it is the last hop before the
// internet, so it resolves the recipient domain itself.
//
// The near-identical lookup in core/emailverify answers a different
// question - "could this address exist" - and deliberately caches a
// negative. Here a negative must not be cached: a domain whose DNS was
// briefly unreachable has to be reachable again on the retry, and a
// cached "no" would turn a blip into a bounce.
package mx

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

// Target is one host to try, in the order returned.
type Target struct {
	// Host is the name to connect to, without a trailing dot so it can
	// be used as a TLS ServerName as it stands.
	Host string

	// Pref is the MX preference, lowest first. Zero for an implicit
	// target, which has no preference to speak of.
	Pref uint16

	// Implicit reports that the domain published no MX and this is the
	// domain itself, per the RFC 5321 section 5.1 fallback. Worth
	// keeping because it changes what a connection failure MEANS: an
	// explicit MX that refuses is a broken mail setup, an implicit one
	// is usually a domain that never wanted mail.
	Implicit bool
}

// Error carries whether the failure is worth retrying. The node
// branches on Perm to decide bounce now or try later, so the
// distinction has to survive out of this package.
type Error struct {
	Domain string
	Perm   bool
	Err    error
}

// Error renders the failure for a log or a caller.
func (e *Error) Error() string { return fmt.Sprintf("mx %s: %v", e.Domain, e.Err) }

// Unwrap returns the underlying error, for errors.Is and errors.As.
func (e *Error) Unwrap() error { return e.Err }

// Permanent matches the shape of smtpclient.SendError.Permanent so
// callers can treat both the same way.
func (e *Error) Permanent() bool { return e.Perm }

// Resolver is the DNS surface this package needs. An interface
// because every test and every live check points it somewhere other
// than the real internet, and that is not something you can retrofit.
type Resolver interface {
	LookupMX(ctx context.Context, domain string) ([]*net.MX, error)
	LookupHost(ctx context.Context, host string) ([]string, error)
}

type netResolver struct{ r *net.Resolver }

// LookupMX resolves the MX records for d. An interface so the delivery
// path can be tested without DNS.
func (n netResolver) LookupMX(ctx context.Context, d string) ([]*net.MX, error) {
	return n.r.LookupMX(ctx, d)
}

// LookupHost resolves h to addresses, the fallback when a domain
// publishes no MX.
func (n netResolver) LookupHost(ctx context.Context, h string) ([]string, error) {
	return n.r.LookupHost(ctx, h)
}

// Config tunes the lookup.
type Config struct {
	// Timeout bounds one resolution, both queries together.
	Timeout time.Duration

	// CacheTTL bounds how long a POSITIVE answer is reused. Failures
	// are never cached - see the package doc.
	CacheTTL time.Duration

	// Resolver overrides DNS. Nil uses the host resolver.
	Resolver Resolver
}

type cached struct {
	targets []Target
	at      time.Time
}

// Lookup resolves and caches mail exchangers.
type Lookup struct {
	cfg Config
	res Resolver

	mu  sync.RWMutex
	hit map[string]cached

	// now is a test seam. Nil means time.Now.
	now func() time.Time
}

// New builds a Lookup. Zero values in cfg take sane defaults so a
// caller that only wants a resolver override does not have to restate
// the timings.
func New(cfg Config) *Lookup {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}

	if cfg.CacheTTL <= 0 {
		cfg.CacheTTL = 5 * time.Minute
	}

	res := cfg.Resolver
	if res == nil {
		res = netResolver{r: net.DefaultResolver}
	}

	return &Lookup{cfg: cfg, res: res, hit: map[string]cached{}}
}

func (l *Lookup) clock() time.Time {
	if l.now != nil {
		return l.now()
	}

	return time.Now()
}

// Resolve returns the hosts to try for domain, best first.
//
// Equal-preference records are shuffled, which RFC 5321 asks for and
// which is the only load spreading a sender gets across a provider's
// front ends.
func (l *Lookup) Resolve(ctx context.Context, domain string) ([]Target, error) {
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if domain == "" {
		return nil, &Error{Domain: domain, Perm: true, Err: errors.New("empty domain")}
	}

	if t, ok := l.cachedTargets(domain); ok {
		return t, nil
	}

	ctx, cancel := context.WithTimeout(ctx, l.cfg.Timeout)
	defer cancel()

	targets, err := l.resolve(ctx, domain)
	if err != nil {
		return nil, err
	}

	l.store(domain, targets)

	return targets, nil
}

func (l *Lookup) cachedTargets(domain string) ([]Target, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	c, ok := l.hit[domain]
	if !ok || l.clock().Sub(c.at) > l.cfg.CacheTTL {
		return nil, false
	}

	// Copy: the caller may shuffle or truncate, and the cache entry is
	// shared with every other delivery to this domain.
	out := make([]Target, len(c.targets))
	copy(out, c.targets)

	return out, true
}

// store keeps its own copy. The slice handed back to the first caller
// is the same one every later caller would read from, so storing it
// directly lets one delivery's reordering rewrite the answer for the
// whole domain.
func (l *Lookup) store(domain string, targets []Target) {
	kept := make([]Target, len(targets))
	copy(kept, targets)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hit[domain] = cached{targets: kept, at: l.clock()}
}

func (l *Lookup) resolve(ctx context.Context, domain string) ([]Target, error) {
	records, err := l.res.LookupMX(ctx, domain)
	if err != nil {
		if dnsErr, ok := errors.AsType[*net.DNSError](err); !ok || !dnsErr.IsNotFound {
			// SERVFAIL, a timeout, a resolver that is down. Not an
			// answer at all, and treating it as one bounces somebody's
			// mail because of our network.
			return nil, &Error{Domain: domain, Err: err}
		}

		// IsNotFound covers two different facts and Go does not let us
		// tell them apart: the domain does not exist, and the domain
		// exists but publishes no MX. Concluding "gone" here would
		// permanently bounce every domain that accepts mail on its
		// A record, which is a great many of them. Only the address
		// lookup below can settle it.
		records = nil
	}

	if nullMX(records) {
		// RFC 7505. The domain has explicitly said it accepts no mail.
		// Without this check the single "." record becomes a hostname we
		// try to dial for three days.
		return nil, &Error{Domain: domain, Perm: true, Err: errors.New("domain accepts no mail (null MX)")}
	}

	if targets := usableTargets(records); len(targets) > 0 {
		return targets, nil
	}

	// No MX: the domain itself, if it resolves. RFC 5321 section 5.1.
	hosts, herr := l.res.LookupHost(ctx, domain)
	if herr != nil {
		if dnsErr, ok := errors.AsType[*net.DNSError](herr); ok && dnsErr.IsNotFound {
			return nil, &Error{Domain: domain, Perm: true, Err: errors.New("domain has no mail exchanger and no address")}
		}

		return nil, &Error{Domain: domain, Err: herr}
	}

	if len(hosts) == 0 {
		return nil, &Error{Domain: domain, Perm: true, Err: errors.New("domain has no mail exchanger and no address")}
	}

	return []Target{{Host: domain, Implicit: true}}, nil
}

// usableTargets drops junk and orders what is left.
func usableTargets(records []*net.MX) []Target {
	var out []Target
	for _, r := range records {
		host := strings.TrimSuffix(strings.TrimSpace(r.Host), ".")
		if host == "" || host == "." {
			continue
		}

		out = append(out, Target{Host: strings.ToLower(host), Pref: r.Pref})
	}

	if len(out) == 0 {
		return nil
	}

	// Shuffle first, then a STABLE sort by preference. Sorting a
	// shuffled slice stably keeps the shuffle within each preference
	// band while the bands themselves stay ordered - doing it the other
	// way round would undo one or the other.
	rand.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	sort.SliceStable(out, func(i, j int) bool { return out[i].Pref < out[j].Pref })

	return out
}

// nullMX reports the RFC 7505 "this domain sends and receives no mail"
// record: exactly one MX whose exchange is the root.
func nullMX(records []*net.MX) bool {
	if len(records) != 1 {
		return false
	}

	host := strings.TrimSpace(records[0].Host)

	return host == "." || host == ""
}
