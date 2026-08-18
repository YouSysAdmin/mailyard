// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package domains

import (
	"slices"
	"testing"

	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	dmodel "github.com/yousysadmin/mailyard/internal/models/domain"
)

func coveringStore(t *testing.T) *Store {
	t.Helper()
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	dbtest.Schema(t, db, `
        INSERT INTO projects (id, name, slug, default_language, created_at)
        VALUES ('e66e7a4d-9e6c-4884-869a-cf9ffcf22181', 'One', 'one', 'en', now()),
               ('6a5f0b90-6a56-47f4-8926-7cc56968798b', 'Two', 'two', 'en', now())`)

	return &Store{Base: database.NewBase(db)}
}

func seedDomain(t *testing.T, s *Store, id, projID, name string, verified bool) {
	t.Helper()
	if err := s.Put(t.Context(), &dmodel.Domain{
		ID: id, ProjectID: projID, Domain: name,
		VerificationToken: "tok-" + id, Verified: verified,
	}); err != nil {
		t.Fatalf("seed %s: %v", name, err)
	}
}

func TestGetVerifiedCovering(t *testing.T) {
	s := coveringStore(t)
	seedDomain(t, s, "504a0295-c50b-4e67-82c9-e916c01ecbd0", "e66e7a4d-9e6c-4884-869a-cf9ffcf22181", "managebac.com", true)
	seedDomain(t, s, "abb2e27c-6e7d-4568-8927-77d45ddf88df", "e66e7a4d-9e6c-4884-869a-cf9ffcf22181", "unverified.com", false)

	cases := []struct {
		name       string
		lookup     string
		wantDomain string
	}{
		{"exact", "managebac.com", "managebac.com"},
		{"the bounce subdomain", "mailer.managebac.com", "managebac.com"},
		{"deeper still", "a.b.managebac.com", "managebac.com"},
		{"a lookalike is not covered", "evilmanagebac.com", ""},
		{"an unrelated domain", "example.com", ""},
		{"an unverified domain covers nothing", "mail.unverified.com", ""},
		{"the unverified domain itself", "unverified.com", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.GetVerifiedCovering(t.Context(), tc.lookup)
			if err != nil {
				t.Fatal(err)
			}

			if tc.wantDomain == "" {
				if got != nil {
					t.Fatalf("%q resolved to %q, want nothing", tc.lookup, got.Domain)
				}

				return
			}

			if got == nil {
				t.Fatalf("%q resolved to nothing, want %q", tc.lookup, tc.wantDomain)
			}

			if got.Domain != tc.wantDomain {
				t.Errorf("%q resolved to %q, want %q", tc.lookup, got.Domain, tc.wantDomain)
			}
		})
	}
}

// Most specific wins, and it decides the owner. A parent must not
// absorb a name another project proved separately - that would hand
// one tenant the right to send as another's subdomain.
func TestTheMostSpecificVerifiedDomainWins(t *testing.T) {
	s := coveringStore(t)
	seedDomain(t, s, "504a0295-c50b-4e67-82c9-e916c01ecbd0", "e66e7a4d-9e6c-4884-869a-cf9ffcf22181", "managebac.com", true)
	seedDomain(t, s, "abb2e27c-6e7d-4568-8927-77d45ddf88df", "6a5f0b90-6a56-47f4-8926-7cc56968798b", "mailer.managebac.com", true)

	got, err := s.GetVerifiedCovering(t.Context(), "mailer.managebac.com")
	if err != nil {
		t.Fatal(err)
	}

	if got == nil || got.ProjectID != "6a5f0b90-6a56-47f4-8926-7cc56968798b" {
		t.Fatalf("mailer.managebac.com resolved to %+v, want the project that verified it", got)
	}

	// Anything under the more specific name follows it.
	deeper, err := s.GetVerifiedCovering(t.Context(), "eu.mailer.managebac.com")
	if err != nil {
		t.Fatal(err)
	}

	if deeper == nil || deeper.ProjectID != "6a5f0b90-6a56-47f4-8926-7cc56968798b" {
		t.Fatalf("eu.mailer.managebac.com resolved to %+v, want p2", deeper)
	}

	// While a sibling still falls through to the apex.
	sibling, err := s.GetVerifiedCovering(t.Context(), "www.managebac.com")
	if err != nil {
		t.Fatal(err)
	}

	if sibling == nil || sibling.ProjectID != "e66e7a4d-9e6c-4884-869a-cf9ffcf22181" {
		t.Fatalf("www.managebac.com resolved to %+v, want p1", sibling)
	}
}

// The accept list a relay node caches.
//
// Two properties, and both are load-bearing somewhere else. Only
// VERIFIED names, because a node answering RCPT off this list is the
// only gate in front of an open port 25 - an unverified name here
// would make the node accept mail the platform then refuses, which
// costs the bandwidth twice and bounces nothing. And SORTED, because
// the caller fingerprints the list to decide whether it has to travel
// at all, so an unstable order would resend it on every heartbeat of
// every node forever.
func TestVerifiedNamesIsTheAcceptListAndIsStable(t *testing.T) {
	s := coveringStore(t)
	const one = "e66e7a4d-9e6c-4884-869a-cf9ffcf22181"
	const two = "6a5f0b90-6a56-47f4-8926-7cc56968798b"

	seedDomain(t, s, "3f6b2c11-0c1a-4d64-9a2f-1f9f1e2a5b01", one, "zeta.example.com", true)
	seedDomain(t, s, "3f6b2c11-0c1a-4d64-9a2f-1f9f1e2a5b02", two, "alpha.example.com", true)
	seedDomain(t, s, "3f6b2c11-0c1a-4d64-9a2f-1f9f1e2a5b03", one, "pending.example.com", false)

	got, err := s.VerifiedNames(t.Context())
	if err != nil {
		t.Fatalf("VerifiedNames: %v", err)
	}

	want := []string{"alpha.example.com", "zeta.example.com"}
	if !slices.Equal(got, want) {
		t.Fatalf("VerifiedNames() = %v, want %v", got, want)
	}
}
