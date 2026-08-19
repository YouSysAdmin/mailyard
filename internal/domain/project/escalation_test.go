// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package project

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	perm "github.com/yousysadmin/mailyard/internal/models/permission"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
)

// everyPermission enumerates the catalogue, which is what an escalation
// looks like written out longhand: the wildcard is refused in a role and
// this is not.
func everyPermission() []string {
	var out []string
	for _, d := range perm.Registry {
		for _, a := range d.Actions {
			out = append(out, string(perm.Of(d.Resource, a)))
		}
	}

	return out
}

// inRequest runs fn inside a real request, so the refusal helpers have a
// live fiber.Ctx to write to.
//
// A real one, not a hand-built context: these helpers write a response
// and return nil, and the bool beside it is the load-bearing half - the
// exact shape TestARefusalHelperIsReturnedAndNotTested exists for. A ctx
// captured out of a handler is released when the request ends, and using
// it afterwards panics inside response.Forbidden. Which it did.
func inRequest(t *testing.T, fn func(c fiber.Ctx)) {
	t.Helper()

	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		fn(c)

		return nil
	})
	res, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/", nil))
	if err != nil {
		t.Fatalf("probe request: %v", err)
	}

	_ = res.Body.Close()
}

// members:write must not be a way to reach everything else.
//
// It is the whole gate on writing roles AND on assigning them, and
// nothing bounded what a role could carry. So a member trusted only to
// manage people could mint a role holding the entire catalogue and give
// it to themselves - two calls from "may invite colleagues" to "may take
// the full project export and erase it". Assigning an EXISTING powerful
// role is the same escalation without even the first call.
//
// The absence of a bound was hidden by a comment that read "a
// members:write holder can already hand out any role through AddMember,
// so this delegates nothing new" - true, and the wrong conclusion: if
// writing delegates nothing new then assigning delegated everything.
//
// Four doors, one rule. This asserts it at the two seams both role writes
// and all three assignment paths go through.
func TestAMembersWriterCannotGrantWhatItDoesNotHold(t *testing.T) {
	// A member trusted with people and nothing else.
	manager := access{
		member: true,
		perms:  perm.NewSet(perm.Of(perm.ResourceMembers, perm.ActionWrite)),
	}
	// An owner, who holds the wildcard.
	owner := access{member: true, owner: true, perms: perm.NewSet(perm.All)}

	all := everyPermission()
	if len(all) < 40 {
		t.Fatalf("the catalogue enumerated to %d entries - the fixture is broken, not the code", len(all))
	}

	mine := []string{string(perm.Of(perm.ResourceMembers, perm.ActionWrite))}

	inRequest(t, func(c fiber.Ctx) {
		// WRITING a role that grants more than the caller holds.
		if _, refused := refuseGranting(c, manager, all); !refused {
			t.Error("a members:write holder was allowed to write a role granting the whole catalogue")
		}
	})

	inRequest(t, func(c fiber.Ctx) {
		// ASSIGNING a role that grants more than the caller holds - the
		// same escalation with the role already there.
		if _, refused := refuseDelegating(c, manager,
			&projmodel.Role{Name: "Everything", Permissions: all}); !refused {
			t.Error("a members:write holder was allowed to assign a role granting the whole catalogue")
		}
	})

	// What they do hold is still theirs to delegate, or the rule would
	// have replaced an escalation with a member nobody can manage.
	inRequest(t, func(c fiber.Ctx) {
		if _, refused := refuseGranting(c, manager, mine); refused {
			t.Error("a members:write holder cannot delegate members:write, which is what they hold")
		}

		if _, refused := refuseDelegating(c, manager,
			&projmodel.Role{Name: "Manager", Permissions: mine}); refused {
			t.Error("a members:write holder cannot assign a role equal to their own permissions")
		}

		// A role granting nothing is a deliberate lockdown and must stay
		// assignable by anybody who manages members.
		if _, refused := refuseDelegating(c, manager, &projmodel.Role{Name: "Locked"}); refused {
			t.Error("a role granting nothing was refused")
		}

		// An owner delegates anything. That is what ownership is, and it
		// is recorded on the membership row rather than in a permission
		// list.
		if _, refused := refuseGranting(c, owner, all); refused {
			t.Error("an owner was refused the right to write a role")
		}

		if _, refused := refuseDelegating(c, owner,
			&projmodel.Role{Name: "Everything", Permissions: all}); refused {
			t.Error("an owner was refused the right to assign a role")
		}
	})
}

// Missing names which permissions are short, because a bare refusal on a
// screen with sixty checkboxes is unactionable.
func TestTheRefusalNamesWhatIsMissing(t *testing.T) {
	caller := perm.NewSet(perm.Of(perm.ResourceMembers, perm.ActionWrite))
	want := []string{
		string(perm.Of(perm.ResourceMembers, perm.ActionWrite)),
		string(perm.Of(perm.ResourceData, perm.ActionRead)),
		"not-a-permission",
	}
	short := caller.Missing(want)
	if len(short) != 1 || short[0] != string(perm.Of(perm.ResourceData, perm.ActionRead)) {
		t.Errorf("Missing = %v, want only data:read - what is held must not be reported, "+
			"and an unparseable entry is the validator's business", short)
	}

	if len(perm.NewSet(perm.All).Missing(everyPermission())) != 0 {
		t.Error("the wildcard is short of something, so an owner would be refused")
	}
}
