// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package project

import (
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
)

func roleFixture(t *testing.T) (*Store, string, string) {
	t.Helper()
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	st := NewStore(db)

	ctx := t.Context()
	mk := func(name string) string {
		id := ids.New()
		if _, err := db.ExecContext(ctx, `
			INSERT INTO projects (id, name, slug, owner_id, created_at)
			VALUES ($1, $2, $3, NULL, now())`, id, name, id); err != nil {
			t.Fatalf("insert project: %v", err)
		}

		return id
	}

	return st, mk("acme"), mk("other")
}

func addRoleMember(t *testing.T, st *Store, projID string, owner bool) string {
	t.Helper()
	userID := ids.New()
	_, err := st.DB().ExecContext(t.Context(), `
		INSERT INTO users (id, email, account_type) VALUES ($1, $2, 1)`,
		userID, userID+"@example.invalid")
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	m := &projmodel.Member{ProjectID: projID, UserID: userID, Owner: owner}
	if err := st.PutMember(t.Context(), m); err != nil {
		t.Fatalf("put member: %v", err)
	}

	return userID
}

func TestRoleRoundTripAndMemberResolution(t *testing.T) {
	st, proj, _ := roleFixture(t)
	ctx := t.Context()

	role := &projmodel.Role{
		ProjectID:   proj,
		Name:        "support",
		Description: "reads mail, writes suppressions",
		Permissions: []string{"emails:read", "suppressions:read", "suppressions:write"},
	}
	if err := st.PutRole(ctx, role); err != nil {
		t.Fatalf("put role: %v", err)
	}

	user := addRoleMember(t, st, proj, false)
	ok, err := st.SetMemberRole(ctx, proj, user, role.ID)
	if err != nil || !ok {
		t.Fatalf("assign role: ok=%v err=%v", ok, err)
	}

	// The membership read resolves the role in the same round trip -
	// this runs on every project-scoped request, so a second query
	// here would be a second query everywhere.
	m, err := st.GetMember(ctx, proj, user)
	if err != nil {
		t.Fatalf("get member: %v", err)
	}

	if !m.HasRole || m.RoleName != "support" || len(m.RolePermissions) != 3 {
		t.Fatalf("member did not resolve its role: %+v", m)
	}

	if m.InheritedRole {
		t.Error("a role assigned to the member read back as inherited from the project default")
	}

	if m.Owner {
		t.Error("assigning a role made somebody an owner")
	}
}

// The default is what makes a member with no role of their own useful,
// and it is resolved in SQL so no code path can forget to apply it.
func TestAMemberWithNoRoleInheritsTheProjectDefault(t *testing.T) {
	st, proj, _ := roleFixture(t)
	ctx := t.Context()

	user := addRoleMember(t, st, proj, false)

	// Before a default is named, a roleless member reaches nothing.
	// This is the floor, and it is deliberately not a permissive one.
	m, err := st.GetMember(ctx, proj, user)
	if err != nil {
		t.Fatal(err)
	}

	if m.HasRole || m.RoleName != "" {
		t.Fatalf("a member of a project with no default role resolved one: %+v", m)
	}

	base := &projmodel.Role{ProjectID: proj, Name: "member", Permissions: []string{"emails:read"}}
	if err := st.PutRole(ctx, base); err != nil {
		t.Fatal(err)
	}

	if ok, err := st.SetDefaultRole(ctx, proj, base.ID); err != nil || !ok {
		t.Fatalf("set default: ok=%v err=%v", ok, err)
	}

	m, err = st.GetMember(ctx, proj, user)
	if err != nil {
		t.Fatal(err)
	}

	if !m.HasRole || m.RoleName != "member" || len(m.RolePermissions) != 1 {
		t.Fatalf("the project default was not applied: %+v", m)
	}

	// The console shows the difference, because "everyone here is a
	// member" and "this person was made a member" survive a change to
	// the default differently.
	if !m.InheritedRole {
		t.Error("an inherited role was reported as the member's own")
	}

	// A role of their own wins over the default.
	own := &projmodel.Role{ProjectID: proj, Name: "support", Permissions: []string{"emails:read", "emails:write"}}
	if err := st.PutRole(ctx, own); err != nil {
		t.Fatal(err)
	}

	if ok, _ := st.SetMemberRole(ctx, proj, user, own.ID); !ok {
		t.Fatal("assign failed")
	}

	m, err = st.GetMember(ctx, proj, user)
	if err != nil {
		t.Fatal(err)
	}

	if m.RoleName != "support" || m.InheritedRole {
		t.Fatalf("the member's own role lost to the default: %+v", m)
	}
}

