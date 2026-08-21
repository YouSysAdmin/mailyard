// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package settings

import (
	"strings"
	"testing"

	smodel "github.com/yousysadmin/mailyard/internal/models/setting"
)

// The from address lands VERBATIM in MAIL FROM, so a value that is not
// a bare address is a 501 protocol error from whichever server the
// pool points at - reported hours later as a delivery failure by a
// fire-and-forget sender whose caller already returned success. The
// write is where the mistake is one keystroke old and the message can
// still name the fix.
func TestPlatformMailFromMustBeABareAddress(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		ok          bool
	}{
		{"a bare address", "no-reply@example.com", true},
		{"empty turns platform mail off", "", true},
		{"surrounding whitespace is trimmed", "  no-reply@example.com ", true},

		// The direct 501 factories: shapes mail.ParseAddress refuses,
		// which EnvelopeAddress then hands WHOLE to MAIL FROM.
		{"a name without angle brackets", "Mailyard no-reply@example.com", false},
		{"an unclosed angle bracket", "Mailyard <no-reply@example.com", false},
		{"not an address at all", "example.com", false},
		{"two addresses", "a@example.com, b@example.com", false},

		// These PARSE - the envelope would survive them - and are
		// refused anyway: the display name belongs in from_name, and
		// accepting it here means the From header carries one name
		// while the setting page shows another.
		{"a display name", "Mailyard <no-reply@example.com>", false},
		{"a dotted display name", "mail.example.com <no-reply@example.com>", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Validate(smodel.KeyPlatformMailFrom, tc.value)
			if tc.ok && err != nil {
				t.Fatalf("Validate(%q) refused: %v", tc.value, err)
			}

			if !tc.ok {
				if err == nil {
					t.Fatalf("Validate(%q) accepted, want a refusal naming the from_name field", tc.value)
				}

				if !strings.Contains(err.Error(), "platform_mail_from_name") {
					t.Errorf("the refusal does not say where the display name goes: %v", err)
				}

				return
			}

			if got != strings.TrimSpace(tc.value) {
				t.Errorf("normalized to %q, want the trimmed input", got)
			}
		})
	}
}
