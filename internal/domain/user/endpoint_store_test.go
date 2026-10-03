// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package user

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	akmodel "github.com/yousysadmin/mailyard/internal/models/apikey"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
)

// The last enabled administrator survives every verb the handler
// offers: demotion, disabling and deletion, driven through the
// endpoint rather than the store. The store refusing is not enough
// when the handler decides whether to ask it - the demotion path once
// read the admin flag after overwriting it and went through the
// unguarded write.
//
// The caller is a platform API key, which is the caller that is never
// "self" and so reaches every one of these paths.
func TestTheLastAdministratorSurvivesTheHandler(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	st := NewStore(db)
	admin := &usermodel.User{ID: "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1", Email: "admin@example.com", Admin: true}
	if err := st.Put(t.Context(), admin); err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	h := &Handler{Runtime: &env.Runtime{Store: &store.Store{User: st, Project: &fakeMemberships{}}}}
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals(domain.ContextKey, &domain.RequestContext{AdminAPIKey: &akmodel.Admin{ID: "k"}})

		return c.Next()
	})
	app.Patch("/users/:id", h.Update)
	app.Delete("/users/:id", h.Delete)

	call := func(method, body string) int {
		t.Helper()
		req := httptest.NewRequest(method, "/users/"+admin.ID, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		res, err := app.Test(req)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}

		defer func() { _ = res.Body.Close() }()

		return res.StatusCode
	}

	for _, tc := range []struct {
		name, method, body string
	}{
		{"demotion", http.MethodPatch, `{"admin": false}`},
		{"disabling", http.MethodPatch, `{"disabled": true}`},
		{"both at once", http.MethodPatch, `{"admin": false, "disabled": true}`},
		{"deletion", http.MethodDelete, ""},
	} {
		if got := call(tc.method, tc.body); got != http.StatusConflict {
			t.Errorf("%s of the last administrator answered %d, want 409", tc.name, got)
		}

		u, err := st.GetByID(t.Context(), admin.ID)
		if err != nil || u == nil {
			t.Fatalf("%s: administrator gone or unreadable: %v", tc.name, err)
		}

		if !u.Admin || u.Disabled {
			t.Fatalf("%s: administrator row is admin=%v disabled=%v after a refused write", tc.name, u.Admin, u.Disabled)
		}
	}

	// An ordinary edit of the same row still goes through, and once a
	// second administrator exists the first may be demoted and deleted.
	if got := call(http.MethodPatch, `{"email": "root@example.com"}`); got != http.StatusOK {
		t.Errorf("an email change on the last administrator answered %d, want 200", got)
	}

	other := &usermodel.User{ID: "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b2", Email: "b@example.com", Admin: true}
	if err := st.Put(t.Context(), other); err != nil {
		t.Fatalf("seed second admin: %v", err)
	}

	if got := call(http.MethodPatch, `{"admin": false}`); got != http.StatusOK {
		t.Errorf("demotion with another administrator present answered %d, want 200", got)
	}

	if got := call(http.MethodDelete, ""); got != http.StatusNoContent {
		t.Errorf("deletion with another administrator present answered %d, want 204", got)
	}
}

// DeleteKeepingAnAdmin refuses only the last enabled administrator and
// removes anybody else.
func TestDeleteKeepingAnAdminRefusesOnlyTheLastOne(t *testing.T) {
	s := newTestStore(t)
	admin := &usermodel.User{ID: "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b3", Email: "admin@example.com", Admin: true}
	if err := s.Put(t.Context(), admin); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteKeepingAnAdmin(t.Context(), admin.ID); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("deleting the last administrator: err = %v, want ErrLastAdmin", err)
	}

	// The seeded plain user is not an administrator and goes.
	if err := s.DeleteKeepingAnAdmin(t.Context(), "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1"); err != nil {
		t.Fatalf("deleting a plain user: %v", err)
	}

	u, err := s.GetByID(t.Context(), "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1")
	if err != nil || u != nil {
		t.Fatalf("plain user still present after delete: %v, %v", u, err)
	}
}