// PutMember re-puts happen on AddMember, AcceptInvitation and OIDC
// auto-provision, and none of them is a statement about the role or
// about ownership.
func TestReAddingAMemberKeepsTheirRoleAndOwnership(t *testing.T) {
	st, proj, _ := roleFixture(t)
	ctx := t.Context()

	role := &projmodel.Role{ProjectID: proj, Name: "narrow", Permissions: []string{"emails:read"}}
	if err := st.PutRole(ctx, role); err != nil {
		t.Fatal(err)
	}

	// Two owners, so the demotion guard is not what keeps the bit set.
	addRoleMember(t, st, proj, true)
	user := addRoleMember(t, st, proj, true)
	if ok, err := st.SetMemberRole(ctx, proj, user, role.ID); err != nil || !ok {
		t.Fatalf("assign: ok=%v err=%v", ok, err)
	}

	// An OIDC sign-in re-puts the membership carrying neither.
	if err := st.PutMember(ctx, &projmodel.Member{ProjectID: proj, UserID: user}); err != nil {
		t.Fatal(err)
	}

	m, err := st.GetMember(ctx, proj, user)
	if err != nil {
		t.Fatal(err)
	}

	if !m.HasRole || m.RoleID != role.ID {
		t.Error("re-putting the member cleared their role - the clobber the writer contract exists to stop")
	}

	if !m.Owner {
		t.Error("re-putting the member demoted an owner, which an OIDC sign-in must never do")
	}
}

// The tenancy rule: a role from another project must be
// indistinguishable from one that does not exist.
func TestRolesDoNotCrossProjects(t *testing.T) {
	st, proj, other := roleFixture(t)
	ctx := t.Context()

	foreign := &projmodel.Role{ProjectID: other, Name: "theirs", Permissions: []string{"emails:read"}}
	if err := st.PutRole(ctx, foreign); err != nil {
		t.Fatal(err)
	}

	user := addRoleMember(t, st, proj, false)

	// Assignment of a foreign role affects zero rows.
	ok, err := st.SetMemberRole(ctx, proj, user, foreign.ID)
	if err != nil {
		t.Fatal(err)
	}

	if ok {
		t.Fatal("a member was assigned a role from another project")
	}

	// So does naming one as the project default, which would otherwise
	// leave the project pointing at something it cannot resolve.
	if ok, err := st.SetDefaultRole(ctx, proj, foreign.ID); err != nil || ok {
		t.Fatalf("a foreign role became the project default: ok=%v err=%v", ok, err)
	}

	// And a read scoped to the wrong project finds nothing.
	if r, err := st.GetRole(ctx, proj, foreign.ID); err != nil || r != nil {
		t.Fatalf("cross-project GetRole: r=%v err=%v", r, err)
	}

	// Even a role id written into the column by force resolves to
	// nothing, because the JOIN is tenancy-scoped too.
	if _, err := st.DB().ExecContext(ctx, `
		UPDATE project_members SET role_id = $1 WHERE project_id = $2 AND user_id = $3`,
		foreign.ID, proj, user); err != nil {
		t.Fatal(err)
	}

	m, err := st.GetMember(ctx, proj, user)
	if err != nil {
		t.Fatal(err)
	}

	if m.HasRole {
		t.Fatal("a foreign role resolved through the membership join")
	}
}

// Deleting a role its members carry would move all of them to the
// project default at once, so it must refuse.
func TestDeleteRoleRefusesWhileMembersCarryIt(t *testing.T) {
	st, proj, _ := roleFixture(t)
	ctx := t.Context()

	role := &projmodel.Role{ProjectID: proj, Name: "held", Permissions: []string{"emails:read"}}
	if err := st.PutRole(ctx, role); err != nil {
		t.Fatal(err)
	}

	user := addRoleMember(t, st, proj, false)
	if ok, _ := st.SetMemberRole(ctx, proj, user, role.ID); !ok {
		t.Fatal("assign failed")
	}

	deleted, holding, isDefault, err := st.DeleteRole(ctx, proj, role.ID)
	if err != nil {
		t.Fatal(err)
	}

	if deleted || holding != 1 || isDefault {
		t.Fatalf("deleted=%v holding=%d default=%v - a referenced role was removed",
			deleted, holding, isDefault)
	}

	// Unassign, then the delete goes through.
	if ok, _ := st.SetMemberRole(ctx, proj, user, ""); !ok {
		t.Fatal("clear failed")
	}

	deleted, holding, _, err = st.DeleteRole(ctx, proj, role.ID)
	if err != nil || !deleted || holding != 0 {
		t.Fatalf("unreferenced delete: deleted=%v holding=%d err=%v", deleted, holding, err)
	}
}

// Deleting the DEFAULT role fails differently and worse: the project
// keeps naming a row that is gone, the join reads that as "no role",
// and everybody who never had one of their own silently loses
// everything at once.
func TestDeleteRoleRefusesTheProjectDefault(t *testing.T) {
	st, proj, _ := roleFixture(t)
	ctx := t.Context()

	role := &projmodel.Role{ProjectID: proj, Name: "member", Permissions: []string{"emails:read"}}
	if err := st.PutRole(ctx, role); err != nil {
		t.Fatal(err)
	}

	if ok, _ := st.SetDefaultRole(ctx, proj, role.ID); !ok {
		t.Fatal("set default failed")
	}

	// Nobody carries it explicitly, so only the default guard stands
	// between this role and deletion.
	deleted, holding, isDefault, err := st.DeleteRole(ctx, proj, role.ID)
	if err != nil {
		t.Fatal(err)
	}

	if deleted || !isDefault {
		t.Fatalf("the project default was deleted: deleted=%v default=%v holding=%d",
			deleted, isDefault, holding)
	}

	// Clearing the default releases it.
	if ok, _ := st.SetDefaultRole(ctx, proj, ""); !ok {
		t.Fatal("clear default failed")
	}

	if deleted, _, _, err := st.DeleteRole(ctx, proj, role.ID); err != nil || !deleted {
		t.Fatalf("delete after clearing the default: deleted=%v err=%v", deleted, err)
	}
}

