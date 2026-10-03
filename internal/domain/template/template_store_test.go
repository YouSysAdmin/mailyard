// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package template

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	sheetmodel "github.com/yousysadmin/mailyard/internal/models/stylesheet"
	tmodel "github.com/yousysadmin/mailyard/internal/models/template"
)

const projID = "e66e7a4d-9e6c-4884-869a-cf9ffcf22181"

func openStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	dbtest.Schema(t, db, `
        INSERT INTO projects (id, name, slug, default_language, created_at)
        VALUES ('`+projID+`', 'Test', 'test', 'en', now())`)

	return NewStore(db), db
}

func loc(lang string) *tmodel.Localization {
	return &tmodel.Localization{ID: ids.New(), Language: lang, Subject: "Hi {{ name }}", Text: "x"}
}

// newTemplate creates a template with one active version holding one
// localization, the shape a create with a subject makes.
func newTemplate(t *testing.T, s *Store, name string) *tmodel.Template {
	t.Helper()
	tpl := &tmodel.Template{ID: ids.New(), ProjectID: projID, Name: name, DefaultLanguage: "en"}
	drafts := []*tmodel.Draft{{
		Version:       &tmodel.Version{ID: ids.New()},
		Localizations: []*tmodel.Localization{loc("en")},
		Active:        true,
	}}
	if err := s.Create(t.Context(), tpl, drafts); err != nil {
		t.Fatalf("create: %v", err)
	}

	return tpl
}

// An update writes the fields it names. Before, it rewrote the whole
// row from a read taken earlier, so an activation that landed between
// the read and the write was reverted.
func TestAnUpdateLeavesTheActiveVersionAlone(t *testing.T) {
	s, _ := openStore(t)
	ctx := t.Context()
	tpl := newTemplate(t, s, "welcome")

	v2 := &tmodel.Version{ID: ids.New(), TemplateID: tpl.ID}
	if err := s.PutVersion(ctx, projID, v2); err != nil {
		t.Fatal(err)
	}

	if err := s.SetActiveVersion(ctx, projID, tpl.ID, v2.ID); err != nil {
		t.Fatal(err)
	}

	desc := "new"
	found, err := s.Update(ctx, projID, tpl.ID, &tmodel.Patch{Description: &desc})
	if err != nil || !found {
		t.Fatalf("update: %v %v", found, err)
	}

	// The stale container put through Put must not touch it either.
	tpl.Name, tpl.Description = "welcome-2", "new"
	if err := s.Put(ctx, tpl); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(ctx, projID, tpl.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got.ActiveVersionID == nil || *got.ActiveVersionID != v2.ID {
		t.Errorf("active = %v, want %s", got.ActiveVersionID, v2.ID)
	}

	if got.Description != "new" || got.Name != "welcome-2" {
		t.Errorf("got %q %q", got.Name, got.Description)
	}

	other := "unrelated"
	found, err = s.Update(ctx, projID, ids.New(), &tmodel.Patch{Name: &other})
	if err != nil || found {
		t.Errorf("an unknown template: %v %v", found, err)
	}
}

func TestVersionNumbersNeverRepeat(t *testing.T) {
	s, _ := openStore(t)
	ctx := t.Context()
	tpl := newTemplate(t, s, "welcome")

	v2 := &tmodel.Version{ID: ids.New(), TemplateID: tpl.ID}
	if err := s.PutVersion(ctx, projID, v2); err != nil {
		t.Fatal(err)
	}

	if v2.Version != 2 {
		t.Fatalf("second version = %d", v2.Version)
	}

	if err := s.DeleteVersion(ctx, projID, tpl.ID, v2.ID); err != nil {
		t.Fatal(err)
	}

	v3 := &tmodel.Version{ID: ids.New(), TemplateID: tpl.ID}
	if err := s.PutVersion(ctx, projID, v3); err != nil {
		t.Fatal(err)
	}

	if v3.Version != 3 {
		t.Errorf("after deleting the highest, next = %d, want 3", v3.Version)
	}

	foreign := &tmodel.Version{ID: ids.New(), TemplateID: tpl.ID}
	if err := s.PutVersion(ctx, ids.New(), foreign); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("another project's template: %v", err)
	}
}

func TestTemplateNamesIgnoreCase(t *testing.T) {
	s, _ := openStore(t)
	ctx := t.Context()
	tpl := newTemplate(t, s, "welcome")

	got, err := s.GetByName(ctx, projID, "WELCOME")
	if err != nil || got == nil || got.ID != tpl.ID {
		t.Fatalf("lookup by another case: %v %v", got, err)
	}

	dup := &tmodel.Template{ID: ids.New(), ProjectID: projID, Name: "Welcome", DefaultLanguage: "en"}
	err = s.Create(ctx, dup, nil)
	if !database.UniqueViolation(err, "templates_project_lower_name_key") {
		t.Errorf("a name differing only in case: %v", err)
	}
}

