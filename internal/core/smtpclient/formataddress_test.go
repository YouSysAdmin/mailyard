// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpclient

import (
	"net/mail"
	"testing"
)

// A display name is DATA, and the two places that composed one by hand
// treated it as text.
//
// Every case here is something `fmt.Sprintf("%s <%s>", name, addr)`
// produced wrongly, and the last two are the ones that actually break
// mail rather than just look untidy.
func TestADisplayNameSurvivesBeingAName(t *testing.T) {
	cases := []struct {
		name  string
		addr  string
		want  string
		about string
	}{
		{"", "no-reply@faria.co", "no-reply@faria.co",
			"nothing to say, so a bare address"},
		{"Faria Education Group", "no-reply@faria.co",
			`"Faria Education Group" <no-reply@faria.co>`,
			"an ordinary name is quoted, which is always legal"},
		{"Faria, Inc.", "no-reply@faria.co",
			`"Faria, Inc." <no-reply@faria.co>`,
			"the comma is the address-list separator: unquoted this is TWO addresses"},
		{`He said "hi"`, "a@b.test", `"He said \"hi\"" <a@b.test>`,
			"a quote inside the quotes has to be escaped"},
		{"Олексій", "a@b.test", "=?utf-8?q?=D0=9E=D0=BB=D0=B5=D0=BA=D1=81=D1=96=D0=B9?= <a@b.test>",
			"a non-ASCII name is RFC 2047 encoded, not sent as raw bytes"},
	}
	for _, tc := range cases {
		got := FormatAddress(tc.name, tc.addr)
		if got != tc.want {
			t.Errorf("%s:\n  FormatAddress(%q, %q)\n  = %q\n  want %q",
				tc.about, tc.name, tc.addr, got, tc.want)
			continue
		}

		// And the result has to parse back to one mailbox with the same
		// address - which is the property the hand-composed version lost.
		list, err := mail.ParseAddressList(got)
		if err != nil {
			t.Errorf("%s: %q does not parse: %v", tc.about, got, err)
			continue
		}

		if len(list) != 1 {
			t.Errorf("%s: %q parses as %d addresses, want 1", tc.about, got, len(list))
			continue
		}

		if list[0].Address != tc.addr {
			t.Errorf("%s: %q parses to address %q, want %q",
				tc.about, got, list[0].Address, tc.addr)
		}

		if list[0].Name != tc.name {
			t.Errorf("%s: %q parses to name %q, want %q",
				tc.about, got, list[0].Name, tc.name)
		}

		// EnvelopeAddress is what the delivery path uses to get back to
		// the bare address, so the two have to agree.
		if EnvelopeAddress(got) != tc.addr {
			t.Errorf("%s: EnvelopeAddress(%q) = %q, want %q",
				tc.about, got, EnvelopeAddress(got), tc.addr)
		}
	}
}

// What the old composition did, kept as a statement of the bug rather
// than as a test of dead code.
func TestTheHandComposedFormWasTwoAddresses(t *testing.T) {
	byHand := "Faria, Inc. <no-reply@faria.co>"
	list, err := mail.ParseAddressList(byHand)
	if err != nil {
		// Some parsers refuse it outright, which is the other failure.
		return
	}

	if len(list) == 1 {
		t.Fatalf("expected %q to read as more than one address - if a stdlib change "+
			"made this legal, the reason for FormatAddress needs revisiting", byHand)
	}
}
