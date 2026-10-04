// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package data

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
)

// erasureCalls records which erasure each fake store was asked for.
type erasureCalls struct {
	calls []string
}

type fakeContacts struct {
	store.ContactStore
	rec *erasureCalls
}

func (f fakeContacts) PurgeAll(context.Context, string) (int64, error) {
	f.rec.calls = append(f.rec.calls, "contacts:all")

	return 3, nil
}

func (f fakeContacts) PurgeForEmail(_ context.Context, _, email string) (int64, error) {
	f.rec.calls = append(f.rec.calls, "contacts:"+email)

	return 1, nil
}

type fakeSuppressions struct {
	store.SuppressionStore
	rec *erasureCalls
}

func (f fakeSuppressions) PurgeForAddress(_ context.Context, _, email string) (int64, error) {
	f.rec.calls = append(f.rec.calls, "suppressions:"+email)

	return 1, nil
}

type fakeEmails struct {
	store.EmailStore
	rec *erasureCalls
}

func (f fakeEmails) StorageKeysForAddress(context.Context, string, string) ([]string, error) {
	return nil, nil
}

func (f fakeEmails) StorageKeysForProjectOlderThan(context.Context, string, time.Time) ([]string, error) {
	return nil, nil
}

func (f fakeEmails) PurgeForAddress(_ context.Context, _, email string) (int64, error) {
	f.rec.calls = append(f.rec.calls, "emails:"+email)

	return 1, nil
}

func (f fakeEmails) PurgeProjectOlderThan(_ context.Context, _ string, before time.Time) (int64, error) {
	if time.Since(before) > time.Hour {
		f.rec.calls = append(f.rec.calls, "emails:older")
	} else {
		f.rec.calls = append(f.rec.calls, "emails:all")
	}

	return 5, nil
}

func erasureApp(rec *erasureCalls) *fiber.App {
	h := &Handler{Runtime: &env.Runtime{Store: &store.Store{
		Contact:     fakeContacts{rec: rec},
		Suppression: fakeSuppressions{rec: rec},
		Email:       fakeEmails{rec: rec},
	}}}
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals(domain.ContextKey, &domain.RequestContext{Ctx: c, Project: &projmodel.Project{ID: "p1"}})

		return c.Next()
	})
	app.Delete("/contacts", h.DeleteContacts)
	app.Delete("/emails", h.DeleteEmailLogs)

	return app
}

// An empty erasure is a mistake, not consent: everything is erased only
// when confirm_all says so, and an address narrows the erasure to it.
func TestAnErasureOfEverythingNeedsConfirmAll(t *testing.T) {
	cases := []struct {
		name, path, body string
		status           int
		calls            string
	}{
		{"contacts, empty body", "/contacts", `{}`, 400, ""},
		{"contacts, confirm false", "/contacts", `{"confirm_all": false}`, 400, ""},
		{"contacts, confirm all", "/contacts", `{"confirm_all": true}`, 200, "contacts:all"},
		{"contacts, one address", "/contacts", `{"email": "Ada@X.test"}`, 200,
			"contacts:ada@x.test suppressions:ada@x.test"},
		{"contacts, an address wins over confirm", "/contacts", `{"email": "a@x.test", "confirm_all": true}`, 200,
			"contacts:a@x.test suppressions:a@x.test"},

		{"emails, empty body", "/emails", `{}`, 400, ""},
		{"emails, confirm all", "/emails", `{"confirm_all": true}`, 200, "emails:all"},
		{"emails, older than", "/emails", `{"older_than_days": 30}`, 200, "emails:older"},
		{"emails, one address", "/emails", `{"email": "Ada@X.test"}`, 200, "emails:ada@x.test"},
	}
	for _, tc := range cases {
		rec := &erasureCalls{}
		req := httptest.NewRequest(fiber.MethodDelete, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := erasureApp(rec).Test(req)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}

		_ = resp.Body.Close()
		if resp.StatusCode != tc.status {
			t.Errorf("%s: status %d, want %d", tc.name, resp.StatusCode, tc.status)
		}

		if got := strings.Join(rec.calls, " "); got != tc.calls {
			t.Errorf("%s: erased %q, want %q", tc.name, got, tc.calls)
		}
	}
}
