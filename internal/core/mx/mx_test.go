// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package mx

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

type fakeResolver struct {
	mx    map[string][]*net.MX
	hosts map[string][]string
	mxErr map[string]error
	hErr  map[string]error

	mxCalls   int
	hostCalls int
}

func (f *fakeResolver) LookupMX(_ context.Context, d string) ([]*net.MX, error) {
	f.mxCalls++
	if err, ok := f.mxErr[d]; ok {
		return nil, err
	}

	return f.mx[d], nil
}

func (f *fakeResolver) LookupHost(_ context.Context, h string) ([]string, error) {
	f.hostCalls++
	if err, ok := f.hErr[h]; ok {
		return nil, err
	}

	return f.hosts[h], nil
}

func notFound() error {
	return &net.DNSError{Err: "no such host", IsNotFound: true}
}

func servfail() error {
	return &net.DNSError{Err: "server misbehaving", IsTemporary: true}
}

func hostsOf(t []Target) []string {
	out := make([]string, len(t))
	for i, x := range t {
		out[i] = x.Host
	}

	return out
}

func TestPreferenceOrder(t *testing.T) {
	r := &fakeResolver{mx: map[string][]*net.MX{
		"example.com": {
			{Host: "backup.example.com.", Pref: 20},
			{Host: "primary.example.com.", Pref: 5},
			{Host: "middle.example.com.", Pref: 10},
		},
	}}
	got, err := New(Config{Resolver: r}).Resolve(t.Context(), "example.com")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	want := []string{"primary.example.com", "middle.example.com", "backup.example.com"}
	for i := range want {
		if got[i].Host != want[i] {
			t.Fatalf("order is %v, want %v", hostsOf(got), want)
		}
	}
}

// The trailing dot is what net.LookupMX returns. Left on, it becomes a
// TLS ServerName that matches no certificate, and every delivery to
// the domain fails verification for a reason nothing reports.
func TestTheTrailingDotIsStripped(t *testing.T) {
	r := &fakeResolver{mx: map[string][]*net.MX{
		"example.com": {{Host: "MX1.Example.COM.", Pref: 10}},
	}}
	got, err := New(Config{Resolver: r}).Resolve(t.Context(), "example.com")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if got[0].Host != "mx1.example.com" {
		t.Errorf("host is %q", got[0].Host)
	}
}

// RFC 7505. A single "." exchange means the domain accepts no mail at
// all. Without this the record becomes a hostname the node dials for
// three days before giving up.
func TestNullMXIsPermanent(t *testing.T) {
	r := &fakeResolver{mx: map[string][]*net.MX{
		"nomail.example": {{Host: ".", Pref: 0}},
	}}
	_, err := New(Config{Resolver: r}).Resolve(t.Context(), "nomail.example")
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("err is %v, want *mx.Error", err)
	}

	if !e.Permanent() {
		t.Error("null MX was reported as temporary, so the node would retry it for days")
	}

	if r.hostCalls != 0 {
		t.Error("null MX fell through to the A/AAAA fallback, which it must never do")
	}
}

// RFC 5321 section 5.1: no MX means try the domain itself.
func TestFallsBackToTheDomainItself(t *testing.T) {
	r := &fakeResolver{
		mx:    map[string][]*net.MX{},
		hosts: map[string][]string{"plain.example": {"192.0.2.10"}},
	}
	got, err := New(Config{Resolver: r}).Resolve(t.Context(), "plain.example")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if len(got) != 1 || got[0].Host != "plain.example" || !got[0].Implicit {
		t.Fatalf("targets are %+v", got)
	}
}

// Some resolvers report "no MX records" as a not-found error rather
// than an empty list. Giving up there would skip the fallback that
// RFC 5321 requires.
func TestANotFoundOnMXStillTriesTheAddress(t *testing.T) {
	r := &fakeResolver{
		mxErr: map[string]error{"plain.example": notFound()},
		hosts: map[string][]string{"plain.example": {"192.0.2.10"}},
	}
	got, err := New(Config{Resolver: r}).Resolve(t.Context(), "plain.example")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if len(got) != 1 || !got[0].Implicit {
		t.Fatalf("targets are %+v", got)
	}
}

func TestAnUnknownDomainIsPermanent(t *testing.T) {
	r := &fakeResolver{
		mxErr: map[string]error{"gone.example": notFound()},
		hErr:  map[string]error{"gone.example": notFound()},
	}
	_, err := New(Config{Resolver: r}).Resolve(t.Context(), "gone.example")
	var e *Error
	if !errors.As(err, &e) || !e.Permanent() {
		t.Fatalf("err is %v, want a permanent *mx.Error", err)
	}
}

