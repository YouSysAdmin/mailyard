// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package emailverify

import (
	"context"
	"net"
	"testing"
)

// fakeResolver answers from fixed maps so no test touches the network.
type fakeResolver struct {
	mx    map[string][]*net.MX
	hosts map[string][]string

	// fail simulates a transient resolver failure (not NXDOMAIN).
	fail  map[string]bool
	calls int
}

func (f *fakeResolver) LookupMX(_ context.Context, d string) ([]*net.MX, error) {
	f.calls++
	if f.fail[d] {
		return nil, &net.DNSError{Err: "server misbehaving", Name: d, IsTemporary: true}
	}

	if mx, ok := f.mx[d]; ok {
		return mx, nil
	}

	return nil, &net.DNSError{Err: "no such host", Name: d, IsNotFound: true}
}

func (f *fakeResolver) LookupHost(_ context.Context, h string) ([]string, error) {
	if f.fail[h] {
		return nil, &net.DNSError{Err: "server misbehaving", Name: h, IsTemporary: true}
	}

	if hosts, ok := f.hosts[h]; ok {
		return hosts, nil
	}

	return nil, &net.DNSError{Err: "no such host", Name: h, IsNotFound: true}
}

func newVerifier(r Resolver) *Verifier {
	return New(Config{}).WithResolver(r)
}

func TestSyntaxRejectsMalformed(t *testing.T) {
	v := newVerifier(&fakeResolver{})
	bad := []string{
		"", "nope", "@example.com", "user@", "user@nodot",
		"a b@example.com", "user@@example.com", "user@.example.com",
		"user@example..com", "user@-example.com", ".user@example.com",
		"user.@example.com",
	}
	for _, addr := range bad {
		res := v.Check(t.Context(), addr, true)
		if res.Status != StatusInvalid {
			t.Errorf("%q = %s, want invalid", addr, res.Status)
		}

		if res.Checks.Syntax {
			t.Errorf("%q should not pass the syntax check", addr)
		}
	}
}

func TestValidAddress(t *testing.T) {
	r := &fakeResolver{mx: map[string][]*net.MX{"example.com": {{Host: "mx.example.com"}}}}
	res := newVerifier(r).Check(t.Context(), "Alice@Example.com", true)

	if res.Status != StatusValid || res.Score != 90 {
		t.Errorf("status=%s score=%d, want valid/90", res.Status, res.Score)
	}

	if res.Email != "alice@example.com" {
		t.Errorf("email = %q, want it normalized", res.Email)
	}

	if !res.Checks.Syntax || !res.Checks.MX {
		t.Errorf("checks = %+v", res.Checks)
	}

	// The whole point of the package: this is never a mailbox proof.
	if res.MailboxVerified || res.Checks.SMTP != "skipped" {
		t.Error("a valid verdict must not claim the mailbox was probed")
	}
}

func TestDisposableShortCircuitsBeforeDNS(t *testing.T) {
	r := &fakeResolver{}
	res := newVerifier(r).Check(t.Context(), "someone@mailinator.com", true)

	if res.Status != StatusDisposable || res.Score != 10 {
		t.Errorf("status=%s score=%d, want disposable/10", res.Status, res.Score)
	}

	if r.calls != 0 {
		t.Errorf("resolver called %d times, want 0 - a known disposable domain should not cost a lookup", r.calls)
	}
}

func TestRoleAccountIsRiskyNotInvalid(t *testing.T) {
	r := &fakeResolver{mx: map[string][]*net.MX{"example.com": {{Host: "mx"}}}}
	v := newVerifier(r)
	for _, addr := range []string{"support@example.com", "INFO@example.com", "support+tag@example.com"} {
		res := v.Check(t.Context(), addr, true)
		if res.Status != StatusRisky || res.Score != 60 {
			t.Errorf("%s = %s/%d, want risky/60", addr, res.Status, res.Score)
		}

		if !res.Checks.RoleAccount {
			t.Errorf("%s should be flagged as a role account", addr)
		}
	}
}

func TestNoMXIsInvalid(t *testing.T) {
	r := &fakeResolver{}
	res := newVerifier(r).Check(t.Context(), "user@nowhere.example", true)
	if res.Status != StatusInvalid || res.Checks.MX {
		t.Errorf("status=%s mx=%v, want invalid with no mx", res.Status, res.Checks.MX)
	}
}

