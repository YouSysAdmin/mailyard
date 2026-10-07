// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package domains

import (
	"bytes"
	"encoding/json/v2"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/plan"
	"github.com/yousysadmin/mailyard/internal/domain/project"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	dmodel "github.com/yousysadmin/mailyard/internal/models/domain"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
)

const (
	ownerProj   = "e66e7a4d-9e6c-4884-869a-cf9ffcf22181"
	granteeProj = "6a5f0b90-6a56-47f4-8926-7cc56968798b"
	thirdProj   = "0b1c6a2e-4f3d-4a8e-9a77-2f0c9e5d1b11"
	apexID      = "504a0295-c50b-4e67-82c9-e916c01ecbd0"
	childID     = "abb2e27c-6e7d-4568-8927-77d45ddf88df"
)

func grantStore(t *testing.T) (*Store, *project.Store) {
	t.Helper()
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	dbtest.Schema(t, db, `
        INSERT INTO projects (id, name, slug, default_language, created_at)
        VALUES ('`+ownerProj+`', 'Main', 'main', 'en', now()),
               ('`+granteeProj+`', 'Auth', 'auth', 'en', now()),
               ('`+thirdProj+`', 'Other', 'other', 'en', now())`)

	return NewStore(db, nil), project.NewStore(db)
}

// A grant lets the grantee send as the owner's domain and anything
// under it, and nobody else. The child a third project holds here is
// seeded straight into the table - the API refuses it now - which is
// the shape an installation from before the zone rule can still have:
// it stays that project's, and the grant on the parent does not reach
// into it.
func TestAGrantLetsAnotherProjectSendAsTheDomain(t *testing.T) {
	s, _ := grantStore(t)
	ctx := t.Context()
	seedDomain(t, s, apexID, ownerProj, "example.com", true)
	seedDomain(t, s, childID, thirdProj, "mail.example.com", true)

	for_ := func(name, projID string) *dmodel.Domain {
		t.Helper()
		d, err := s.GetVerifiedCoveringFor(ctx, name, projID)
		if err != nil {
			t.Fatal(err)
		}

		return d
	}

	if for_("example.com", ownerProj) == nil {
		t.Fatal("the owner may not send as its own domain")
	}

	if for_("example.com", granteeProj) != nil {
		t.Fatal("a project sends as another's domain before it was shared")
	}

	if err := s.Grant(ctx, &dmodel.Grant{DomainID: apexID, ProjectID: granteeProj, GrantedBy: "a@b.test"}); err != nil {
		t.Fatal(err)
	}

	// An offer covers nothing until the other project accepts it.
	if for_("example.com", granteeProj) != nil {
		t.Fatal("a project sends as another's domain before it accepted the share")
	}

	if ok, err := s.AcceptGrant(ctx, apexID, thirdProj); err != nil || ok {
		t.Fatalf("a project with no offer accepted one: %v %v", ok, err)
	}

	if ok, err := s.AcceptGrant(ctx, apexID, granteeProj); err != nil || !ok {
		t.Fatalf("accept: %v %v", ok, err)
	}

	// A repeat is a no-op, not a conflict, and does not undo the acceptance.
	if err := s.Grant(ctx, &dmodel.Grant{DomainID: apexID, ProjectID: granteeProj}); err != nil {
		t.Fatalf("a repeat grant: %v", err)
	}

	if d := for_("auth.example.com", granteeProj); d == nil || d.ID != apexID {
		t.Fatalf("grantee on a subdomain = %+v, want the owner's row", d)
	}

	if for_("example.com", thirdProj) != nil {
		t.Fatal("a project the domain was not shared with may send as it")
	}

	if for_("x.mail.example.com", granteeProj) != nil {
		t.Fatal("the grant on the parent reached into a child another project verified")
	}

	grants, err := s.ListGrants(ctx, ownerProj, apexID)
	if err != nil || len(grants) != 1 || grants[0].ProjectName != "Auth" || grants[0].ProjectSlug != "auth" {
		t.Fatalf("ListGrants = %+v, %v, want the one grant with the grantee's name and slug", grants, err)
	}

	// Only the owner reads the grants.
	if other, _ := s.ListGrants(ctx, granteeProj, apexID); len(other) != 0 {
		t.Fatal("the grantee could list the owner's grants")
	}

	shared, err := s.ListShared(ctx, granteeProj)
	if err != nil || len(shared) != 1 || shared[0].Domain != "example.com" || shared[0].OwnerName != "Main" {
		t.Fatalf("ListShared = %+v, %v, want example.com from Main", shared, err)
	}

	// The owner's own subdomain is covered by the grant on its apex,
	// and it signs with the subdomain's own row.
	const ownSubID = "7d1f3b2a-9c4e-4f6a-8b1d-3e5c7a9b0f12"
	seedDomain(t, s, ownSubID, ownerProj, "auth.example.com", true)
	if d := for_("login.auth.example.com", granteeProj); d == nil || d.ID != ownSubID {
		t.Fatalf("grantee on the owner's own subdomain = %+v, want that subdomain's row", d)
	}

	if gone, err := s.Revoke(ctx, apexID, granteeProj); err != nil || !gone {
		t.Fatalf("Revoke = %v, %v", gone, err)
	}

	if gone, _ := s.Revoke(ctx, apexID, granteeProj); gone {
		t.Fatal("a second revoke reported a grant")
	}

	if for_("example.com", granteeProj) != nil {
		t.Fatal("the grantee still sends as the domain after the revoke")
	}

	// Deleting the domain takes every grant with it.
	if err := s.Grant(ctx, &dmodel.Grant{DomainID: apexID, ProjectID: granteeProj}); err != nil {
		t.Fatal(err)
	}

	if err := s.Delete(ctx, ownerProj, apexID); err != nil {
		t.Fatal(err)
	}

	if left, _ := s.ListShared(ctx, granteeProj); len(left) != 0 {
		t.Fatalf("a grant outlived its domain: %+v", left)
	}
}

