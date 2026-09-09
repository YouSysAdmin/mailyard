// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package database

import "testing"

// "None" has to be spelled the way the column's own default spells it.
//
// A plain encoder writes `null` for a nil slice and a nil map, and every
// column MustJSON feeds is NOT NULL DEFAULT '[]' or '{}'. A message with
// no attachments would store the string `null` and nothing would
// complain: `null` reads back as a nil slice, so the round trip is clean
// and the wire response identical.
//
// The predicates written against the sentinel are what break. The
// retention sweeps ask for an attachments_json that is neither the empty
// array nor the empty string, and `null` satisfies both - so the content
// pass would rewrite every settled row in its window, attachment or not,
// on the biggest table in the installation.
func TestNoneIsSpelledTheWayTheColumnDefaultsIt(t *testing.T) {
	var (
		nilSlice   []string
		nilMap     map[string]string
		nilStructs []struct{ A int }
	)
	for _, tc := range []struct {
		what string
		in   any
		want string
	}{
		{"a nil string slice", nilSlice, `[]`},
		{"a nil slice of structs", nilStructs, `[]`},
		{"a nil map", nilMap, `{}`},
		{"an empty slice", []string{}, `[]`},
		{"an empty map", map[string]string{}, `{}`},

		// Present values are untouched, which is the half that says the
		// normalization is not just returning a constant.
		{"one element", []string{"a"}, `["a"]`},
		{"one key", map[string]string{"k": "v"}, `{"k":"v"}`},

		// A nil POINTER is absent rather than empty, and `null` is the
		// honest answer for it. Nothing stores one through here today,
		// and the distinction is why this is not a blanket rule.
		{"a nil pointer", (*struct{ A int })(nil), `null`},
		{"an untyped nil", nil, `null`},
	} {
		if got := string(MustJSON(tc.in)); got != tc.want {
			t.Errorf("%s: MustJSON = %s, want %s", tc.what, got, tc.want)
		}
	}
}