func TestAOnlyDomainCountsAsImplicitMX(t *testing.T) {
	// RFC 5321: a domain with an address record but no MX still
	// accepts mail at that host.
	r := &fakeResolver{hosts: map[string][]string{"legacy.example": {"192.0.2.1"}}}
	res := newVerifier(r).Check(t.Context(), "user@legacy.example", true)
	if res.Status != StatusValid || !res.Checks.MX {
		t.Errorf("status=%s mx=%v, want valid via the A record fallback", res.Status, res.Checks.MX)
	}
}

func TestTransientDNSFailureIsUnknownNotInvalid(t *testing.T) {
	r := &fakeResolver{fail: map[string]bool{"flaky.example": true}}
	v := newVerifier(r)
	res := v.Check(t.Context(), "user@flaky.example", true)

	if res.Status != StatusUnknown {
		t.Errorf("status = %s, want unknown - a resolver failure is not evidence the address is bad", res.Status)
	}

	// And it must not be cached, or one bad minute poisons the answer
	// for the whole TTL.
	r.fail = nil
	r.mx = map[string][]*net.MX{"flaky.example": {{Host: "mx"}}}
	again := v.Check(t.Context(), "user@flaky.example", false)
	if again.Status != StatusValid {
		t.Errorf("second check = %s, want valid once DNS recovered (an unknown must not be cached)", again.Status)
	}
}

func TestCacheHitAvoidsSecondLookup(t *testing.T) {
	r := &fakeResolver{mx: map[string][]*net.MX{"example.com": {{Host: "mx"}}}}
	v := newVerifier(r)

	first := v.Check(t.Context(), "user@example.com", false)
	if first.Cached {
		t.Error("the first check cannot be a cache hit")
	}

	before := r.calls

	second := v.Check(t.Context(), "user@example.com", false)
	if !second.Cached {
		t.Error("the second check should be served from cache")
	}

	if r.calls != before {
		t.Errorf("resolver called again (%d -> %d) despite the cache", before, r.calls)
	}

	// fresh=true must bypass it.
	third := v.Check(t.Context(), "user@example.com", true)
	if third.Cached {
		t.Error("fresh=true must bypass the cache")
	}

	if r.calls == before {
		t.Error("fresh=true should have caused a new lookup")
	}
}

// RFC 7505: a domain publishing a single MX with the root as its target
// is declaring that it accepts no mail.
//
// It used to score valid/90, because the check was `err == nil &&
// len(mx) > 0` and a null MX satisfies both. The domain owner had said
// the opposite of what we reported.
func TestNullMXIsNotDeliverable(t *testing.T) {
	for _, host := range []string{".", ""} {
		r := &fakeResolver{mx: map[string][]*net.MX{"example.com": {{Host: host}}}}
		res := newVerifier(r).Check(t.Context(), "alice@example.com", true)

		if res.Checks.MX {
			t.Errorf("host %q: MX check passed for a null MX", host)
		}

		if res.Status == StatusValid {
			t.Errorf("host %q: status = valid for a domain that accepts no mail", host)
		}
	}
}

// A null MX must not fall through to the A/AAAA fallback. That fallback
// is for a domain with no MX at all, and RFC 7505 exists to stop the
// implicit A lookup - so a host record must not rescue it.
func TestNullMXDoesNotFallBackToAddressRecords(t *testing.T) {
	r := &fakeResolver{
		mx:    map[string][]*net.MX{"example.com": {{Host: "."}}},
		hosts: map[string][]string{"example.com": {"192.0.2.1"}},
	}
	res := newVerifier(r).Check(t.Context(), "alice@example.com", true)

	if res.Checks.MX {
		t.Error("an A record rescued a null MX")
	}
}

// A null MX alongside a real host is a misconfiguration, and the
// deliverable reading of it is the real host - so this stays valid.
func TestNullMXBesideARealHostIsStillDeliverable(t *testing.T) {
	r := &fakeResolver{mx: map[string][]*net.MX{
		"example.com": {{Host: "."}, {Host: "mx.example.com"}},
	}}
	res := newVerifier(r).Check(t.Context(), "alice@example.com", true)

	if !res.Checks.MX {
		t.Error("a real host beside a null MX was treated as no mail server")
	}
}