// A create that fails part way leaves nothing, so the request can be
// sent again.
func TestCreateIsAtomic(t *testing.T) {
	s, db := openStore(t)
	ctx := t.Context()

	tpl := &tmodel.Template{ID: ids.New(), ProjectID: projID, Name: "imported", DefaultLanguage: "en"}
	drafts := []*tmodel.Draft{
		{
			Version:       &tmodel.Version{ID: ids.New(), Version: 4},
			Stylesheet:    &sheetmodel.Stylesheet{ID: ids.New(), ProjectID: projID, Name: "house", CSS: "p{}"},
			Localizations: []*tmodel.Localization{loc("en")},
			Active:        true,
		},
		{
			Version:       &tmodel.Version{ID: ids.New()},
			Localizations: []*tmodel.Localization{loc("en"), loc("en")},
		},
	}
	if err := s.Create(ctx, tpl, drafts); err == nil {
		t.Fatal("a duplicate language must fail the create")
	}

	var n int
	if err := db.QueryRowContext(ctx, `SELECT
        (SELECT COUNT(*) FROM templates) + (SELECT COUNT(*) FROM template_versions) +
        (SELECT COUNT(*) FROM stylesheets)`).Scan(&n); err != nil {
		t.Fatal(err)
	}

	if n != 0 {
		t.Errorf("%d rows left behind", n)
	}

	drafts[1].Localizations = []*tmodel.Localization{loc("en"), loc("fr")}
	tpl.ActiveVersionID = nil
	if err := s.Create(ctx, tpl, drafts); err != nil {
		t.Fatalf("retry: %v", err)
	}

	if drafts[1].Version.Version != 5 || tpl.ActiveVersionID == nil || *tpl.ActiveVersionID != drafts[0].Version.ID {
		t.Errorf("numbering %d, active %v", drafts[1].Version.Version, tpl.ActiveVersionID)
	}

	next := &tmodel.Version{ID: ids.New(), TemplateID: tpl.ID}
	if err := s.PutVersion(ctx, projID, next); err != nil || next.Version != 6 {
		t.Errorf("counter after import: %d %v", next.Version, err)
	}
}

func TestTheActiveVersionKeepsSomethingToSend(t *testing.T) {
	s, _ := openStore(t)
	ctx := t.Context()
	tpl := newTemplate(t, s, "welcome")
	active := *tpl.ActiveVersionID

	if err := s.DeleteVersion(ctx, projID, tpl.ID, active); !errors.Is(err, ErrVersionActive) {
		t.Errorf("deleting the active version: %v", err)
	}

	locs, err := s.ListLocalizations(ctx, projID, active)
	if err != nil || len(locs) != 1 {
		t.Fatalf("localizations: %v %v", locs, err)
	}

	if err := s.DeleteLocalization(ctx, projID, tpl.ID, locs[0].ID); !errors.Is(err, ErrLastLocalization) {
		t.Errorf("deleting the last localization of the active version: %v", err)
	}

	fr := loc("fr")
	fr.VersionID = active
	if err := s.PutLocalization(ctx, projID, fr); err != nil {
		t.Fatal(err)
	}

	// Another template's id does not reach it.
	other := newTemplate(t, s, "other")
	if err := s.DeleteLocalization(ctx, projID, other.ID, fr.ID); err != nil {
		t.Fatal(err)
	}

	if got, _ := s.GetLocalizationByID(ctx, projID, other.ID, fr.ID); got != nil {
		t.Error("a localization must not be found under another template")
	}

	if got, _ := s.GetLocalizationByID(ctx, projID, tpl.ID, fr.ID); got == nil {
		t.Fatal("deleted through another template's id")
	}

	if err := s.DeleteLocalization(ctx, projID, tpl.ID, fr.ID); err != nil {
		t.Errorf("one of two may go: %v", err)
	}
}

func TestACampaignStillUsingATemplateIsFound(t *testing.T) {
	s, db := openStore(t)
	ctx := t.Context()
	tpl := newTemplate(t, s, "welcome")
	variant := newTemplate(t, s, "variant")
	unused := newTemplate(t, s, "unused")

	insert := func(name, status, templateID, variants string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `
            INSERT INTO campaigns (id, project_id, name, from_email, template_id, list_id, status, ab_variants)
            VALUES ($1, $2, $3, 'a@b.test', $4, $5, $6, $7)`,
			ids.New(), projID, name, templateID, ids.New(), status, variants); err != nil {
			t.Fatal(err)
		}
	}

	insert("done", "sent", unused.ID, "[]")
	insert("spring", "scheduled", tpl.ID, "[]")
	insert("ab", "draft", ids.New(), `[{"name":"b","template_id":"`+variant.ID+`","split_percentage":50}]`)

	for id, want := range map[string]string{tpl.ID: "spring", variant.ID: "ab", unused.ID: ""} {
		got, err := s.CampaignUsing(ctx, projID, id)
		if err != nil || got != want {
			t.Errorf("template %s: %q %v, want %q", id, got, err, want)
		}
	}

	if got, _ := s.CampaignUsing(ctx, ids.New(), tpl.ID); got != "" {
		t.Errorf("another project sees %q", got)
	}
}

// The embed flag is stored on create, and an update that does not name
// it leaves it alone.
func TestTheEmbedFlagSurvivesAPartialUpdate(t *testing.T) {
	s, _ := openStore(t)
	ctx := t.Context()
	tpl := &tmodel.Template{ID: ids.New(), ProjectID: projID, Name: "embed", DefaultLanguage: "en", EmbedImages: true}
	if err := s.Create(ctx, tpl, nil); err != nil {
		t.Fatal(err)
	}

	desc := "changed"
	if found, err := s.Update(ctx, projID, tpl.ID, &tmodel.Patch{Description: &desc}); err != nil || !found {
		t.Fatalf("update: %v %v", found, err)
	}

	got, err := s.Get(ctx, projID, tpl.ID)
	if err != nil || got == nil || !got.EmbedImages || got.Description != "changed" {
		t.Fatalf("after a partial update = %+v, %v, want embed kept", got, err)
	}

	off := false
	if _, err := s.Update(ctx, projID, tpl.ID, &tmodel.Patch{EmbedImages: &off}); err != nil {
		t.Fatal(err)
	}

	if got, _ := s.Get(ctx, projID, tpl.ID); got == nil || got.EmbedImages || got.Description != "changed" {
		t.Fatalf("after turning it off = %+v", got)
	}
}
