// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package project

import (
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
)

func roundTripStore(t *testing.T) *Store {
	t.Helper()
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	dbtest.Schema(t, db, `
        INSERT INTO users (id, email, account_type, created_at)
        VALUES ('1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1', 'owner@example.com', 1, now())`)

	return &Store{Base: database.NewBase(db)}
}

func distinctProject(id, slug string) *projmodel.Project {
	return &projmodel.Project{
		ID:              id,
		Name:            "Distinctive " + slug,
		Slug:            slug,
		Description:     "a description",
		OwnerID:         "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1",
		DefaultLanguage: "uk",
		PlanID:          "5f24f95e-0d4b-44d4-812b-060064ccb204",
		DefaultRoleID:   "3c4ea422-5533-411d-8baf-2762645d07c3",
		StrictSenders:   true,
		TrackOpens:      true,
		TrackClicks:     true,
		CreatedAt:       time.Now().UTC().Truncate(time.Millisecond),
	}
}

func assertSurvived(t *testing.T, want, got *projmodel.Project) {
	t.Helper()
	if got == nil {
		t.Fatal("the row came back nil")
	}

	// Field by field, so a failure names the column that drifted.
	for _, c := range []struct {
		field     string
		want, got any
	}{
		{"name", want.Name, got.Name},
		{"slug", want.Slug, got.Slug},
		{"description", want.Description, got.Description},
		{"owner_id", want.OwnerID, got.OwnerID},
		{"default_language", want.DefaultLanguage, got.DefaultLanguage},
		{"plan_id", want.PlanID, got.PlanID},
		{"default_role_id", want.DefaultRoleID, got.DefaultRoleID},
		{"strict_senders", want.StrictSenders, got.StrictSenders},
		{"track_opens", want.TrackOpens, got.TrackOpens},
		{"track_clicks", want.TrackClicks, got.TrackClicks},
	} {
		if c.got != c.want {
			t.Errorf("%s came back %v, want %v", c.field, c.got, c.want)
		}
	}
}

// Write then read, with every field set to something distinctive.
//
// The projects table is written by two hand-written INSERTs with
// hand-written placeholder lists and read by a hand-written SELECT
// scanned positionally. Removing bounce_address broke one of the
// INSERTs - the column came out of the list while its placeholder
// stayed - and every existing test still passed, because none of them
// went through CreateWithOwner. The failure surfaced as a server that
// would not finish bootstrapping.
//
// emails has carried this guard since the third time the same thing
// happened to it. This is the same guard for projects.
func TestProjectSurvivesARoundTrip(t *testing.T) {
	s := roundTripStore(t)
	want := distinctProject("e66e7a4d-9e6c-4884-869a-cf9ffcf22181", "distinctive")

	if err := s.Put(t.Context(), want); err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := s.Get(t.Context(), "e66e7a4d-9e6c-4884-869a-cf9ffcf22181")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	assertSurvived(t, want, got)
}

// CreateWithOwner is a second INSERT over the same columns, and it is
// the one bootstrap uses - so a mismatch here stops the server coming
// up at all rather than failing some later request.
func TestCreateWithOwnerSurvivesARoundTrip(t *testing.T) {
	s := roundTripStore(t)
	want := distinctProject("6a5f0b90-6a56-47f4-8926-7cc56968798b", "with-owner")

	if err := s.CreateWithOwner(t.Context(), want, "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1"); err != nil {
		t.Fatalf("create with owner: %v", err)
	}

	got, err := s.Get(t.Context(), "6a5f0b90-6a56-47f4-8926-7cc56968798b")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	assertSurvived(t, want, got)

	// And the owner membership the same statement writes.
	members, err := s.ListMembers(t.Context(), "6a5f0b90-6a56-47f4-8926-7cc56968798b")
	if err != nil {
		t.Fatalf("list members: %v", err)
	}

	if len(members) != 1 || members[0].UserID != "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1" || !members[0].Owner {
		t.Errorf("owner membership is %+v", members)
	}
}

// Every READER of the projects table, not just Get.
//
// wsSelect is one hand-written column list scanned positionally, and
// for a while ListForUser carried a second copy of it, aliased for a
// JOIN. That copy went stale the moment the table gained a column -
// "expected 14 destination arguments in Scan, not 15" - and no test
// caught it, because none of them listed a project by user. The
// duplicate is gone, and this makes sure a new reader cannot
// reintroduce one quietly.
func TestEveryProjectReaderScansTheSameColumns(t *testing.T) {
	s := roundTripStore(t)
	want := distinctProject("601da9d8-a9cd-49ad-8ca1-7998351403ef", "every-reader")
	if err := s.CreateWithOwner(t.Context(), want, "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1"); err != nil {
		t.Fatalf("create: %v", err)
	}

	// CreateWithOwner does not carry default_role_id through the
	// membership path, so set it the way the API does.
	if _, err := s.SetDefaultRole(t.Context(), "601da9d8-a9cd-49ad-8ca1-7998351403ef", ""); err != nil {
		t.Fatalf("set default role: %v", err)
	}

	want.DefaultRoleID = ""

	byID, err := s.Get(t.Context(), "601da9d8-a9cd-49ad-8ca1-7998351403ef")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	assertSurvived(t, want, byID)

	bySlug, err := s.GetBySlug(t.Context(), "every-reader")
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}

	assertSurvived(t, want, bySlug)

	all, err := s.List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	assertSurvived(t, want, findProject(t, all, "601da9d8-a9cd-49ad-8ca1-7998351403ef"))

	mine, err := s.ListForUser(t.Context(), "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1")
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}

	assertSurvived(t, want, findProject(t, mine, "601da9d8-a9cd-49ad-8ca1-7998351403ef"))

}

func findProject(t *testing.T, list []*projmodel.Project, id string) *projmodel.Project {
	t.Helper()
	for _, w := range list {
		if w.ID == id {
			return w
		}
	}

	t.Fatalf("project %s missing from a list of %d", id, len(list))

	return nil
}
