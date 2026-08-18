// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package permission

import (
	"sort"
	"testing"
)

// TestEveryResourceDeclaresUsableActions is the shape check on the
// catalogue itself.
//
// A resource with no actions is a row in the console grid with no
// boxes. A duplicate is a box rendered twice. Neither fails anywhere
// else - both just look like a rendering bug to whoever hits them.
func TestEveryResourceDeclaresUsableActions(t *testing.T) {
	seen := map[Resource]bool{}
	for _, d := range Registry {
		if seen[d.Resource] {
			t.Errorf("%s appears twice in the registry", d.Resource)
		}

		seen[d.Resource] = true

		if len(d.Actions) == 0 {
			t.Errorf("%s declares no actions, so nothing can ever be granted on it", d.Resource)
		}

		if d.Label == "" || d.Description == "" {
			t.Errorf("%s has no label or description - the grid renders both", d.Resource)
		}

		dup := map[Action]bool{}
		for _, a := range d.Actions {
			switch a {
			case ActionRead, ActionWrite, ActionDelete:
			default:
				t.Errorf("%s declares unknown action %q", d.Resource, a)
			}

			if dup[a] {
				t.Errorf("%s declares %s twice", d.Resource, a)
			}

			dup[a] = true
		}
	}
}

// TestParseRefusesAnActionTheResourceDoesNotHave is the reason
// Definition.Actions exists at all.
//
// Before it, the catalogue advertised three permissions no route has
// ever asked for - analytics:write, contacts:write, audit:write -
// because two actions were assumed for every resource. Each of those
// could be ticked in the grid, stored in a role, and would read
// forever as a granted privilege that grants nothing.
func TestParseRefusesAnActionTheResourceDoesNotHave(t *testing.T) {
	// Real resource, real action, and still not a thing.
	for _, bad := range []string{
		"contacts:write", "contacts:delete",
		"analytics:write", "analytics:delete",
		"audit:write", "audit:delete",
		"data:write",      // export or erase, nothing between
		"emails:delete",   // erasure of mail is the GDPR surface
		"relay:read",      // enrolment only
		"settings:delete", // deleting the project is ownership, not a permission
	} {
		if _, _, ok := Parse(bad); ok {
			t.Errorf("Parse accepted %q, which no route enforces", bad)
		}
	}

	// And the ones that are real.
	for _, good := range []string{
		"templates:delete", "campaigns:delete", "smtp:delete", "members:delete",
		"data:delete", "relay:write", "emails:write", "contacts:read",
	} {
		if _, _, ok := Parse(good); !ok {
			t.Errorf("Parse refused %q, which the router does enforce", good)
		}
	}
}

// TestParseRejectsRubbish - Parse feeds anything that later stores a
// project's own policy, so it has to refuse rather than invent.
func TestParseRejectsRubbish(t *testing.T) {
	for _, bad := range []string{
		"", "emails", "emails:", ":read", "nosuch:read",
		"emails:READ", "*", "emails:read:extra",
	} {
		if _, _, ok := Parse(bad); ok {
			t.Errorf("Parse accepted %q", bad)
		}
	}

	r, a, ok := Parse("emails:read")
	if !ok || r != ResourceEmails || a != ActionRead {
		t.Errorf("Parse(emails:read) = %v %v %v", r, a, ok)
	}
}

// TestTouchesAsksTheResourceWhichActionsExist.
//
// Touches is the group gate's early refusal, and it used to try read
// then write. A resource whose only action is neither - relay, which
// is write-only, or anything that later gains delete alone - would be
// answered "you may not reach this group" for a permission the caller
// actually holds.
func TestTouchesAsksTheResourceWhichActionsExist(t *testing.T) {
	if !NewSet(Of(ResourceRelay, ActionWrite)).Touches(ResourceRelay) {
		t.Error("a relay:write holder does not touch relay, so the group gate would refuse them")
	}

	if !NewSet(Of(ResourceTemplates, ActionDelete)).Touches(ResourceTemplates) {
		t.Error("a delete-only holder does not touch the resource")
	}

	if NewSet(Of(ResourceEmails, ActionRead)).Touches(ResourceTemplates) {
		t.Error("Touches answered for a resource the set says nothing about")
	}

	// The nil set is what a RequestContext carries before the project
	// has resolved.
	var nilSet Set
	if nilSet.Has(ResourceEmails, ActionRead) || nilSet.Touches(ResourceEmails) {
		t.Error("the nil set allows something")
	}
}

