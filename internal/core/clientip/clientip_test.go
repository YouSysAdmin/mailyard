// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package clientip

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// TestTheCallerIsTheFirstAddressWeDidNotWrite walks the cases an operator
// actually meets, including the two that made Fiber's own reader unusable
// here: a chain the caller prepended to, and a caller naming an address it
// would like to be treated as.
func TestTheCallerIsTheFirstAddressWeDidNotWrite(t *testing.T) {
	for _, tc := range []struct {
		name      string
		trusted   []string
		forwarded string
		want      string
	}{{
		name: "no proxy configured, the header is not evidence",
		// 0.0.0.0 is what fasthttp reports as the peer under app.Test.
		trusted:   nil,
		forwarded: "203.0.113.9",
		want:      "0.0.0.0",
	}, {
		name:      "the peer is not a trusted proxy",
		trusted:   []string{"10.0.0.0/8"},
		forwarded: "203.0.113.9",
		want:      "0.0.0.0",
	}, {
		name:      "one hop, the proxy named the caller",
		trusted:   []string{"0.0.0.0/32"},
		forwarded: "203.0.113.9",
		want:      "203.0.113.9",
	}, {
		name: "the caller sent its own header and the proxy appended",
		// This is the spoof: leftmost is what the caller wrote.
		trusted:   []string{"0.0.0.0/32", "10.0.0.0/8"},
		forwarded: "8.8.8.8, 203.0.113.9, 10.0.0.7",
		want:      "203.0.113.9",
	}, {
		name:      "two of our own hops are skipped",
		trusted:   []string{"0.0.0.0/32", "10.0.0.0/8", "192.168.0.0/16"},
		forwarded: "203.0.113.9, 192.168.1.5, 10.0.0.7",
		want:      "203.0.113.9",
	}, {
		name:      "a hop wrote a port",
		trusted:   []string{"0.0.0.0/32", "10.0.0.0/8"},
		forwarded: "203.0.113.9:41234, 10.0.0.7",
		want:      "203.0.113.9",
	}, {
		name:      "4-in-6, so the answer is one host and not two",
		trusted:   []string{"0.0.0.0/32", "10.0.0.0/8"},
		forwarded: "::ffff:203.0.113.9, 10.0.0.7",
		want:      "203.0.113.9",
	}, {
		name:      "an ipv6 caller",
		trusted:   []string{"0.0.0.0/32"},
		forwarded: "2001:db8::1",
		want:      "2001:db8::1",
	}, {
		name:      "a bracketed ipv6 with a port",
		trusted:   []string{"0.0.0.0/32"},
		forwarded: "[2001:db8::1]:443",
		want:      "2001:db8::1",
	}, {
		name: "an unreadable entry breaks the chain",
		// Everything left of "unknown" was written by something we cannot
		// account for, so the answer degrades to who connected.
		trusted:   []string{"0.0.0.0/32", "10.0.0.0/8"},
		forwarded: "203.0.113.9, unknown, 10.0.0.7",
		want:      "0.0.0.0",
	}, {
		name:      "every entry is one of ours",
		trusted:   []string{"0.0.0.0/32", "10.0.0.0/8"},
		forwarded: "10.0.0.9, 10.0.0.7",
		want:      "0.0.0.0",
	}, {
		name:      "no header at all",
		trusted:   []string{"0.0.0.0/32"},
		forwarded: "",
		want:      "0.0.0.0",
	}, {
		name:      "a bare address entry, not a range",
		trusted:   []string{"0.0.0.0"},
		forwarded: "203.0.113.9",
		want:      "203.0.113.9",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := New(tc.trusted)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			var got string
			app := fiber.New()
			app.Get("/", func(c fiber.Ctx) error {
				got = r.Stamp(c)
				if from := From(c); from != got {
					t.Errorf("From answered %q where Stamp answered %q", from, got)
				}

				return c.SendString("ok")
			})

			req := httptest.NewRequest(fiber.MethodGet, "/", nil)
			if tc.forwarded != "" {
				req.Header.Set(fiber.HeaderXForwardedFor, tc.forwarded)
			}

			if _, err := app.Test(req); err != nil {
				t.Fatalf("Test: %v", err)
			}

			if got != tc.want {
				t.Errorf("caller is %q, want %q (xff %q, trusted %v)", got, tc.want, tc.forwarded, tc.trusted)
			}
		})
	}
}

// TestAnUnstampedRequestFallsBackToThePeer pins the fallback, which is
// what a package standing up its own app in a test gets.
func TestAnUnstampedRequestFallsBackToThePeer(t *testing.T) {
	var got string
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		got = From(c)

		return c.SendString("ok")
	})

	req := httptest.NewRequest(fiber.MethodGet, "/", nil)
	req.Header.Set(fiber.HeaderXForwardedFor, "8.8.8.8")
	if _, err := app.Test(req); err != nil {
		t.Fatalf("Test: %v", err)
	}

	if got != "0.0.0.0" {
		t.Errorf("unstamped request answered %q, want the peer address", got)
	}
}

// TestTheAnswerOutlivesTheRequest is the property the audit trail rests
// on: the string is not a view of the request buffer.
//
// Three requests over one app, each keeping what it was told, and the
// buffers are pooled and reused between them - which is what made the
// header-backed version report a later request's address for an earlier
// event.
func TestTheAnswerOutlivesTheRequest(t *testing.T) {
	r, err := New([]string{"0.0.0.0/32"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var kept []string
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		kept = append(kept, r.Stamp(c))

		return c.SendString("ok")
	})

	want := []string{"203.0.113.9", "198.51.100.44", "192.0.2.1"}
	for _, ip := range want {
		req := httptest.NewRequest(fiber.MethodGet, "/", nil)
		req.Header.Set(fiber.HeaderXForwardedFor, ip)
		if _, err := app.Test(req); err != nil {
			t.Fatalf("Test: %v", err)
		}
	}

	for i, ip := range want {
		if kept[i] != ip {
			t.Errorf("request %d kept %q, want %q - the string is a view of a reused buffer", i, kept[i], ip)
		}
	}
}

// TestAnUnparsableTrustEntryIsAnError - an entry that matches nothing is
// a trust list that is not what the operator wrote.
func TestAnUnparsableTrustEntryIsAnError(t *testing.T) {
	if _, err := New([]string{"10.0.0.0/8", "not-an-address"}); err == nil {
		t.Fatal("an unparsable trusted_proxies entry was accepted")
	}
}
