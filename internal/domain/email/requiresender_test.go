// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"errors"
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/internal/domain/store"
	dmodel "github.com/yousysadmin/mailyard/internal/models/domain"
)

func senderStore(d *dmodel.Domain) *store.Store {
	return &store.Store{Domain: &fakeDomains{verified: d}}
}

func TestRequireVerifiedSender(t *testing.T) {
	cases := []struct {
		name    string
		domain  *dmodel.Domain
		sender  string
		wantErr bool
	}{
		{
			name:   "a domain this project verified",
			domain: &dmodel.Domain{ProjectID: "proj-a", Domain: "example.com", Verified: true},
			sender: "hi@example.com",
		},
		{
			name:    "nobody verified it",
			domain:  nil,
			sender:  "hi@example.com",
			wantErr: true,
		},
		{
			// The one that made this a bug rather than a missing
			// feature. Domain names are globally unique here, so
			// without the project comparison any tenant could put a
			// neighbour's domain in From and have it delivered.
			name:    "another project verified it",
			domain:  &dmodel.Domain{ProjectID: "proj-b", Domain: "example.com", Verified: true},
			sender:  "hi@example.com",
			wantErr: true,
		},
		{
			// Controlling a zone means controlling every name under
			// it, which is how the rest of the ecosystem reads a
			// verified domain too. The bounce pattern depends on it:
			// the envelope sender lives on a subdomain with its own MX
			// and SPF so the provider's MX stays off the apex.
			name:   "a subdomain of a verified domain",
			domain: &dmodel.Domain{ProjectID: "proj-a", Domain: "example.com", Verified: true},
			sender: "hi@mail.example.com",
		},
		{
			// By whole labels. A suffix comparison would read this as
			// being under example.com.
			name:    "a lookalike that merely ends in the verified name",
			domain:  &dmodel.Domain{ProjectID: "proj-a", Domain: "example.com", Verified: true},
			sender:  "hi@evilexample.com",
			wantErr: true,
		},
		{
			name:    "a subdomain of a domain another project verified",
			domain:  &dmodel.Domain{ProjectID: "proj-b", Domain: "example.com", Verified: true},
			sender:  "hi@mail.example.com",
			wantErr: true,
		},
		{
			name:    "no domain at all",
			domain:  nil,
			sender:  "nonsense",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := RequireVerifiedSender(t.Context(), senderStore(tc.domain), "proj-a", tc.sender)
			if tc.wantErr && err == nil {
				t.Fatalf("sender %q was accepted, want a refusal", tc.sender)
			}

			if !tc.wantErr && err != nil {
				t.Fatalf("sender %q was refused: %v", tc.sender, err)
			}
		})
	}
}

// Telling the two refusals apart would let any tenant ask "is
// example.com registered here" one send at a time, and cross-project
// access is meant to look like a missing resource.
func TestRefusalDoesNotRevealAnotherProjectsDomain(t *testing.T) {
	unknown := RequireVerifiedSender(t.Context(), senderStore(nil), "proj-a", "hi@example.com")
	taken := RequireVerifiedSender(t.Context(),
		senderStore(&dmodel.Domain{ProjectID: "proj-b", Domain: "example.com", Verified: true}),
		"proj-a", "hi@example.com")
	if unknown == nil || taken == nil {
		t.Fatal("both cases must be refused")
	}

	if unknown.Error() != taken.Error() {
		t.Errorf("the two refusals differ, so the message says whether the domain exists:\n  unknown: %s\n  taken:   %s",
			unknown.Error(), taken.Error())
	}

	// And it must stay a caller mistake, not a 500.
	if _, ok := errors.AsType[*RequestError](unknown); !ok {
		t.Errorf("refusal is %T, want *RequestError so the endpoint answers 400", unknown)
	}

	if strings.Contains(unknown.Error(), "proj-b") {
		t.Error("the refusal names the owning project")
	}
}
