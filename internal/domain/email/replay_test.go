// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	akmodel "github.com/yousysadmin/mailyard/internal/models/apikey"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
	perm "github.com/yousysadmin/mailyard/internal/models/permission"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
	sbmodel "github.com/yousysadmin/mailyard/internal/models/sandbox"
)

type replayEmails struct {
	store.EmailStore
	held emailmodel.KeyHolder
}

func (f replayEmails) ReserveKey(context.Context, string, string) (emailmodel.KeyHolder, bool, error) {
	return f.held, false, nil
}

func (f replayEmails) Get(_ context.Context, _, id string) (*emailmodel.Email, error) {
	return &emailmodel.Email{ID: id, Subject: "SECRET-SUBJECT"}, nil
}

type replaySandbox struct{ store.SandboxStore }

func (replaySandbox) Get(_ context.Context, _, id string) (*sbmodel.Email, error) {
	return &sbmodel.Email{ID: id, Subject: "SECRET-SUBJECT"}, nil
}

// A replay hands back what the key produced only to a caller allowed to
// read it, since the ledger is shared by every credential in a project.
func TestAReplayIsOnlyHandedToACallerWhoMayReadIt(t *testing.T) {
	const real, capture = "0198f6a1-3c7e-7b21-9f4d-2a5c8e0b1d33", "0198f6a1-3c7e-7b21-9f4d-2a5c8e0b1d34"

	for _, tc := range []struct {
		name    string
		held    emailmodel.KeyHolder
		sandbox bool
		perms   []string
		want    int
	}{
		{"a sandbox key replaying a real message", emailmodel.KeyHolder{EmailID: real}, true, nil, http.StatusConflict},
		{"a sandbox key granted emails:read", emailmodel.KeyHolder{EmailID: real}, true, []string{"emails:read"}, http.StatusConflict},
		{"a writer without emails:read", emailmodel.KeyHolder{EmailID: real}, false, []string{"emails:write"}, http.StatusConflict},
		{"a reader of emails", emailmodel.KeyHolder{EmailID: real}, false, []string{"emails:read"}, http.StatusOK},
		{"a capture without sandbox:read", emailmodel.KeyHolder{SandboxEmailID: capture}, false, []string{"sandbox:write"}, http.StatusConflict},
		{"a sandbox key replaying its capture", emailmodel.KeyHolder{SandboxEmailID: capture}, true, nil, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &Handler{Runtime: &env.Runtime{Store: &store.Store{
				Email:   replayEmails{held: tc.held},
				Sandbox: replaySandbox{},
			}}}

			rc := &domain.RequestContext{
				Project:     &projmodel.Project{ID: "proj-a"},
				APIKey:      &akmodel.Key{Sandbox: tc.sandbox},
				Permissions: perm.ForKey(tc.perms, tc.sandbox),
			}

			app := fiber.New()
			app.Post("/", func(c fiber.Ctx) error {
				c.Locals(domain.ContextKey, rc)

				return h.sendOnce(c, rc, func() (*emailmodel.Email, []string, error) {
					t.Fatal("a held key ran the send")

					return nil, nil, nil
				})
			})

			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.Header.Set("Idempotency-Key", "order-1")

			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}

			defer func() { _ = resp.Body.Close() }()

			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != tc.want {
				t.Fatalf("status %d, want %d: %s", resp.StatusCode, tc.want, body)
			}

			if tc.want != http.StatusOK && strings.Contains(string(body), "SECRET-SUBJECT") {
				t.Errorf("a refused replay carried the record: %s", body)
			}
		})
	}
}
