// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package oidc

import "testing"

// Who the allowlists admit. The lists arrive lowercased from the store,
// the claims arrive however the IdP wrote them.
func TestAdmitAppliesTheAllowlistsInOrder(t *testing.T) {
	cases := []struct {
		name   string
		cfg    Config
		claims Claims
		admit  bool
	}{
		{"no lists, the IdP is the gate", Config{}, Claims{Email: "a@x.test"}, true},
		{"unverified refused when required", Config{RequireEmailVerified: true},
			Claims{Email: "a@x.test"}, false},
		{"verified admitted when required", Config{RequireEmailVerified: true},
			Claims{Email: "a@x.test", EmailVerified: true}, true},

		{"listed address in any case", Config{AllowedEmails: []string{"ada@x.test"}},
			Claims{Email: " Ada@X.test "}, true},
		{"unlisted address", Config{AllowedEmails: []string{"ada@x.test"}},
			Claims{Email: "bob@x.test"}, false},
		{"address list with no email claim", Config{AllowedEmails: []string{"ada@x.test"}},
			Claims{}, false},
		{"a listed address skips the domain and group checks",
			Config{AllowedEmails: []string{"ada@other.test"}, AllowedDomains: []string{"x.test"},
				GroupsClaim: "groups", AllowedGroups: []string{"admins"}},
			Claims{Email: "ada@other.test"}, true},
		{"verification still applies to a listed address",
			Config{RequireEmailVerified: true, AllowedEmails: []string{"ada@x.test"}},
			Claims{Email: "ada@x.test"}, false},

		{"listed domain", Config{AllowedDomains: []string{"x.test"}}, Claims{Email: "a@X.TEST"}, true},
		{"a subdomain is not the domain", Config{AllowedDomains: []string{"x.test"}},
			Claims{Email: "a@sub.x.test"}, false},
		{"a lookalike suffix is not the domain", Config{AllowedDomains: []string{"x.test"}},
			Claims{Email: "a@evilx.test"}, false},
		{"domain list with no email claim", Config{AllowedDomains: []string{"x.test"}}, Claims{}, false},

		{"group in a list claim, any case",
			Config{GroupsClaim: "groups", AllowedGroups: []string{"admins"}},
			Claims{Raw: map[string]any{"groups": []any{"Users", "ADMINS"}}}, true},
		{"group as a single string",
			Config{GroupsClaim: "roles", AllowedGroups: []string{"admins"}},
			Claims{Raw: map[string]any{"roles": "admins"}}, true},
		{"no overlapping group",
			Config{GroupsClaim: "groups", AllowedGroups: []string{"admins"}},
			Claims{Raw: map[string]any{"groups": []string{"users"}}}, false},
		{"group claim missing",
			Config{GroupsClaim: "groups", AllowedGroups: []string{"admins"}},
			Claims{Raw: map[string]any{}}, false},
		{"group claim of an unusable shape",
			Config{GroupsClaim: "groups", AllowedGroups: []string{"admins"}},
			Claims{Raw: map[string]any{"groups": 7}}, false},
		{"domain and group both required",
			Config{AllowedDomains: []string{"x.test"}, GroupsClaim: "groups", AllowedGroups: []string{"admins"}},
			Claims{Email: "a@x.test", Raw: map[string]any{"groups": []any{"users"}}}, false},
	}
	for _, tc := range cases {
		p := &Provider{cfg: tc.cfg}
		err := p.Admit(&tc.claims)
		if (err == nil) != tc.admit {
			t.Errorf("%s: admitted %v (%v), want %v", tc.name, err == nil, err, tc.admit)
		}
	}
}