// TestSetListIsSorted - the console renders this and two members' sets
// get compared by eye.
func TestSetListIsSorted(t *testing.T) {
	got := FromStrings([]string{
		"templates:write", "emails:read", "templates:delete", "campaigns:read",
	}).List()
	if len(got) != 4 {
		t.Fatalf("built %d entries, want 4: %v", len(got), got)
	}

	if !sort.StringsAreSorted(got) {
		t.Errorf("List() is not sorted: %v", got)
	}
}

// TestFromStringsSkipsWhatItCannotParse pins the read-side tolerance.
//
// A role is data and data outlives code: a resource renamed two
// releases from now leaves a string in the column that Parse refuses.
// Failing the whole set would lock every holder out over one stale
// entry, honoring it would grant something nobody defined - skipping
// grants nothing and costs nothing else.
func TestFromStringsSkipsWhatItCannotParse(t *testing.T) {
	s := FromStrings([]string{
		"emails:read", "nosuch:read", "contacts:write", "*", "", "templates:delete",
	})
	if !s.Has(ResourceEmails, ActionRead) || !s.Has(ResourceTemplates, ActionDelete) {
		t.Error("valid entries were lost alongside the invalid ones")
	}

	if len(s) != 2 {
		t.Errorf("set holds %d entries, want the 2 valid ones - the wildcard must not survive "+
			"into a project role's set: %v", len(s), s.List())
	}

	// Empty input is a set, not nil: an empty role is a deliberate
	// lockdown and Has on it must answer false by content, not by
	// accident of a nil map.
	if got := FromStrings(nil); got == nil {
		t.Error("FromStrings(nil) returned a nil set")
	}
}

// TestForMemberResolvesOwnerThenRoleThenNothing pins the three cases
// the middleware and the project endpoint both rely on, in order.
func TestForMemberResolvesOwnerThenRoleThenNothing(t *testing.T) {
	// Owner holds the wildcard, and holds it even while carrying a
	// role - ownership is not a role and cannot be narrowed by one.
	own := ForMember(true, []string{"emails:read"}, true)
	if len(own) != 1 {
		t.Errorf("owner resolved to %d entries, want the literal star: %v", len(own), own.List())
	}

	for _, d := range Registry {
		for _, a := range d.Actions {
			if !own.Has(d.Resource, a) {
				t.Errorf("owner cannot %s:%s", d.Resource, a)
			}
		}
	}

	// A role replaces nothing and grants exactly itself.
	got := ForMember(false, []string{"emails:read"}, true)
	if !got.Has(ResourceEmails, ActionRead) || got.Has(ResourceEmails, ActionWrite) {
		t.Error("a role granted something other than what it lists")
	}

	// An empty role is a lockdown, not an absence. The store's join
	// answers hasRole, so this case is reachable and must not fall
	// through to anything.
	locked := ForMember(false, nil, true)
	if len(locked) != 0 {
		t.Errorf("an empty role yielded %d permissions", len(locked))
	}

	// No role at all - and there is no floor to land on. The project
	// names a default role or its members reach nothing.
	none := ForMember(false, []string{"emails:read"}, false)
	if len(none) != 0 {
		t.Errorf("a member with no role resolved to %v, want nothing", none.List())
	}
}

// TestForKeyEmptyGrantsNothing pins the direction the API key default
// was flipped in.
//
// Treating an empty list as "send" is the least surprising default while
// sending is all a key can do. On a machine surface that reaches the
// whole project, an unstated
// intention has to fail closed - otherwise a key minted by a script
// that forgot the field can post mail.
func TestForKeyEmptyGrantsNothing(t *testing.T) {
	for _, empty := range [][]string{nil, {}} {
		s := ForKey(empty, false)
		if len(s) != 0 {
			t.Fatalf("ForKey(%v) granted %v, want nothing", empty, s.List())
		}

		for _, d := range Registry {
			if s.Touches(d.Resource) {
				t.Errorf("ForKey(%v) touches %s", empty, d.Resource)
			}
		}
	}
}

