// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package mailheader

import (
	"maps"
	"strings"
	"testing"
)

// "Bcc " with a trailing space is not reserved, and a lenient receiver
// folds the space away and honours it as Bcc - so the name is validated
// as RFC 5322 ftext before the reserved lookup.
func TestValidName(t *testing.T) {
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
		if got := ValidName(name); got != want {
			t.Errorf("ValidName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestReservedCoversTheSetAndThePrefix(t *testing.T) {
	for name, want := range map[string]bool{
		"From":                         true,
		"MESSAGE-ID":                   true,
		"reply-to":                     true,
		"X-Mailyard-Email-Id":          true,
		"x-mailyard-sandbox-retention": true,
		"X-Mailyard":                   false,
		"X-Ticket":                     false,
		"In-Reply-To":                  false,
	} {
		if got := Reserved(name); got != want {
			t.Errorf("Reserved(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestValidateNamesTheRefusal(t *testing.T) {
	cases := []struct {
		headers map[string]string
		kind    Kind
	}{
		{map[string]string{"X-Ok": "1"}, 0},
		{nil, 0},
		{map[string]string{"Bcc ": "x"}, KindInvalidName},
		{map[string]string{"Subject": "x"}, KindReserved},
		{map[string]string{"X-Mailyard-Anything": "x"}, KindReserved},
		{map[string]string{"X-Ok": "a\r\nBcc: b"}, KindInvalidValue},
		{map[string]string{"X-Ok": "a\x00b"}, KindInvalidValue},
		{map[string]string{"X-Ok": "a\x1b[31mb"}, KindInvalidValue},
		{map[string]string{"X-Ok": "tab\tis fine"}, 0},
		{map[string]string{"X-Ok": strings.Repeat("v", MaxValueLen)}, 0},
		{map[string]string{"X-Ok": strings.Repeat("v", MaxValueLen+1)}, KindTooLong},
		{map[string]string{"X-" + strings.Repeat("n", MaxNameLen): "1"}, KindTooLong},
	}
	for _, c := range cases {
		err := Validate(c.headers)
		switch {
		case c.kind == 0 && err != nil:
			t.Errorf("Validate(%v) = %v, want nil", c.headers, err)
		case c.kind != 0 && (err == nil || err.Kind != c.kind):
			t.Errorf("Validate(%v) = %v, want kind %d", c.headers, err, c.kind)
		}
	}

	many := map[string]string{}
	for i := range MaxCustom + 1 {
		many["X-N-"+string(rune('a'+i))] = "1"
	}

	if err := Validate(many); err == nil || err.Kind != KindTooMany {
		t.Errorf("Validate(%d headers) = %v, want too many", len(many), err)
	}
}

// The caller's spelling wins over a default with the same name, and a
// message without headers keeps a nil map.
func TestMergeLetsTheOwnHeaderWin(t *testing.T) {
	defaults := map[string]string{"X-Env": "staging", "X-Team": "ops"}
	own := map[string]string{"x-env": "prod", "X-Ticket": "1"}
	got := Merge(defaults, own)
	want := map[string]string{"x-env": "prod", "X-Ticket": "1", "X-Team": "ops"}
	if !maps.Equal(got, want) {
		t.Errorf("Merge = %v, want %v", got, want)
	}

	if defaults["X-Env"] != "staging" || len(own) != 2 {
		t.Error("Merge mutated an input")
	}

	if Merge(nil, nil) != nil {
		t.Error("Merge(nil, nil) should stay nil")
	}

	if got := Merge(defaults, nil); !maps.Equal(got, defaults) {
		t.Errorf("Merge(defaults, nil) = %v, want the defaults", got)
	}
}

func TestForwardableDropsReservedAndListed(t *testing.T) {
	parsed := map[string]string{
		"From":                         "a@example.com",
		"Message-Id":                   "<x@example.com>",
		"X-Mailyard-Sandbox-Retention": "1",
		"X-Mailer":                     "swaks",
		"X-Ticket":                     "4182",
	}
	got := Forwardable(parsed, []string{"x-mailer"})
	want := map[string]string{"X-Ticket": "4182"}
	if !maps.Equal(got, want) {
		t.Errorf("Forwardable = %v, want %v", got, want)
	}

	if Forwardable(map[string]string{"From": "a"}, nil) != nil {
		t.Error("nothing forwardable should be nil, not an empty map")
	}
}