// The owner shares by slug and is refused for everything a grant
// could not honour. Nothing here answers for a domain the caller does
// not own.
func TestTheGrantsEndpoint(t *testing.T) {
	s, ps := grantStore(t)
	seedDomain(t, s, apexID, ownerProj, "example.com", true)
	seedDomain(t, s, childID, ownerProj, "pending.test", false)

	h := &Handler{Runtime: &env.Runtime{Store: &store.Store{Domain: s, Project: ps}}}
	app := func(projID string) *fiber.App {
		a := fiber.New()
		a.Use(func(c fiber.Ctx) error {
			c.Locals(domain.ContextKey, &domain.RequestContext{Project: &projmodel.Project{ID: projID}})

			return c.Next()
		})
		a.Get("/domains", h.List)
		a.Get("/domains/:id/grants", h.Grants)
		a.Post("/domains/:id/grants", h.Share)
		a.Delete("/domains/:id/grants/:project_id", h.Unshare)
		a.Post("/domains/:id/shares/accept", h.AcceptShare)
		a.Delete("/domains/:id/shares", h.LeaveShare)

		return a
	}

	call := func(a *fiber.App, method, path, body string) (int, string) {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		res, err := a.Test(req)
		if err != nil {
			t.Fatal(err)
		}

		b, _ := io.ReadAll(res.Body)

		return res.StatusCode, string(b)
	}

	owner, grantee := app(ownerProj), app(granteeProj)
	cases := []struct {
		name, path, slug string
		want             int
	}{
		{"own project", "/domains/" + apexID + "/grants", "main", 400},
		{"unknown slug", "/domains/" + apexID + "/grants", "nope", 404},
		{"unverified domain", "/domains/" + childID + "/grants", "auth", 400},
		{"shared", "/domains/" + apexID + "/grants", "auth", 200},
		{"shared again", "/domains/" + apexID + "/grants", "auth", 200},
	}
	for _, tc := range cases {
		if code, body := call(owner, "POST", tc.path, `{"project_slug":"`+tc.slug+`"}`); code != tc.want {
			t.Errorf("%s: %d %s, want %d", tc.name, code, body, tc.want)
		}
	}

	// The grantee cannot see or touch the owner's grants.
	if code, _ := call(grantee, "POST", "/domains/"+apexID+"/grants", `{"project_slug":"other"}`); code != 404 {
		t.Errorf("the grantee shared the owner's domain onward: %d", code)
	}

	if code, _ := call(grantee, "GET", "/domains/"+apexID+"/grants", ""); code != 404 {
		t.Errorf("the grantee listed the owner's grants: %d", code)
	}

	// Pending: the owner sees the slug it typed and no name, the
	// grantee sees the offer.
	_, body := call(owner, "GET", "/domains/"+apexID+"/grants", "")
	var gr GrantsResponse
	if err := json.Unmarshal([]byte(body), &gr); err != nil || len(gr.Grants) != 1 ||
		gr.Grants[0].Status != dmodel.GrantPending || gr.Grants[0].ProjectName != "" || gr.Grants[0].ProjectSlug != "auth" {
		t.Fatalf("grants = %s, want one pending for the slug auth with no name", body)
	}

	_, body = call(grantee, "GET", "/domains", "")
	var lr ListResponse
	if err := json.Unmarshal([]byte(body), &lr); err != nil || len(lr.Domains) != 0 || len(lr.Shared) != 1 ||
		lr.Shared[0].OwnerName != "Main" || lr.Shared[0].Status != dmodel.GrantPending {
		t.Fatalf("the grantee's list = %s, want no domains of its own and example.com offered by Main", body)
	}

	// Only the project the offer names can answer it, and a stranger
	// gets the same answer as for a domain that was never shared.
	third := app(thirdProj)
	if code, _ := call(third, "POST", "/domains/"+apexID+"/shares/accept", ""); code != 404 {
		t.Errorf("a stranger accepted an offer not made to it: %d", code)
	}

	if code, _ := call(third, "DELETE", "/domains/"+apexID+"/shares", ""); code != 404 {
		t.Errorf("a stranger declined an offer not made to it: %d", code)
	}

	if code, _ := call(grantee, "POST", "/domains/"+childID+"/shares/accept", ""); code != 404 {
		t.Errorf("accepting a domain never offered: %d", code)
	}

	code, body := call(grantee, "POST", "/domains/"+apexID+"/shares/accept", "")
	var sr ShareResponse
	if err := json.Unmarshal([]byte(body), &sr); code != 200 || err != nil || sr.Shared == nil ||
		sr.Shared.Status != dmodel.GrantAccepted || sr.Shared.AcceptedAt == nil {
		t.Fatalf("accept = %d %s, want the share accepted", code, body)
	}

	// Accepted: the name is told, and the owner's list says so.
	_, body = call(owner, "GET", "/domains/"+apexID+"/grants", "")
	if err := json.Unmarshal([]byte(body), &gr); err != nil || len(gr.Grants) != 1 ||
		gr.Grants[0].Status != dmodel.GrantAccepted || gr.Grants[0].ProjectName != "Auth" {
		t.Fatalf("grants = %s, want one accepted for Auth", body)
	}

	if code, _ := call(grantee, "POST", "/domains/"+apexID+"/shares/accept", ""); code != 200 {
		t.Errorf("accepting twice: %d, want 200", code)
	}

	// The grantee can give it back, after which the owner has nothing to revoke.
	if code, _ := call(grantee, "DELETE", "/domains/"+apexID+"/shares", ""); code != 204 {
		t.Errorf("leave: %d, want 204", code)
	}

	if code, _ := call(owner, "DELETE", "/domains/"+apexID+"/grants/"+granteeProj, ""); code != 404 {
		t.Errorf("revoke after the grantee left: %d, want 404", code)
	}

	// Offered again and withdrawn by the owner before an answer.
	if code, _ := call(owner, "POST", "/domains/"+apexID+"/grants", `{"project_slug":"auth"}`); code != 200 {
		t.Errorf("share again: %d", code)
	}

	if code, _ := call(owner, "DELETE", "/domains/"+apexID+"/grants/"+granteeProj, ""); code != 204 {
		t.Errorf("revoke: %d, want 204", code)
	}

	if code, _ := call(owner, "DELETE", "/domains/"+apexID+"/grants/"+granteeProj, ""); code != 404 {
		t.Errorf("a second revoke: %d, want 404", code)
	}

	_, body = call(owner, "GET", "/domains", "")
	if err := json.Unmarshal([]byte(body), &lr); err != nil || lr.Shared == nil {
		t.Fatalf("the owner's list = %s, want shared as an array, never null", body)
	}
}

