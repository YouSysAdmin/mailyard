// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package dnsname

import (
	"slices"
	"testing"
)

func TestCoveringNames(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"mailer.managebac.com", []string{"mailer.managebac.com", "managebac.com"}},
		{"managebac.com", []string{"managebac.com"}},
		{"a.b.c.example.com", []string{"a.b.c.example.com", "b.c.example.com", "c.example.com", "example.com"}},
		// A single label is nobody's to verify, so it is never asked
		// about.
		{"com", nil},
		{"", nil},
		// Normalized the way a resolver would present it.
		{" Mailer.ManageBac.com. ", []string{"mailer.managebac.com", "managebac.com"}},
	}
	for _, tc := range cases {
		if got := Covering(tc.in); !slices.Equal(got, tc.want) {
			t.Errorf("Covering(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// The lookalike case, spelled out because getting it wrong is the
// classic version of this bug: strings.HasSuffix("evilmanagebac.com",
// "managebac.com") is true.
func TestCoveringNamesNeverCrossesALabelBoundary(t *testing.T) {
	for _, lookalike := range []string{"evilmanagebac.com", "notmanagebac.com", "managebac.com.evil.net"} {
		if slices.Contains(Covering(lookalike), "managebac.com") {
			t.Errorf("%q was treated as being under managebac.com", lookalike)
		}
	}
}
