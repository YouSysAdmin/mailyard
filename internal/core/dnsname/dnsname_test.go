// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package dnsname

import (
	"slices"
	"strings"
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

func TestValidHostNames(t *testing.T) {
	for name, want := range map[string]bool{
		"mail.example.com":               true,
		"mail.example.com.":              true,
		"localhost":                      true,
		"xn--bcher-kva.de":               true,
		"a-b.example":                    true,
		"":                               false,
		"*.example.com":                  false,
		"bad host!":                      false,
		"x..y":                           false,
		"-a.example":                     false,
		"a-.example":                     false,
		"exa_mple.com":                   false,
		"10.0.0.1":                       false,
		strings.Repeat("a", 64) + ".com": false,
	} {
		if got := Valid(name); got != want {
			t.Errorf("Valid(%q) = %v, want %v", name, got, want)
		}
	}
}