// A verified domain holds its zone, in both directions: nobody else
// claims a name under it, and nobody verifies a name above one another
// project holds. The project's own names are never in its way.
func TestAVerifiedDomainHoldsItsZone(t *testing.T) {
	s, _ := grantStore(t)
	ctx := t.Context()
	seedDomain(t, s, apexID, ownerProj, "example.com", true)
	seedDomain(t, s, childID, thirdProj, "deep.other.org", true)

	cases := []struct {
		name, projID string
		want         bool
	}{
		{"auth.example.com", granteeProj, true},
		{"a.b.example.com", granteeProj, true},
		{"example.com", granteeProj, true},
		{"auth.example.com", ownerProj, false},
		{"other.org", granteeProj, true},
		{"other.org", thirdProj, false},
		{"evilexample.com", granteeProj, false},
		{"xexample.com", granteeProj, false},
		{"unrelated.net", granteeProj, false},
	}
	for _, tc := range cases {
		got, err := s.ZoneTakenByAnother(ctx, tc.name, tc.projID)
		if err != nil {
			t.Fatal(err)
		}

		if got != tc.want {
			t.Errorf("ZoneTakenByAnother(%s, %s) = %v, want %v", tc.name, tc.projID, got, tc.want)
		}
	}

	// An unverified claim holds nothing.
	seedDomain(t, s, "1f2e3d4c-5b6a-4978-8a9b-0c1d2e3f4a5b", ownerProj, "pending.io", false)
	if got, _ := s.ZoneTakenByAnother(ctx, "mail.pending.io", granteeProj); got {
		t.Error("an unverified claim took its zone")
	}
}

