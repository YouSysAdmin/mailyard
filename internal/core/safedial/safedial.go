// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package safedial builds HTTP clients that refuse to connect to
// addresses inside the deployment's own network.
//
// The problem it solves is server-side request forgery: outgoing
// webhook URLs are supplied by project members, and without a guard
// a member can aim one at the cloud metadata service
// (169.254.169.254), at a sibling container, or at this process's own
// admin API and use the platform as a proxy into the private network.
//
// The check runs in the dialer's Control hook, which fires after name
// resolution with the address the kernel is about to connect to. That
// placement is the whole point: validating the URL's hostname at
// creation time is defeated by a name that resolves to a public
// address once and a private one a second later (DNS rebinding), and
// by any redirect. Every hop pays the check because every hop dials.
package safedial

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// blocked lists the ranges an outbound call may never reach. The
// netip.Addr predicates cover loopback, link-local, multicast and
// RFC 1918, so this carries only what they miss.
var blocked = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // "this host on this network"
	netip.MustParsePrefix("100.64.0.0/10"),   // carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // documentation
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
	netip.MustParsePrefix("198.51.100.0/24"), // documentation
	netip.MustParsePrefix("203.0.113.0/24"),  // documentation
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved, includes broadcast
	netip.MustParsePrefix("::/128"),          // unspecified
	netip.MustParsePrefix("2001:db8::/32"),   // documentation
	netip.MustParsePrefix("fc00::/7"),        // unique local
}

// ErrBlocked is returned when a dial targets a disallowed address. It
// is deliberately vague about which rule matched: the caller is the
// person who chose the URL, and enumerating the internal network back
// to them defeats the purpose.
type ErrBlocked struct {
	Addr string
}

// Error renders the failure for a log or a caller.
func (e *ErrBlocked) Error() string {
	return fmt.Sprintf("connection to %s refused: the address is inside a private or reserved range", e.Addr)
}

// AddrAllowed reports whether addr may be dialled. Exported so
// handlers can give an operator a useful error at configuration time
// rather than letting the first delivery fail silently - but the
// dialer, not this, is the enforcement point.
func AddrAllowed(addr netip.Addr) bool {
	// An IPv4 address delivered as ::ffff:a.b.c.d passes every IPv6
	// predicate while being an ordinary IPv4 destination. Unmap first
	// so one representation cannot walk past the rules written for
	// the other.
	a := addr.Unmap()
	switch {
	case !a.IsValid(),
		a.IsLoopback(),
		a.IsUnspecified(),
		a.IsPrivate(),
		a.IsLinkLocalUnicast(),
		a.IsLinkLocalMulticast(),
		a.IsInterfaceLocalMulticast(),
		a.IsMulticast():
		return false
	}

	for _, p := range blocked {
		// A prefix only ever matches its own family, so the mismatched
		// half of the list costs one comparison each.
		if p.Addr().Is4() == a.Is4() && p.Contains(a) {
			return false
		}
	}

	return true
}

// HostAllowed resolves host and reports whether every address it
// answers with is dialable. Used for the friendly up-front error.
// A name that does not resolve is reported as allowed: refusing it
// here would turn a transient DNS failure into a validation error,
// and the dialer will refuse it anyway if it ever resolves badly.
func HostAllowed(ctx context.Context, host string) bool {
	if addr, err := netip.ParseAddr(host); err == nil {
		return AddrAllowed(addr)
	}

	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return true
	}

	for _, a := range addrs {
		if !AddrAllowed(a) {
			return false
		}
	}

	return true
}

// Dialer returns a net.Dialer that refuses private destinations, for
// a protocol that is not HTTP - the SMTP client dials with it. The same
// guard as Client, in the same place: the Control hook, after the name
// has resolved. allowPrivate returns a plain bounded dialer.
func Dialer(timeout time.Duration, allowPrivate bool) *net.Dialer {
	d := &net.Dialer{Timeout: timeout}
	if !allowPrivate {
		d.Control = Control
	}

	return d
}

// Control is the dialer hook that enforces AddrAllowed. Exported so a
// caller building a dialer of its own can attach exactly this.
func Control(_, address string, _ syscall.RawConn) error {
	// address is host:port with the host already resolved to a
	// literal, which is why this check cannot be rebound.
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return &ErrBlocked{Addr: address}
	}

	ip, err := netip.ParseAddr(host)
	if err != nil || !AddrAllowed(ip) {
		return &ErrBlocked{Addr: host}
	}

	return nil
}

// Client returns an http.Client that refuses private destinations and
// does not follow redirects.
//
// Redirects are refused rather than re-checked because a webhook
// endpoint has no legitimate reason to bounce us elsewhere, and
// following one turns a single reviewed URL into an open-ended chain.
// The dial guard would catch a private hop regardless - this just
// keeps the delivery log honest about where the request went.
//
// allowPrivate disables the address guard entirely, for operators
// whose receivers genuinely live on the same private network.
func Client(timeout time.Duration, allowPrivate bool) *http.Client {
	c := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	if allowPrivate {
		return c
	}

	d := Dialer(timeout, false)
	d.KeepAlive = 30 * time.Second
	c.Transport = &http.Transport{
		DialContext:           d.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return c
}
