// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package project

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	akmodel "github.com/yousysadmin/mailyard/internal/models/apikey"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
)

// usersByEmail answers UserStore.Get from memory. Every other method
// is unimplemented.
type usersByEmail struct {
	store.UserStore
	all map[string]*usermodel.User
}

func (u usersByEmail) Get(_ context.Context, email string) (*usermodel.User, error) {
	return u.all[email], nil
}

var (
	self     = &usermodel.User{ID: "u-self", Email: "self@example.test"}
	admin    = &usermodel.User{ID: "u-admin", Email: "admin@example.test", Admin: true}
	bob      = &usermodel.User{ID: "u-bob", Email: "bob@example.test"}
	disabled = &usermodel.User{ID: "u-off", Email: "off@example.test", Disabled: true}
)

// WHO OWNS A NEW PROJECT. Only a platform administrator names somebody
// else, a platform credential must name somebody, and a name is an
// existing, enabled account.
func TestTheOwnerOfANewProject(t *testing.T) {
	platformKey := &domain.RequestContext{AdminAPIKey: &akmodel.Admin{Name: "ops"}}
	cases := []struct {
		name     string
		rc       *domain.RequestContext
		email    string
		status   int
		owner    string
		rejected string
	}{
		{"platform key names an account", platformKey, "bob@example.test", 200, "u-bob", ""},
		{"platform key names nobody", platformKey, "", 400, "", "required"},
		{"platform key names an unknown address", platformKey, "ghost@example.test", 400, "", "exists"},
		{"platform key names a disabled account", platformKey, "off@example.test", 400, "", "enabled"},
		{"admin session names nobody", &domain.RequestContext{User: admin}, "", 200, "u-admin", ""},
		{"admin session names an account", &domain.RequestContext{User: admin}, "bob@example.test", 200, "u-bob", ""},
		{"member names nobody", &domain.RequestContext{User: self}, "", 200, "u-self", ""},
		{"member names itself", &domain.RequestContext{User: self}, "self@example.test", 200, "u-self", ""},
		{"member names another account", &domain.RequestContext{User: self}, "bob@example.test", 403, "", ""},
	}

	h := &Handler{Runtime: runtimeWith(t, true, false)}
	h.Runtime.Store = &store.Store{User: usersByEmail{all: map[string]*usermodel.User{
		bob.Email: bob, disabled.Email: disabled,
	}}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var owner *usermodel.User
			app := fiber.New()
			app.Post("/", func(c fiber.Ctx) error {
				u, resp, ok := h.ownerOf(c, tc.rc, tc.email)
				if !ok {
					return resp
				}

				owner = u

				return c.SendStatus(fiber.StatusOK)
			})

			res, err := app.Test(httptest.NewRequest(fiber.MethodPost, "/", strings.NewReader("")))
			if err != nil {
				t.Fatal(err)
			}

			defer func() { _ = res.Body.Close() }()

			if res.StatusCode != tc.status {
				t.Fatalf("status %d, expected %d", res.StatusCode, tc.status)
			}

			if tc.owner != "" && (owner == nil || owner.ID != tc.owner) {
				t.Fatalf("owner %v, expected %s", owner, tc.owner)
			}

			if tc.rejected != "" {
				var body struct {
					Fields []validation.FieldError `json:"fields"`
				}
				if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}

				if len(body.Fields) != 1 || body.Fields[0].Field != "owner_email" || body.Fields[0].Rule != tc.rejected {
					t.Fatalf("fields %+v, expected owner_email/%s", body.Fields, tc.rejected)
				}
			}
		})
	}
}

// A platform credential may create a project whatever the setting
// says. A project key, which also has no user, still may not.
func TestAPlatformCredentialCreatesProjects(t *testing.T) {
	rt := runtimeWith(t, false, false)

	if !MayCreate(rt, &domain.RequestContext{AdminAPIKey: &akmodel.Admin{Name: "ops"}}) {
		t.Error("a platform credential was refused")
	}

	if MayCreate(rt, &domain.RequestContext{APIKey: &akmodel.Key{Name: "app"}}) {
		t.Error("a project key may create a project")
	}
}
