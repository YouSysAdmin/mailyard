// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package proxylisten

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-smtp"
	"github.com/pires/go-proxyproto"
)

// The claimed client address, and a proxy address it is claimed
// through. Both from the documentation ranges, so neither can be
// confused with anything real.
const (
	claimedClient = "203.0.113.9"
	v1Header      = "PROXY TCP4 " + claimedClient + " 198.51.100.1 4321 25\r\n"
)

// A trusted proxy's header is believed.
//
// The whole point: without this the address every listener reads is the
// balancer's, which puts every sender on earth in one per-IP rate
// bucket and computes SPF against a hop of ours.
func TestATrustedProxyDecidesWhoTheClientIs(t *testing.T) {
	got := remoteSeenBy(t, []string{"127.0.0.1/32", "::1/128"}, v1Header)

	if got != claimedClient {
		t.Errorf("listener saw %q, want %q - the PROXY header from a trusted peer was not used",
			got, claimedClient)
	}
}

// A STRANGER'S header is not believed, and this is the one that matters.
//
// A PROXY header is an unauthenticated claim about who is calling. If
// it were honored from any peer, anyone who can reach the port could
// assert an address and get two things for free: a forged SPF pass as
// whoever they named, and somebody else's rate budget drained while
// their own stays untouched.
//
// The trusted list here deliberately does not contain the loopback the
// test connects from, so the connection is exactly an untrusted one.
// SKIP means the bytes are never read as a header - they arrive as
// ordinary session data and earn a 500 from the SMTP parser, which is
// what an unknown command deserves.
func TestAStrangersHeaderIsNotBelieved(t *testing.T) {
	got := remoteSeenBy(t, []string{"10.0.0.0/8"}, v1Header)

	if got == claimedClient {
		t.Fatalf("listener believed %q from an untrusted peer - a stranger can forge a source "+
			"address, pass SPF as anybody and spend another sender's rate budget", claimedClient)
	}

	if !strings.HasPrefix(got, "127.0.0.1") && !strings.HasPrefix(got, "::1") {
		t.Errorf("listener saw %q, want the real loopback peer", got)
	}
}

// A trusted peer that sends no header is refused rather than guessed at.
//
// REQUIRE and not USE. A proxy always sends one, so its absence means
// the deployment is not what the config says - and guessing fails
// SILENTLY: the address becomes the proxy's, which is the exact bug
// this package exists to fix, now with a config key claiming it is
// fixed.
func TestATrustedPeerWithNoHeaderIsRefused(t *testing.T) {
	ln := wrapped(t, []string{"127.0.0.1/32", "::1/128"})

	accepted := make(chan error, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			accepted <- err

			return
		}

		defer func() { _ = c.Close() }()
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, 64)
		_, rerr := c.Read(buf)
		accepted <- rerr
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// Ordinary SMTP, no PROXY header.
	_, _ = conn.Write([]byte("EHLO example.com\r\n"))
	defer func() { _ = conn.Close() }()

	err = <-accepted
	if err == nil {
		t.Fatal("a trusted peer sending no PROXY header was accepted - the client address would " +
			"silently be the balancer's, which is what this is meant to prevent")
	}

	if !errors.Is(err, proxyproto.ErrNoProxyProtocol) {
		t.Logf("refused with %v", err)
	}
}

