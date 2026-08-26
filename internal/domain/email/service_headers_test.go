// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import "testing"

// "Bcc " with a trailing space is not in protectedHeaders, and a
// lenient receiver folds the space away and honours it as Bcc - so the
// name is validated as RFC 5322 ftext before the reserved lookup.
func TestValidHeaderName(t *testing.T) {
	for name, want := range map[string]bool{
		"X-Custom":       true,
		"x-lower-9":      true,
		"":               false,
		"Bcc ":           false,
		" Bcc":           false,
		"bcc\t":          false,
		"X:Y":            false,
		"X\r\nBcc":       false,
		"Ünïcode":        false,
		"X-With-Space Y": false,
	} {
		if got := validHeaderName(name); got != want {
			t.Errorf("validHeaderName(%q) = %v, want %v", name, got, want)
		}
	}
}
