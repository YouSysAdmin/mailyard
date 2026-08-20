// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpserver

import "testing"

// A listed domain does not cover its subdomains, and that is the whole
// point of the rule rather than an oversight.
//
// Domain VERIFICATION covers subdomains - GetVerifiedCovering answers
// on label boundaries - so the instinct is to match here too. SPF says
// otherwise: a relay named in the record for example.com is not thereby
// named in the record for mail.example.com, and mail leaving through it
// as that subdomain fails exactly the check this list exists to keep
// passing. So the subdomain gets its own line or it does not go.
func TestAnAllowedDomainDoesNotCoverItsSubdomains(t *testing.T) {
	s := &Server{AllowedDomains: []string{"example.com"}}

	if !s.AllowsDomain("hi@example.com") {
		t.Error("the listed domain itself must be allowed")
	}

	if s.AllowsDomain("hi@mail.example.com") {
		t.Error("a subdomain must not ride on the parent's entry - SPF is written per name")
	}

	if s.AllowsDomain("hi@notexample.com") {
		t.Error("a suffix match is not a domain match")
	}
}

// Empty is not "deny everything". Every row that existed before the
// column did carries an empty list, and a project that never split its
// servers by domain must keep sending.
func TestAnEmptyDomainListAllowsAnySender(t *testing.T) {
	s := &Server{}

	if !s.AllowsDomain("hi@anywhere.test") {
		t.Error("an unrestricted server must carry any domain")
	}
}

// The two lists are separate questions and both are asked. A server
// restricted by domain but not by address admits every mailbox in that
// domain, which is the case a project splitting relay nodes actually
// has - its addresses are not enumerated anywhere.
func TestTheDomainRuleStandsWithoutAnAddressRule(t *testing.T) {
	s := &Server{AllowedDomains: []string{"example.com"}}

	if !s.AllowsSender("anything@example.com") {
		t.Error("an empty allowed_emails must not refuse anybody")
	}

	if !s.AllowsDomain("anything@example.com") || s.AllowsDomain("anything@other.test") {
		t.Error("the domain rule must decide on its own")
	}
}
