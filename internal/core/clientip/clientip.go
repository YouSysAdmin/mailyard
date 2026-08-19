// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package clientip answers one question - which address is the CALLER's -
// and is the only place in the tree that answers it.
//
// WHO CONNECTED IS A SEPARATE QUESTION FROM WHO IS CALLING, which is the
// same split the SMTP listeners make with the PROXY protocol
// (internal/core/proxylisten). On HTTP the second question is answered by
// X-Forwarded-For, and that header is an unauthenticated claim: a client
// sends whatever it likes and each hop APPENDS the peer it saw, so the
// LEFTMOST entry is the one nobody verified.
//
// Fiber's own c.IP() reads it leftmost-first, and that is why nothing here
// uses it for anything but the peer address:
//
//   - With EnableIPValidation off - the default - it returns the WHOLE
//     header. Behind two hops that is "203.0.113.9, 10.0.0.7", which
//     net.ParseIP refuses, so every api key carrying an ip allowlist was
//     refused and every per-ip rate bucket got a key the caller chose.
//   - With it on, it returns the first VALID entry, which a caller can set
//     to an allowlisted address by sending the header itself. That turns a
//     broken allowlist into a bypassed one.
//
// So the walk here goes RIGHT TO LEFT and stops at the first address that
// is not one of our own proxies: everything to the right of it was written
// by a hop we trust, and everything to the left of it is unverifiable.
package clientip

import (
	"net/netip"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/proxylisten"
)

// localsKey is the request-local the middleware stamps. Unexported type,
// so nothing outside can write the answer.
type localsKey struct{}

// Resolver decides the caller's address for a request, against the
// operator's trusted-proxy list. Build one at boot with New.
type Resolver struct {
	trusted []netip.Prefix
}

// New parses the trusted-proxy list into a Resolver. Entries are plain
// addresses or CIDR ranges, and an unparsable entry is an error rather
// than an entry that silently matches nothing.
//
// An EMPTY list is valid and means the deployment has no proxy in front:
// the peer address is the caller and X-Forwarded-For is never read.
// Parsing is shared with the SMTP side (proxylisten.ParseTrusted) so the
// two trust lists cannot come to disagree about what an entry means.
func New(entries []string) (*Resolver, error) {
	trusted, err := proxylisten.ParseTrusted(entries)
	if err != nil {
		return nil, err
	}

	return &Resolver{trusted: trusted}, nil
}

// Stamp resolves the caller's address and stores it on the request, so
// every later reader gets the same answer without re-parsing a header.
//
// Called once, from the requestContext middleware.
func (r *Resolver) Stamp(c fiber.Ctx) string {
	ip := r.Resolve(c)
	c.Locals(localsKey{}, ip)

	return ip
}

// Resolve answers the caller's address for this request.
//
// The peer address wins outright unless the peer is a trusted proxy: a
// stranger's X-Forwarded-For is not evidence of anything, and reading it
// would let anyone who can reach the port pick their own identity.
//
// A chain that cannot be read to the end degrades to the peer rather than
// guessing. An entry that is neither an address nor an address:port breaks
// the chain, and every entry to the LEFT of it is then unverifiable - so
// the answer becomes the last thing we know for certain, which is who
// connected.
func (r *Resolver) Resolve(c fiber.Ctx) string {
	// c.IP() with no ProxyHeader configured is the TCP peer and nothing
	// else - see the package doc for why this is the only call to it in
	// the tree.
	peer := c.IP()
	if len(r.trusted) == 0 || !r.trustedText(peer) {
		return peer
	}

	forwarded := c.Get(fiber.HeaderXForwardedFor)
	for i := len(forwarded); i > 0; {
		start := strings.LastIndexByte(forwarded[:i], ',') + 1

		entry := strings.TrimSpace(forwarded[start:i])
		i = start - 1

		if entry == "" {
			continue
		}

		addr, ok := parse(entry)
		if !ok {
			return peer
		}

		if r.isTrusted(addr) {
			continue
		}

		// Canonical form, not the raw header text: this reaches an ip
		// allowlist, a rate-limit key and a TEXT column, and "::ffff:1.2.3.4"
		// and "1.2.3.4" naming one host in three of those and two in the
		// fourth is exactly the kind of difference nobody debugs.
		return addr.String()
	}

	return peer
}

// From reports the caller's address for a request the middleware has
// stamped, falling back to the peer address when nothing stamped it -
// which is a unit test standing an app up by hand, not a live route.
//
// THE RETURNED STRING IS ALWAYS FRESHLY ALLOCATED, so a caller may keep
// it past the end of the request. That is load-bearing for the audit
// trail, which queues an event and writes it on another goroutine: a
// header-backed string would be read after fasthttp reused the buffer.
// Both paths here allocate - netip.Addr.String() builds one, and
// fasthttp's RemoteIP().String() builds one.
func From(c fiber.Ctx) string {
	if ip, ok := c.Locals(localsKey{}).(string); ok {
		return ip
	}

	return c.IP()
}

// trustedText reports whether a textual address is one of our proxies.
func (r *Resolver) trustedText(ip string) bool {
	addr, ok := parse(ip)
	if !ok {
		return false
	}

	return r.isTrusted(addr)
}

// isTrusted matches an address against the list.
func (r *Resolver) isTrusted(addr netip.Addr) bool {
	for _, p := range r.trusted {
		if p.Contains(addr) {
			return true
		}
	}

	return false
}

// parse reads one X-Forwarded-For entry, which may or may not carry a
// port - some proxies append one, and a bracketed IPv6 literal is how
// they write it when they do.
//
// The result is UNMAPPED, which is load-bearing for the same reason it is
// on the SMTP side: a dual-stack listener reports an IPv4 peer as
// ::ffff:10.0.0.1, which matches no 10.0.0.0/8 prefix - so without it the
// trust list matches NOTHING on exactly the deployment this exists for,
// and the feature is on and doing nothing. Unmapping HERE rather than at
// the match keeps the address that was matched and the address that is
// returned the same one.
func parse(entry string) (netip.Addr, bool) {
	if addr, err := netip.ParseAddr(entry); err == nil {
		return addr.Unmap(), true
	}

	if ap, err := netip.ParseAddrPort(entry); err == nil {
		return ap.Addr().Unmap(), true
	}

	return netip.Addr{}, false
}