// TestForKeyHonoursTheWildcard - a key may hold what a project role
// may not. See ForKey for why the two differ.
func TestForKeyHonoursTheWildcard(t *testing.T) {
	s := ForKey([]string{"emails:read", All}, false)
	for _, d := range Registry {
		for _, a := range d.Actions {
			if !s.Has(d.Resource, a) {
				t.Fatalf("wildcard key cannot %s:%s", d.Resource, a)
			}
		}
	}

	// A set of one, so a later resource is covered too rather than the
	// wildcard being expanded at resolve time into today's list.
	if len(s) != 1 {
		t.Errorf("wildcard resolved to %d entries, want the literal star", len(s))
	}
}

// TestForKeySkipsWhatItCannotParse - read-side leniency, same as a
// project role: a renamed resource must neither grant nor lock out.
func TestForKeySkipsWhatItCannotParse(t *testing.T) {
	s := ForKey([]string{"emails:read", "contacts:write", "nonsense", "gone:read", ""}, false)
	if !s.Has(ResourceEmails, ActionRead) {
		t.Error("the parseable entry was lost")
	}

	if len(s) != 1 {
		t.Errorf("granted %v, want only emails:read", s.List())
	}
}

// TestResolvedSetsAreAlwaysFresh - a resolved set lands on a
// RequestContext, and one mutation of a shared map would grant or
// revoke for every request in the process until the next restart.
//
// The presets that made this a live hazard are gone, but both
// resolvers still build maps and both are still handed out, so the
// property is worth holding on to rather than deleting with them.
func TestResolvedSetsAreAlwaysFresh(t *testing.T) {
	a := ForKey([]string{All}, false)
	b := ForKey([]string{All}, false)
	a[Of(ResourceEmails, ActionRead)] = struct{}{}
	if len(b) != 1 {
		t.Fatal("two ForKey calls share a map")
	}

	c := ForMember(true, nil, false)
	d := ForMember(true, nil, false)
	c[Of(ResourceEmails, ActionRead)] = struct{}{}
	if len(d) != 1 {
		t.Fatal("two ForMember calls share a map")
	}
}

// TestTheSandboxFlagIsTheWholeDefinition.
//
// The flag decides that everything a key sends is captured, and it
// also decides that the key is judged on the SANDBOX rather than on
// emails. Between those two it has to GRANT something, or ticking it
// alone mints a credential that answers "no access to sandbox in this
// project" to every call - which is what it did: the console showed a
// checkbox and a permission grid, and only both together worked.
//
// Two switches for one idea, and from an SDK the second one is
// invisible.
func TestTheSandboxFlagIsTheWholeDefinition(t *testing.T) {
	// Ticked and nothing else. This is what a person hands a
	// contractor, and it has to work on its own.
	only := ForKey(nil, true)
	for _, a := range []Action{ActionRead, ActionWrite, ActionDelete} {
		if !only.Has(ResourceSandbox, a) {
			t.Errorf("a sandbox key cannot sandbox:%s, so the flag alone mints a useless key", a)
		}
	}

	// And nothing beyond the sandbox: it must not become a way to
	// reach real mail.
	for _, d := range Registry {
		if d.Resource == ResourceSandbox {
			continue
		}

		if only.Touches(d.Resource) {
			t.Errorf("a sandbox key reaches %s", d.Resource)
		}
	}

	// The flag ADDS, never subtracts - a sandbox key that also needs
	// to read templates says so in its list and keeps both.
	both := ForKey([]string{"templates:read"}, true)
	if !both.Has(ResourceTemplates, ActionRead) || !both.Has(ResourceSandbox, ActionWrite) {
		t.Errorf("the flag replaced the list instead of adding to it: %v", both.List())
	}

	// Without the flag nothing is implied, which is the rule an empty
	// list has everywhere else.
	if len(ForKey(nil, false)) != 0 {
		t.Error("a key with no flag and no list was granted something")
	}
}
