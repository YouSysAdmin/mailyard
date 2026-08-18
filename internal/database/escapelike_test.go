// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package database

import "testing"

// A placeholder stops SQL injection but not PATTERN injection: the
// driver hands the string to LIKE as data, and LIKE still reads % and _ inside it as wildcards.
// In a search box that is harmless. In the GDPR erase, which builds a pattern out of an address and DELETEs
// whatever it matches, it is the difference between erasing one
// recipient and erasing a whole domain - and "%@example.com" passes
// email validation, so the input is reachable.
func TestEscapeLikeNeutralizesWildcards(t *testing.T) {
	cases := map[string]string{
		"a@x.com":      `a@x.com`,
		"%@x.com":      `\%@x.com`,
		"a%b@x.com":    `a\%b@x.com`,
		"_@x.com":      `\_@x.com`,
		`a\%b@x.com`:   `a\\\%b@x.com`,
		"%_%":          `\%\_\%`,
		"nothing here": "nothing here",
	}
	for in, want := range cases {
		if got := EscapeLike(in); got != want {
			t.Errorf("EscapeLike(%q) = %q, want %q", in, got, want)
		}
	}
}

// The backslash must be escaped before the wildcards, or the pass that
// escapes % would produce a `\%` that the backslash pass then doubles
// into `\\%` - a literal backslash followed by a live wildcard, which
// is the bug the ordering exists to avoid.
func TestEscapeLikeOrdersBackslashFirst(t *testing.T) {
	if got := EscapeLike(`\`); got != `\\` {
		t.Fatalf("EscapeLike(backslash) = %q, want two backslashes", got)
	}

	// A value that is only a wildcard must come back fully inert.
	if got := EscapeLike("%"); got != `\%` {
		t.Errorf("EscapeLike(%%) = %q", got)
	}
}
