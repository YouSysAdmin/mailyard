// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package proxylisten wraps an SMTP listener so the PROXY protocol
// decides who the client is.
//
// Only the SMTP listeners need this. Fiber already reads
// X-Forwarded-For for us, gated by server.trusted_proxies, but SMTP has
// nowhere in the protocol to carry the original address. Put a TCP
// balancer in front and every session looks like it came from the
// balancer, which breaks three things at once:
//
//   - The per-IP session limiters, which collapse into one bucket for
//     every sender, so submission.rate_per_minute becomes the limit for
//     the whole installation rather than per client.
//   - SPF on the MX, computed from the connecting IP, and so computed
//     for a balancer no sender's SPF record names.
//   - Every log line and stored client_ip, naming a hop of ours.
//
// Trust is the whole design here. A PROXY header is just a claim about
// who is calling, with nothing to authenticate it, so believing one from
// a stranger is worse than never reading it at all - anyone who can
// reach the port could forge a source address, and with it an SPF pass
// and somebody else's rate budget. We only read the header from peers on
// the trusted list, and that list may not be empty.
package proxylisten

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/pires/go-proxyproto"
)

// headerTimeout bounds how long one connection may take to send its
// header.
//
// The header is the first thing on the wire and a proxy writes it
// immediately, so this is only a slowloris bound: without it a peer
// that connects and says nothing holds a goroutine and a file
// descriptor forever. Short, because a legitimate proxy is never slow
// here.
const headerTimeout = 5 * time.Second

// Wrap returns a listener that reads a PROXY header from the peers in
// trusted, and behaves exactly like ln for everyone else.
//
// An empty list is an error rather than "trust everybody", which is
// what the library's own default would give: DefaultPolicy is REQUIRE,
// and REQUIRE alone honors a header from any peer. That default is
// right for a listener only a proxy can reach and wrong for every
// listener here, so it is never used.
func Wrap(ln net.Listener, trusted []string) (net.Listener, error) {
	nets, err := ParseTrusted(trusted)
	if err != nil {
		return nil, err
	}

	if len(nets) == 0 {
		return nil, errors.New("proxy protocol enabled with no trusted proxies: a PROXY header is an " +
			"unauthenticated claim about who is calling, so reading one from any peer would let a stranger " +
			"forge a source address")
	}

	return &proxyproto.Listener{
		Listener:          ln,
		ConnPolicy:        policy(nets),
		ReadHeaderTimeout: headerTimeout,
	}, nil
}

// ParseTrusted turns the configured entries into prefixes.
//
// A bare address is accepted as well as a CIDR, because naming one
// balancer is the common case and writing /32 for it is the kind of
// detail an operator gets wrong once and then debugs for an hour.
//
// Exported so a caller can reject a malformed list before it binds
// anything.
func ParseTrusted(entries []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(entries))
	for _, raw := range entries {
		if p, err := netip.ParsePrefix(raw); err == nil {
			out = append(out, p)

			continue
		}

		addr, err := netip.ParseAddr(raw)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q is neither an address nor a CIDR", raw)
		}

		out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
	}

	return out, nil
}

// policy answers, per connection, what to do with a PROXY header.
//
// Two outcomes, and neither is the library's USE:
//
//	trusted peer   -> REQUIRE. The header must be there. A proxy always
//	                  sends one, so its absence means the deployment is
//	                  not what the config says - and the failure of
//	                  guessing is silent: the address becomes the
//	                  PROXY's, which is the exact bug this package
//	                  exists to fix, now with a config key claiming it
//	                  is fixed. Refusing the connection is loud.
//	everyone else  -> SKIP. Not IGNORE: IGNORE reads the header and
//	                  discards it, so a stranger writing `PROXY ...`
//	                  gets a quiet accept. SKIP never looks, so those
//	                  bytes reach the SMTP parser and earn a 500, which
//	                  is what an unknown command deserves.
func policy(trusted []netip.Prefix) proxyproto.ConnPolicyFunc {
	return func(opts proxyproto.ConnPolicyOptions) (proxyproto.Policy, error) {
		if trustedPeer(trusted, opts.Upstream) {
			return proxyproto.REQUIRE, nil
		}

		return proxyproto.SKIP, nil
	}
}

// trustedPeer reports whether an accepted connection came from a
// configured proxy.
//
// The 4-in-6 unmap is load-bearing rather than tidiness: a dual-stack
// listener reports an IPv4 peer as ::ffff:10.0.0.1, and that does not
// match a 10.0.0.0/8 prefix. Without it the trusted list silently
// matches nothing on exactly the deployment this is written for, and
// the symptom is every session still showing the balancer's address.
func trustedPeer(trusted []netip.Prefix, upstream net.Addr) bool {
	var addr netip.Addr
	switch a := upstream.(type) {
	case *net.TCPAddr:
		var ok bool
		if addr, ok = netip.AddrFromSlice(a.IP); !ok {
			return false
		}
	default:
		host, _, err := net.SplitHostPort(upstream.String())
		if err != nil {
			return false
		}

		if addr, err = netip.ParseAddr(host); err != nil {
			return false
		}
	}

	addr = addr.Unmap()

	for _, p := range trusted {
		if p.Contains(addr) {
			return true
		}
	}

	return false
}