// The distinction the whole package exists for. A resolver that is
// down is our problem, and bouncing somebody's mail over it turns a
// local outage into a suppressed address.
func TestAResolverFailureIsTemporary(t *testing.T) {
	r := &fakeResolver{mxErr: map[string]error{"example.com": servfail()}}
	_, err := New(Config{Resolver: r}).Resolve(t.Context(), "example.com")
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("err is %v, want *mx.Error", err)
	}

	if e.Permanent() {
		t.Error("SERVFAIL was reported as permanent, which bounces mail over our own DNS")
	}
}

func TestAPositiveAnswerIsCached(t *testing.T) {
	r := &fakeResolver{mx: map[string][]*net.MX{
		"example.com": {{Host: "mx.example.com.", Pref: 10}},
	}}
	l := New(Config{Resolver: r, CacheTTL: time.Minute})
	for range 3 {
		if _, err := l.Resolve(t.Context(), "example.com"); err != nil {
			t.Fatalf("Resolve: %v", err)
		}
	}

	if r.mxCalls != 1 {
		t.Errorf("made %d MX queries for 3 resolutions", r.mxCalls)
	}
}

// A failure must never be cached. The retry that would have worked is
// exactly the one the cache would answer from.
func TestAFailureIsNotCached(t *testing.T) {
	r := &fakeResolver{mxErr: map[string]error{"example.com": servfail()}}
	l := New(Config{Resolver: r, CacheTTL: time.Hour})
	if _, err := l.Resolve(t.Context(), "example.com"); err == nil {
		t.Fatal("expected a failure")
	}

	// DNS recovers.
	delete(r.mxErr, "example.com")
	r.mx = map[string][]*net.MX{"example.com": {{Host: "mx.example.com.", Pref: 10}}}

	got, err := l.Resolve(t.Context(), "example.com")
	if err != nil {
		t.Fatalf("the failure was cached, so recovery was invisible: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("targets are %+v", got)
	}
}

func TestCacheExpires(t *testing.T) {
	r := &fakeResolver{mx: map[string][]*net.MX{
		"example.com": {{Host: "mx.example.com.", Pref: 10}},
	}}
	now := time.Unix(1700000000, 0)
	l := New(Config{Resolver: r, CacheTTL: time.Minute})
	l.now = func() time.Time { return now }

	if _, err := l.Resolve(t.Context(), "example.com"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	now = now.Add(2 * time.Minute)
	if _, err := l.Resolve(t.Context(), "example.com"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if r.mxCalls != 2 {
		t.Errorf("made %d MX queries, want a second one after the TTL", r.mxCalls)
	}
}

// The cached slice is shared. Handing it out directly lets one
// delivery's shuffle or truncation rewrite what every other delivery
// to that domain sees.
func TestTheCallerCannotMutateTheCache(t *testing.T) {
	r := &fakeResolver{mx: map[string][]*net.MX{
		"example.com": {{Host: "a.example.com.", Pref: 10}, {Host: "b.example.com.", Pref: 20}},
	}}
	l := New(Config{Resolver: r, CacheTTL: time.Hour})

	first, err := l.Resolve(t.Context(), "example.com")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	first[0].Host = "attacker.example"

	second, err := l.Resolve(t.Context(), "example.com")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if second[0].Host != "a.example.com" {
		t.Errorf("the cache was mutated through a returned slice: %v", hostsOf(second))
	}
}

func TestEmptyDomainIsRejected(t *testing.T) {
	l := New(Config{Resolver: &fakeResolver{}})
	for _, in := range []string{"", "   ", "."} {
		if _, err := l.Resolve(t.Context(), in); err == nil {
			t.Errorf("Resolve(%q) was accepted", in)
		}
	}
}

// Equal preference is the only place a sender gets to spread load
// across a provider's front ends, and RFC 5321 asks for it. The bands
// themselves must still be ordered.
func TestEqualPreferenceIsShuffledWithinItsBand(t *testing.T) {
	r := &fakeResolver{mx: map[string][]*net.MX{
		"example.com": {
			{Host: "a.example.com.", Pref: 10},
			{Host: "b.example.com.", Pref: 10},
			{Host: "c.example.com.", Pref: 10},
			{Host: "z.example.com.", Pref: 99},
		},
	}}

	seen := map[string]bool{}
	for range 40 {
		l := New(Config{Resolver: r})
		got, err := l.Resolve(t.Context(), "example.com")
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}

		if got[3].Host != "z.example.com" {
			t.Fatalf("the shuffle escaped its preference band: %v", hostsOf(got))
		}

		seen[got[0].Host] = true
	}

	if len(seen) < 2 {
		t.Errorf("40 resolutions always picked %v first, so equal preferences are not shuffled", seen)
	}
}