// go-smtp has to hand the WRAPPED connection to the backend, or every
// word above is true of a listener nobody reads from.
//
// Backend.NewSession takes the address off c.Conn().RemoteAddr() in all
// three listeners, and that Conn() is go-smtp's field rather than
// anything we control. An upgrade that started wrapping it in a bufio
// or a net.Conn of its own would leave the rate limiter and SPF back on
// the proxy's address with nothing failing.
func TestTheSMTPBackendSeesTheClientAddress(t *testing.T) {
	ln := wrapped(t, []string{"127.0.0.1/32", "::1/128"})
	seen := make(chan string, 1)

	srv := smtp.NewServer(&capturingBackend{seen: seen})
	srv.Domain = "test"
	srv.ReadTimeout = 2 * time.Second
	srv.WriteTimeout = 2 * time.Second
	defer func() { _ = srv.Close() }()
	go func() { _ = srv.Serve(ln) }()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte(v1Header + "EHLO example.com\r\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case got := <-seen:
		if !strings.HasPrefix(got, claimedClient+":") {
			t.Errorf("the SMTP backend saw %q, want %s - go-smtp is no longer handing the wrapped "+
				"connection to the backend, so the rate limiter and SPF are back on the proxy's address",
				got, claimedClient)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no session was opened")
	}
}

// Enabling the protocol without naming a proxy is refused, not treated
// as "trust everybody".
func TestAnEmptyTrustedListIsRefused(t *testing.T) {
	ln := listen(t)
	if _, err := Wrap(ln, nil); err == nil {
		t.Fatal("Wrap accepted an empty trusted list, which would honor a header from any peer")
	}
}

func TestAMalformedTrustedEntryIsRefused(t *testing.T) {
	ln := listen(t)
	if _, err := Wrap(ln, []string{"10.0.0.0/8", "not-an-address"}); err == nil {
		t.Fatal("Wrap accepted a malformed trusted entry")
	}
}

// A bare address is accepted as well as a CIDR: naming one balancer is
// the common case, and writing /32 for it is the detail an operator
// gets wrong once and then debugs for an hour.
func TestABareAddressIsATrustedProxy(t *testing.T) {
	nets, err := ParseTrusted([]string{"192.0.2.7", "2001:db8::1"})
	if err != nil {
		t.Fatalf("ParseTrusted: %v", err)
	}

	if len(nets) != 2 {
		t.Fatalf("got %d prefixes, want 2", len(nets))
	}

	for _, tc := range []struct{ addr string }{{"192.0.2.7"}, {"2001:db8::1"}} {
		if !trustedPeer(nets, &net.TCPAddr{IP: net.ParseIP(tc.addr), Port: 9}) {
			t.Errorf("%s is not matched by its own bare-address entry", tc.addr)
		}
	}

	if trustedPeer(nets, &net.TCPAddr{IP: net.ParseIP("192.0.2.8"), Port: 9}) {
		t.Error("a bare address matched a neighbour, so it was not read as a single host")
	}
}

// A dual-stack listener reports an IPv4 peer as ::ffff:10.1.2.3, which
// does not match a 10.0.0.0/8 prefix.
//
// Without the unmap the trusted list matches nothing on exactly the
// deployment this is written for, and the symptom is every session
// still carrying the balancer's address - a feature that is on,
// configured correctly, and doing nothing.
func TestAnIPv4PeerInsideIPv6IsStillTrusted(t *testing.T) {
	nets, err := ParseTrusted([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("ParseTrusted: %v", err)
	}

	mapped := netip.AddrFrom16(netip.MustParseAddr("::ffff:10.1.2.3").As16())

	if !trustedPeer(nets, &net.TCPAddr{IP: mapped.AsSlice(), Port: 25}) {
		t.Error("a 4-in-6 peer did not match its IPv4 prefix")
	}
}

// remoteSeenBy returns the address a listener reports for a client that
// opened with the given bytes.
func remoteSeenBy(t *testing.T, trusted []string, opening string) string {
	t.Helper()
	ln := wrapped(t, trusted)

	type result struct {
		addr string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			done <- result{err: err}

			return
		}

		defer func() { _ = c.Close() }()
		// RemoteAddr is what triggers header processing, and it has to
		// be read before the address is asked for anywhere else - which
		// is exactly what the SMTP backends do in NewSession.
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		br := bufio.NewReader(c)
		_, _ = br.Peek(1)
		done <- result{addr: c.RemoteAddr().String()}
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	defer func() { _ = conn.Close() }()
	if _, err := io.WriteString(conn, opening+"EHLO example.com\r\n"); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("accept: %v", r.err)
		}

		host, _, err := net.SplitHostPort(r.addr)
		if err != nil {
			return r.addr
		}

		return host
	case <-time.After(5 * time.Second):
		t.Fatal("listener never reported an address")
	}

	return ""
}

func wrapped(t *testing.T, trusted []string) net.Listener {
	t.Helper()

	ln, err := Wrap(listen(t), trusted)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}

	t.Cleanup(func() { _ = ln.Close() })

	return ln
}

func listen(t *testing.T) net.Listener {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	return ln
}

// capturingBackend reports the address go-smtp gave the session, which
// is the address all three real backends key their rate limiter on.
type capturingBackend struct{ seen chan string }

func (b *capturingBackend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	addr := ""
	if c != nil && c.Conn() != nil {
		addr = c.Conn().RemoteAddr().String()
	}

	select {
	case b.seen <- addr:
	default:
	}

	return &discardSession{}, nil
}

type discardSession struct{}

func (discardSession) Mail(string, *smtp.MailOptions) error { return nil }
func (discardSession) Rcpt(string, *smtp.RcptOptions) error { return nil }
func (discardSession) Data(io.Reader) error                 { return nil }
func (discardSession) Reset()                               {}
func (discardSession) Logout() error                        { return nil }
