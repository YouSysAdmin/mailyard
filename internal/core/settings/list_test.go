// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package settings_test

import (
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/settings"
	smodel "github.com/yousysadmin/mailyard/internal/models/setting"
)

// A list setting stores the JSON array every reader parses, and REFUSES
// anything else.
//
// The refusal is the point. StringList answers nil on a decode error, so
// before this a malformed value stored happily and read back as "nothing
// configured" - which for the ACME host list means the certificate stops
// being offered for those names, with the settings page showing the value
// the operator typed and nothing anywhere disagreeing.
func TestAListSettingMustBeAJSONArray(t *testing.T) {
	const key = smodel.KeyACMEHosts
	canonical := `["mail.example.com","mx.example.com"]`

	ok := []struct {
		name string
		in   string
		want string
	}{
		{"the array the console and every script send", canonical, canonical},
		{"blank entries and stray spaces are dropped",
			`[" mail.example.com ", "", "mx.example.com"]`, canonical},
		{"an empty array clears the setting", `[]`, ""},
		{"so does an empty value", "   ", ""},
	}
	for _, tc := range ok {
		got, err := settings.Validate(key, tc.in)
		if err != nil {
			t.Errorf("%s: Validate(%q) errored: %v", tc.name, tc.in, err)
			continue
		}

		if got != tc.want {
			t.Errorf("%s: Validate(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}

	// One value with no punctuation is the shape somebody reaches for
	// with curl, and it is exactly the shape that used to store fine and
	// mean nothing.
	for _, bad := range []string{"mail.example.com", `["mail.example.com"`, `{"a":1}`, `[1,2]`} {
		if got, err := settings.Validate(key, bad); err == nil {
			t.Errorf("Validate(%q) was accepted as %q - a value that is not an array "+
				"reads back as an empty list, so it has to be refused at the write",
				bad, got)
		}
	}
}

// The type has to be declared, or the console renders a one-line box and
// the operator is back to hand-writing JSON.
func TestTheHostListIsDeclaredAsAList(t *testing.T) {
	d, ok := smodel.Lookup(smodel.KeyACMEHosts)
	if !ok {
		t.Fatalf("%s is not in the registry", smodel.KeyACMEHosts)
	}

	if d.Type != smodel.TypeList {
		t.Errorf("%s is type %q, want %q - the console picks its control from this, "+
			"so a string here is a 110px input for a list of hostnames",
			smodel.KeyACMEHosts, d.Type, smodel.TypeList)
	}
}
