package safedial

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

func TestAddrAllowed(t *testing.T) {
	refused := []string{
		"127.0.0.1",        // loopback
		"::1",              // loopback v6
		"0.0.0.0",          // unspecified
		"169.254.169.254",  // cloud metadata, the one that matters most
		"10.1.2.3",         // rfc 1918
		"172.16.0.1",       // rfc 1918
		"192.168.1.1",      // rfc 1918
		"100.64.0.1",       // carrier-grade nat
		"198.18.0.1",       // benchmarking
		"224.0.0.1",        // multicast
		"255.255.255.255",  // broadcast
		"fe80::1",          // link local
		"fd00::1",          // unique local
		"::ffff:127.0.0.1", // ipv4 loopback wearing an ipv6 costume
		"::ffff:169.254.169.254",
	}
	for _, s := range refused {
		addr, err := netip.ParseAddr(s)
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}

		if AddrAllowed(addr) {
			t.Errorf("AddrAllowed(%s) = true, want false", s)
		}
	}

	allowed := []string{"1.1.1.1", "93.184.216.34", "2606:4700:4700::1111"}
	for _, s := range allowed {
		addr, err := netip.ParseAddr(s)
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}

		if !AddrAllowed(addr) {
			t.Errorf("AddrAllowed(%s) = false, want true", s)
		}
	}
}

// The guard has to stop a real connection, not just answer a
// predicate: httptest listens on loopback, which is exactly the class
// of address a hostile webhook URL would name.
func TestClientRefusesLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	blocked, err := Client(5*time.Second, false).Get(srv.URL)
	if err == nil {
		_ = blocked.Body.Close()
		t.Fatal("expected the guarded client to refuse a loopback target")
	}

	resp, err := Client(5*time.Second, true).Get(srv.URL)
	if err != nil {
		t.Fatalf("allowPrivate client should have connected: %v", err)
	}

	_ = resp.Body.Close()
}

// A webhook receiver must not be able to bounce us somewhere else.
func TestClientDoesNotFollowRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/landed", http.StatusFound)

			return
		}

		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv.Close()

	resp, err := Client(5*time.Second, true).Get(srv.URL + "/start")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d (the redirect itself, unfollowed)",
			resp.StatusCode, http.StatusFound)
	}
}

// Dialer is the guard for a protocol that is not HTTP - the SMTP client
// dials with it. The check has to fire before the connection exists, so
// no listener is needed here: a refused address is refused whether or
// not anything answers on it.
func TestDialerRefusesLoopback(t *testing.T) {
	var blocked *ErrBlocked
	_, err := Dialer(time.Second, false).Dial("tcp", "127.0.0.1:25")
	if !errors.As(err, &blocked) {
		t.Fatalf("guarded dial to loopback: got %v, want ErrBlocked", err)
	}

	// The escape hatch is a plain dialer: whatever happens, it is not
	// the guard refusing.
	_, err = Dialer(200*time.Millisecond, true).Dial("tcp", "127.0.0.1:1")
	if errors.As(err, &blocked) {
		t.Fatal("allowPrivate dialer still refused loopback")
	}
}
