// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package project

import (
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

// An invitation is a way a role reaches somebody, so it is bounded by
// what the inviter holds like the other three. AcceptInvitation writes
// the invited role into the membership without asking again.
func TestAnInvitationCannotCarryAWiderRoleThanTheInviter(t *testing.T) {
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

	manager := addRoleMember(t, st, proj, false)
	if ok, err := st.SetMemberRole(ctx, proj, manager, people.ID); err != nil || !ok {
		t.Fatalf("assign role: ok=%v err=%v", ok, err)
	}

	owner := addRoleMember(t, st, proj, true)

	h := &Handler{Runtime: &env.Runtime{Store: &store.Store{Project: st}, Config: &env.Config{}}}
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals(domain.ContextKey, &domain.RequestContext{User: &usermodel.User{ID: c.Get("X-Test-User")}})

		return c.Next()
	})
	app.Post("/projects/:id/invitations", h.CreateInvitation)

	invite := func(caller, roleID string) int {
		t.Helper()
		body := `{"email":"invitee-` + roleID + `@example.invalid","role_id":"` + roleID + `"}`
		req := httptest.NewRequest(fiber.MethodPost, "/projects/"+proj+"/invitations", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-User", caller)
		res, err := app.Test(req)
		if err != nil {
			t.Fatalf("invite: %v", err)
		}

		_ = res.Body.Close()

		return res.StatusCode
	}

	if got := invite(manager, everything.ID); got != fiber.StatusForbidden {
		t.Errorf("a members:write holder invited with a role holding everything: %d, want 403", got)
	}

	if got := invite(manager, people.ID); got != fiber.StatusCreated {
		t.Errorf("a member inviting with their own role was refused: %d, want 201", got)
	}

	if got := invite(owner, everything.ID); got != fiber.StatusCreated {
		t.Errorf("an owner inviting with any role was refused: %d, want 201", got)
	}
}