// The member count a role reports includes the people holding it by
// INHERITANCE. Counting only explicit holders would report zero for
// the role most of the project is actually using, and that number is
// what the delete refusal and the console are built on.
func TestRoleMemberCountIncludesInheritedHolders(t *testing.T) {
	st, proj, _ := roleFixture(t)
	ctx := t.Context()

	role := &projmodel.Role{ProjectID: proj, Name: "member", Permissions: []string{"emails:read"}}
	if err := st.PutRole(ctx, role); err != nil {
		t.Fatal(err)
	}

	addRoleMember(t, st, proj, false)
	addRoleMember(t, st, proj, false)
	explicit := addRoleMember(t, st, proj, false)
	if ok, _ := st.SetMemberRole(ctx, proj, explicit, role.ID); !ok {
		t.Fatal("assign failed")
	}

	got, err := st.GetRole(ctx, proj, role.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got.Members != 1 || got.Default {
		t.Fatalf("before it is the default: members=%d default=%v", got.Members, got.Default)
	}

	if ok, _ := st.SetDefaultRole(ctx, proj, role.ID); !ok {
		t.Fatal("set default failed")
	}

	got, err = st.GetRole(ctx, proj, role.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got.Members != 3 || !got.Default {
		t.Fatalf("as the default it should cover all three: members=%d default=%v",
			got.Members, got.Default)
	}
}

// An empty permissions array survives the round trip as '[]', never as
// the empty string the join uses to mean "no role" - a role granting
// nothing is a deliberate lockdown and must not fall through to the
// project default.
func TestAnEmptyRoleIsALockdownNotAnAbsence(t *testing.T) {
	st, proj, _ := roleFixture(t)
	ctx := t.Context()

	open := &projmodel.Role{ProjectID: proj, Name: "member", Permissions: []string{"emails:read"}}
	if err := st.PutRole(ctx, open); err != nil {
		t.Fatal(err)
	}

	if ok, _ := st.SetDefaultRole(ctx, proj, open.ID); !ok {
		t.Fatal("set default failed")
	}

	frozen := &projmodel.Role{ProjectID: proj, Name: "frozen", Permissions: []string{}}
	if err := st.PutRole(ctx, frozen); err != nil {
		t.Fatal(err)
	}

	user := addRoleMember(t, st, proj, false)
	if ok, _ := st.SetMemberRole(ctx, proj, user, frozen.ID); !ok {
		t.Fatal("assign failed")
	}

	m, err := st.GetMember(ctx, proj, user)
	if err != nil {
		t.Fatal(err)
	}

	if !m.HasRole || m.RoleName != "frozen" {
		t.Fatalf("an empty role read back as no role at all, so the member picked up "+
			"the project default instead of being locked down: %+v", m)
	}

	if len(m.RolePermissions) != 0 {
		t.Fatalf("empty role carries %v", m.RolePermissions)
	}
}

// A project with no owner could never be deleted or handed on, and
// nothing in the product can put an owner back. Both paths that could
// produce one refuse inside a single statement, so two people
// resigning at the same moment cannot both win.
func TestAProjectKeepsAtLeastOneOwner(t *testing.T) {
	st, proj, _ := roleFixture(t)
	ctx := t.Context()

	sole := addRoleMember(t, st, proj, true)
	plain := addRoleMember(t, st, proj, false)

	if ok, err := st.SetMemberOwner(ctx, proj, sole, false); err != nil || ok {
		t.Fatalf("the last owner was demoted: ok=%v err=%v", ok, err)
	}

	if ok, err := st.RemoveMember(ctx, proj, sole); err != nil || ok {
		t.Fatalf("the last owner was removed: ok=%v err=%v", ok, err)
	}

	// A second owner releases the first.
	if ok, err := st.SetMemberOwner(ctx, proj, plain, true); err != nil || !ok {
		t.Fatalf("promote: ok=%v err=%v", ok, err)
	}

	if ok, err := st.RemoveMember(ctx, proj, sole); err != nil || !ok {
		t.Fatalf("remove after promoting a second owner: ok=%v err=%v", ok, err)
	}

	// And now that one is the last.
	if ok, _ := st.SetMemberOwner(ctx, proj, plain, false); ok {
		t.Fatal("the remaining owner was demoted")
	}

	// A plain member is removable without any of this.
	other := addRoleMember(t, st, proj, false)
	if ok, err := st.RemoveMember(ctx, proj, other); err != nil || !ok {
		t.Fatalf("removing a non-owner: ok=%v err=%v", ok, err)
	}
}
