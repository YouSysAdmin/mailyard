// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package user

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	akmodel "github.com/yousysadmin/mailyard/internal/models/apikey"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
)

// fakeMemberships answers the membership questions Delete asks.
type fakeMemberships struct {
	store.ProjectStore
	members []*projmodel.Member
}

func (f *fakeMemberships) MembershipsForUser(_ context.Context, userID string) ([]*projmodel.Member, error) {
	var out []*projmodel.Member
	for _, m := range f.members {
		if m.UserID == userID {
			out = append(out, m)
		}
	}

	return out, nil
}

func (f *fakeMemberships) ListMembers(_ context.Context, projID string) ([]*projmodel.Member, error) {
	var out []*projmodel.Member
	for _, m := range f.members {
		if m.ProjectID == projID {
			out = append(out, m)
		}
	}

	return out, nil
}

func (f *fakeMemberships) Get(_ context.Context, id string) (*projmodel.Project, error) {
	return &projmodel.Project{ID: id, Name: "Acme"}, nil
}

type fakeUsers struct {
	store.UserStore
	deleted bool
}

func (f *fakeUsers) GetByID(_ context.Context, id string) (*usermodel.User, error) {
	return &usermodel.User{ID: id, Email: "a@x.test"}, nil
}

func (f *fakeUsers) DeleteKeepingAnAdmin(context.Context, string) error {
	f.deleted = true

	return nil
}

// Deleting the only owner of a project would leave a project nobody can
// give away or shut down, so it is refused until another owner exists.
func TestTheOnlyOwnerOfAProjectIsNotDeleted(t *testing.T) {
	const userID, otherID, projID = "u-1", "u-2", "p-1"
	projects := &fakeMemberships{members: []*projmodel.Member{
		{ProjectID: projID, UserID: userID, Owner: true},
		{ProjectID: projID, UserID: otherID},
	}}
	users := &fakeUsers{}

	h := &Handler{Runtime: &env.Runtime{Store: &store.Store{User: users, Project: projects}}}
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals(domain.ContextKey, &domain.RequestContext{AdminAPIKey: &akmodel.Admin{ID: "k"}})

		return c.Next()
	})
	app.Delete("/users/:id", h.Delete)

	del := func() int {
		res, err := app.Test(httptest.NewRequest(http.MethodDelete, "/users/"+userID, nil))
		if err != nil {
			t.Fatal(err)
		}

		defer func() { _ = res.Body.Close() }()

		return res.StatusCode
	}

	if code := del(); code != http.StatusConflict || users.deleted {
		t.Fatalf("sole owner delete answered %d, deleted=%v, want 409 and kept", code, users.deleted)
	}

	projects.members[1].Owner = true
	if code := del(); code != http.StatusNoContent || !users.deleted {
		t.Errorf("delete with another owner answered %d, deleted=%v", code, users.deleted)
	}
}
