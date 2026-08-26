// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package safetext

import "testing"

// A masked address keeps the domain and the first letter, nothing else.
func TestMaskAddress(t *testing.T) {
	for in, want := range map[string]string{
		"jane.doe@example.com": "j***@example.com",
		"  Ann@Example.org ":   "A***@Example.org",
		"ж@пошта.укр":          "ж***@пошта.укр",
		"@example.com":         "***",
		"not-an-address":       "***",
		"":                     "***",
	} {
		if got := MaskAddress(in); got != want {
			t.Errorf("MaskAddress(%q) = %q, want %q", in, got, want)
		}
	}
}
