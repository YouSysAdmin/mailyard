// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package validation

import (
	"strings"
	"testing"
)

// If the ipcidr tag were never registered, validator would fail open
// on an unknown tag name in some configurations and the handlers using
// it would stop checking their input entirely - so assert both
// directions.
func TestIPCIDRTag(t *testing.T) {
	v := Init()

	type input struct {
		IPs []string `validate:"omitempty,dive,ipcidr"`
	}

	good := []string{"10.0.0.1", "192.168.1.0/24", "::1", "2001:db8::/32", " 10.0.0.1 "}
	if err := v.Struct(input{IPs: good}); err != nil {
		t.Errorf("valid entries rejected: %v", err)
	}

	for _, bad := range []string{"", "not-an-ip", "10.0.0.256", "10.0.0.0/33", "10.0.0.1-10.0.0.5"} {
		if err := v.Struct(input{IPs: []string{bad}}); err == nil {
			t.Errorf("entry %q accepted, want rejected", bad)
		}
	}
}

func TestIPCIDRMessage(t *testing.T) {
	v := Init()
	type input struct {
		AllowedIPs []string `validate:"omitempty,dive,ipcidr"`
	}
	err := v.Struct(input{AllowedIPs: []string{"nope"}})
	if err == nil {
		t.Fatal("expected a validation error")
	}

	fes := Humanize(err)
	if len(fes) != 1 {
		t.Fatalf("got %d field errors, want 1", len(fes))
	}

	// The generic fallback would not mention IP or CIDR at all.
	if !strings.Contains(fes[0].Message, "IP address or CIDR") {
		t.Errorf("message %q does not explain the rule", fes[0].Message)
	}
}

// A group slug is named in a send, so only the derived shape is taken.
func TestSlugTag(t *testing.T) {
	v := Init()

	type input struct {
		Slug string `validate:"omitempty,slug"`
	}

	for _, ok := range []string{"eu", "eu-west-1", "a1"} {
		if err := v.Struct(input{Slug: ok}); err != nil {
			t.Errorf("slug %q rejected: %v", ok, err)
		}
	}

	for _, bad := range []string{"has space", "a/b", "-a", "a-", "a--b", "Upper", "x_y"} {
		if err := v.Struct(input{Slug: bad}); err == nil {
			t.Errorf("slug %q accepted, want rejected", bad)
		}
	}
}

// A server sender list entry that is not an address or *@domain
// matches no sender, so it is refused.
func TestSenderRuleAndDomainNameTags(t *testing.T) {
	v := Init()

	type input struct {
		Emails  []string `validate:"omitempty,dive,senderrule"`
		Domains []string `validate:"omitempty,dive,domainname"`
	}

	good := input{
		Emails:  []string{"a@example.com", "*@example.com", "First.Last@Mail.Example.com"},
		Domains: []string{"example.com", "@example.com", "mail.example.co.uk"},
	}
	if err := v.Struct(good); err != nil {
		t.Errorf("valid entries rejected: %v", err)
	}

	for _, bad := range []string{"abc", "a@b", "*@", "*@nodot", "A <a@example.com>", "a@exa mple.com", "@example.com"} {
		if err := v.Struct(input{Emails: []string{bad}}); err == nil {
			t.Errorf("sender rule %q accepted, want rejected", bad)
		}
	}

	for _, bad := range []string{"abc", "example.com.", "-a.com", "a..com", "a_b.com", "*.example.com"} {
		if err := v.Struct(input{Domains: []string{bad}}); err == nil {
			t.Errorf("domain %q accepted, want rejected", bad)
		}
	}
}