// The API applies it: a name inside or above another project's zone is
// refused at the claim, and the owner's own subdomain is not.
func TestClaimingInsideAnotherProjectsZoneIsRefused(t *testing.T) {
	s, ps := grantStore(t)
	seedDomain(t, s, apexID, ownerProj, "example.com", true)
	seedDomain(t, s, childID, thirdProj, "mail.other.org", true)

	h := &Handler{Runtime: &env.Runtime{
		Config: &env.Config{},
		Store:  &store.Store{Domain: s, Project: ps, Plan: plan.NewStore(s.DB())},
	}}
	call := func(projID, name string) int {
		t.Helper()
		a := fiber.New()
		a.Use(func(c fiber.Ctx) error {
			c.Locals(domain.ContextKey, &domain.RequestContext{Project: &projmodel.Project{ID: projID}})

			return c.Next()
		})
		a.Post("/domains", h.Create)
		req := httptest.NewRequest("POST", "/domains", bytes.NewBufferString(`{"domain":"`+name+`"}`))
		req.Header.Set("Content-Type", "application/json")
		res, err := a.Test(req)
		if err != nil {
			t.Fatal(err)
		}

		return res.StatusCode
	}

	if code := call(granteeProj, "auth.example.com"); code != 409 {
		t.Errorf("a subdomain of another project's domain: %d, want 409", code)
	}

	if code := call(granteeProj, "other.org"); code != 409 {
		t.Errorf("the parent of another project's domain: %d, want 409", code)
	}

	if code := call(ownerProj, "auth.example.com"); code != 201 {
		t.Errorf("the owner's own subdomain: %d, want 201", code)
	}
}
