// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package project

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
)

// fakeUsers answers the one lookup AddMember makes.
type fakeUsers struct {
	store.UserStore
	byEmail map[string]*usermodel.User
}

func (f *fakeUsers) Get(_ context.Context, email string) (*usermodel.User, error) {
	return f.byEmail[email], nil
}

// No role means the default role, so clearing a role, adding a member
// and inviting without one are bounded by what that default grants.
func TestNoRoleIsCheckedAsTheDefaultRole(t *testing.T) {
	st, proj, _ := roleFixture(t)
	ctx := t.Context()

	people := &projmodel.Role{ProjectID: proj, Name: "people",
		Permissions: []string{"members:read", "members:write"}}
	everything := &projmodel.Role{ProjectID: proj, Name: "everything", Permissions: everyPermission()}
	for _, r := range []*projmodel.Role{people, everything} {
		if err := st.PutRole(ctx, r); err != nil {
			t.Fatalf("put role %s: %v", r.Name, err)
		}
	}

	if ok, err := st.SetDefaultRole(ctx, proj, everything.ID); err != nil || !ok {
		t.Fatalf("set default: ok=%v err=%v", ok, err)
	}

	manager := addRoleMember(t, st, proj, false)
	if ok, err := st.SetMemberRole(ctx, proj, manager, people.ID); err != nil || !ok {
		t.Fatalf("assign role: ok=%v err=%v", ok, err)
	}

	owner := addRoleMember(t, st, proj, true)
	outsider := addRoleMember(t, st, proj, false)
	if _, err := st.RemoveMember(ctx, proj, outsider); err != nil {
		t.Fatalf("remove: %v", err)
	}

	users := &fakeUsers{byEmail: map[string]*usermodel.User{
		outsider + "@example.invalid": {ID: outsider, Email: outsider + "@example.invalid"},
	}}
	h := &Handler{Runtime: &env.Runtime{Store: &store.Store{Project: st, User: users}, Config: &env.Config{}}}
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals(domain.ContextKey, &domain.RequestContext{User: &usermodel.User{ID: c.Get("X-Test-User")}})

		return c.Next()
	})
	app.Patch("/projects/:id/members/:userId", h.UpdateMember)
	app.Post("/projects/:id/members", h.AddMember)
	app.Post("/projects/:id/invitations", h.CreateInvitation)

	call := func(method, path, caller, body string) int {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-User", caller)
		res, err := app.Test(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}

		_ = res.Body.Close()

		return res.StatusCode
	}

	base := "/projects/" + proj
	if got := call(fiber.MethodPatch, base+"/members/"+manager, manager, `{"role_id":""}`); got != fiber.StatusForbidden {
		t.Errorf("clearing own role onto a wider default: %d, want 403", got)
	}

	m, err := st.GetMember(ctx, proj, manager)
	if err != nil || m == nil || m.RoleID != people.ID {
		t.Fatalf("the refused clear changed the role: %+v %v", m, err)
	}

	addBody := `{"email":"` + outsider + `@example.invalid"}`
	if got := call(fiber.MethodPost, base+"/members", manager, addBody); got != fiber.StatusForbidden {
		t.Errorf("adding a member onto a wider default: %d, want 403", got)
	}

	if got := call(fiber.MethodPost, base+"/invitations", manager, `{"email":"x@example.invalid"}`); got != fiber.StatusForbidden {
		t.Errorf("inviting onto a wider default: %d, want 403", got)
	}

	if got := call(fiber.MethodPost, base+"/invitations", owner, `{"email":"y@example.invalid"}`); got != fiber.StatusCreated {
		t.Errorf("an owner inviting without a role: %d, want 201", got)
	}
}

// A refused ownership change writes nothing, the role included.
func TestARefusedOwnershipChangeKeepsTheRole(t *testing.T) {
	st, proj, _ := roleFixture(t)
	ctx := t.Context()

	people := &projmodel.Role{ProjectID: proj, Name: "people", Permissions: []string{"members:read"}}
	if err := st.PutRole(ctx, people); err != nil {
		t.Fatalf("put role: %v", err)
	}

	owner := addRoleMember(t, st, proj, true)

	h := &Handler{Runtime: &env.Runtime{Store: &store.Store{Project: st}, Config: &env.Config{}}}
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals(domain.ContextKey, &domain.RequestContext{User: &usermodel.User{ID: owner}})

		return c.Next()
	})
	app.Patch("/projects/:id/members/:userId", h.UpdateMember)

	body := `{"owner":false,"role_id":"` + people.ID + `"}`
	req := httptest.NewRequest(fiber.MethodPatch, "/projects/"+proj+"/members/"+owner, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	if res.StatusCode != fiber.StatusConflict {
		t.Fatalf("demoting the last owner: %d, want 409", res.StatusCode)
	}

	m, err := st.GetMember(ctx, proj, owner)
	if err != nil || m == nil || m.RoleID != "" || !m.Owner {
		t.Fatalf("the refused request wrote something: %+v %v", m, err)
	}
}

// PATCH on a role changes what it names and keeps the rest.
func TestARolePatchKeepsTheFieldsItDoesNotName(t *testing.T) {
	st, proj, _ := roleFixture(t)
	ctx := t.Context()

	role := &projmodel.Role{ProjectID: proj, Name: "support", Description: "first line",
		Permissions: []string{"emails:read"}}
	if err := st.PutRole(ctx, role); err != nil {
		t.Fatalf("put role: %v", err)
	}

	owner := addRoleMember(t, st, proj, true)

	h := &Handler{Runtime: &env.Runtime{Store: &store.Store{Project: st}, Config: &env.Config{}}}
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals(domain.ContextKey, &domain.RequestContext{User: &usermodel.User{ID: owner}})

		return c.Next()
	})
	app.Patch("/projects/:id/roles/:roleId", h.UpdateRole)

	req := httptest.NewRequest(fiber.MethodPatch, "/projects/"+proj+"/roles/"+role.ID, strings.NewReader(`{"name":"helpdesk"}`))
	req.Header.Set("Content-Type", "application/json")
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	if res.StatusCode != fiber.StatusOK {
		t.Fatalf("rename only: %d, want 200", res.StatusCode)
	}

	got, err := st.GetRole(ctx, proj, role.ID)
	if err != nil || got == nil {
		t.Fatalf("get role: %v", err)
	}

	if got.Name != "helpdesk" || got.Description != "first line" || len(got.Permissions) != 1 {
		t.Fatalf("role after rename: %+v", got)
	}
}
