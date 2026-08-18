// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package validation

import "testing"

func TestFriendlyField(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		// Override map cases: tricky abbreviations + non-obvious names
		// users see by their label, not their json tag.
		{"id", "ID"},
		// Auto-titlecase fallback for plain snake_case fields.
		{"name", "Name"},
		{"priority", "Priority"},
		{"some_new_field", "Some new field"},
		// Empty / malformed inputs degrade gracefully.
		{"", "This field"},
	}
	for _, c := range cases {
		got := friendlyField(c.in)
		if got != c.want {
			t.Errorf("friendlyField(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
