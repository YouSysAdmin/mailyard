// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package domains

import (
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	dmodel "github.com/yousysadmin/mailyard/internal/models/domain"
)

// An unverified claim does not hold a name from anybody. Two projects
// may claim it, only one may hold it VERIFIED, and verifying drops the
// other's claim of the name and of the names below it.
func TestAClaimIsPerProjectUntilItVerifies(t *testing.T) {
	s, _ := grantStore(t)
	ctx := t.Context()

	squatter := ids.New()
	seedDomain(t, s, squatter, thirdProj, "victim.example", false)
	below := ids.New()
	seedDomain(t, s, below, thirdProj, "mail.victim.example", false)
	owner := ids.New()
	seedDomain(t, s, owner, ownerProj, "victim.example", false)

	if d, err := s.GetByNameIn(ctx, ownerProj, "Victim.Example."); err != nil || d == nil || d.ID != owner {
		t.Fatalf("GetByNameIn = %+v %v, want the owner's own claim", d, err)
	}

	// A second claim of the same name in the same project is refused.
	if err := s.Put(ctx, &dmodel.Domain{
		ID: ids.New(), ProjectID: ownerProj, Domain: "victim.example.", VerificationToken: "x",
	}); err == nil {
		t.Error("a project claimed the same name twice, the dotted spelling slipped past")
	}

	d, err := s.Get(ctx, ownerProj, owner)
	if err != nil || d == nil {
		t.Fatal(err)
	}

	d.Verified = true
	if err := s.Put(ctx, d); err != nil {
		t.Fatalf("verify: %v", err)
	}

	dropped, err := s.DropStaleClaims(ctx, "victim.example", ownerProj)
	if err != nil || dropped != 2 {
		t.Fatalf("DropStaleClaims = %d %v, want the two stale claims", dropped, err)
	}

	if got, _ := s.Get(ctx, ownerProj, owner); got == nil {
		t.Error("the verified claim itself was dropped")
	}

	// A second verified row for one name cannot exist.
	if err := s.Put(ctx, &dmodel.Domain{
		ID: ids.New(), ProjectID: granteeProj, Domain: "victim.example", VerificationToken: "y", Verified: true,
	}); err == nil {
		t.Error("two projects hold the same name verified")
	}
}

// A trailing dot is the same name, so the claim drops it.
func TestATrailingDotIsDroppedFromAClaim(t *testing.T) {
	in := createInput{Domain: "  Victim.Example.  "}
	if err := validation.NormalizeAndValidate(&in); err != nil {
		t.Fatalf("validate: %v", err)
	}

	if in.Domain != "victim.example" {
		t.Errorf("domain = %q, want victim.example", in.Domain)
	}
}
