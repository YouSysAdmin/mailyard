// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package safetext

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestClampAlwaysYieldsStorableText covers the two ways a bounded
// header can poison an INSERT: a multi-byte rune straddling the cap,
// and bytes that were never UTF-8 to begin with.
func TestClampAlwaysYieldsStorableText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"short ascii passes through", "Mozilla/5.0", 400, "Mozilla/5.0"},
		{"cut lands between runes", strings.Repeat("日", 5), 3, strings.Repeat("日", 3)},
		{"invalid bytes are dropped", "abc\xff\xfedef", 400, "abcdef"},
		{"invalid bytes near the cap", "ab\xffcdef", 3, "abc"},
		{"empty stays empty", "", 10, ""},
	}
	for _, tc := range cases {
		got := Clamp(tc.in, tc.max)
		if got != tc.want {
			t.Errorf("%s: Clamp(%q, %d) = %q, want %q", tc.name, tc.in, tc.max, got, tc.want)
		}

		if !utf8.ValidString(got) {
			t.Errorf("%s: result is not valid UTF-8", tc.name)
		}
	}
}

// A remote reply is kept to a fixed number of bytes and never split
// inside a character.
func TestRemoteTextIsCappedOnARuneBoundary(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"short text passes through", "535 bad credentials", 19},
		{"a megabyte is cut to the cap", strings.Repeat("a", 1<<20), RemoteTextMax},
		{"a straddling rune is left out", strings.Repeat("a", RemoteTextMax-1) + "日", RemoteTextMax - 1},
		{"invalid bytes are dropped", "535\xff\xfe no", 6},
	}
	for _, tc := range cases {
		got := RemoteText(tc.in)
		if len(got) != tc.want {
			t.Errorf("%s: %d bytes, want %d", tc.name, len(got), tc.want)
		}

		if !utf8.ValidString(got) {
			t.Errorf("%s: result is not valid UTF-8", tc.name)
		}
	}
}
