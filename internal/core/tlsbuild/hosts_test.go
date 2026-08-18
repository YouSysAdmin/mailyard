// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tlsbuild

import (
	"slices"
	"testing"
)

// The name AND the wildcard, because they cover different things:
// *.mail.example.com does not match mail.example.com, so a
// certificate carrying only the wildcard fails on the very name the
// operator configured.
func TestSelfSignedHostsCarryTheNameAndAWildcard(t *testing.T) {
	for name, tc := range map[string]struct {
		fqdn string
		want []string
	}{
		"ordinary host": {
			"mail.example.com",
			[]string{"mail.example.com", "*.mail.example.com", "localhost"},
		},
		"apex": {
			"example.com",
			[]string{"example.com", "*.example.com", "localhost"},
		},
		// A single label has nothing under it worth naming.
		"single label": {"mailyard", []string{"mailyard", "localhost"}},
		// A wildcard on an address is not a name any client matches.
		"ipv4":  {"127.0.0.1", []string{"127.0.0.1", "localhost"}},
		"ipv6":  {"::1", []string{"::1", "localhost"}},
		"empty": {"", []string{"localhost"}},
		// A scratch instance whose public_url IS localhost. Appending it
		// again put "DNS:localhost, DNS:localhost" in every generated
		// pair - harmless, and it reads as a bug to anybody running
		// openssl on it. Found by doing exactly that.
		"localhost":       {"localhost", []string{"localhost"}},
		"localhost cased": {"LocalHost", []string{"localhost"}},
	} {
		got := selfSignedHosts(tc.fqdn)
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: selfSignedHosts(%q) = %v, want %v", name, tc.fqdn, got, tc.want)
		}
	}
}
