// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package env

import "testing"

// A list written the way a person writes one.
//
// Viper splits an environment variable on commas and does not trim what
// it split, so "a, b" arrives as ["a", " b"]. The cost is different at
// every consumer and none of it is obvious: server.trusted_proxies
// fails the BOOT, because netip.ParsePrefix refuses " 10.0.0.0/8" while
// the value in the file looks perfectly correct. A CORS origin quietly
// stops matching. An allowed domain matches nothing, and the send
// resolves no candidates at all.
func TestASpaceAfterACommaIsNotPartOfTheValue(t *testing.T) {
	t.Setenv("MAILYARD_SERVER_TRUSTED_PROXIES", "10.0.0.0/8, 172.16.0.0/12,192.168.0.0/16")
	t.Setenv("MAILYARD_CORS_ALLOWED_ORIGINS", "https://a.test, https://b.test")
	t.Setenv("MAILYARD_DATABASE_REPLICA_DSNS", "postgres://one, postgres://two")

	c, err := Load("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	for _, tc := range []struct {
		name string
		got  []string
		want []string
	}{
		{"server.trusted_proxies", c.Server.TrustedProxies,
			[]string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}},
		{"cors.allowed_origins", c.CORS.AllowedOrigins,
			[]string{"https://a.test", "https://b.test"}},
		{"database.replica_dsns", c.Database.ReplicaDSNs,
			[]string{"postgres://one", "postgres://two"}},
	} {
		if len(tc.got) != len(tc.want) {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)

			continue
		}

		for i := range tc.want {
			if tc.got[i] != tc.want[i] {
				t.Errorf("%s[%d] = %q, want %q", tc.name, i, tc.got[i], tc.want[i])
			}
		}
	}
}

// The nested case, because the walk has to reach it: a listener's
// proxy_protocol.trusted sits two structs down, and an empty one there
// is a BOOT FAILURE by design - so an entry that fails to parse cannot
// be told apart from the mistake that check exists to catch.
func TestTheTrimReachesANestedList(t *testing.T) {
	t.Setenv("MAILYARD_SUBMISSION_PROXY_PROTOCOL_TRUSTED", "10.0.0.1, 10.0.0.2")

	c, err := Load("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	got := c.Submission.ProxyProtocol.Trusted
	if len(got) != 2 || got[0] != "10.0.0.1" || got[1] != "10.0.0.2" {
		t.Errorf("submission.proxy_protocol.trusted = %q", got)
	}
}
