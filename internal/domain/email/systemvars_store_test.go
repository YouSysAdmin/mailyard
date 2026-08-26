// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	coretracking "github.com/yousysadmin/mailyard/internal/core/tracking"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	stylesheetdomain "github.com/yousysadmin/mailyard/internal/domain/stylesheet"
	templatedomain "github.com/yousysadmin/mailyard/internal/domain/template"
	tmodel "github.com/yousysadmin/mailyard/internal/models/template"
)

// The seam the two unit tests beside this one cannot reach: that
// RenderTemplate is the place the reserved names are injected, so a
// stored template using one renders on the transactional path.
//
// Written as a store test because the injection sits inside the
// resolve-template-version-localization walk, and a fake would only
// prove the fake injects.
func TestAStoredTemplateResolvesAReservedVariable(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)

	const projID = "e66e7a4d-9e6c-4884-869a-cf9ffcf22181"
	dbtest.Schema(t, db, `
        INSERT INTO projects (id, name, slug, default_language, created_at)
        VALUES ('`+projID+`', 'Test', 'test', 'en', now())`)

	ts := templatedomain.NewStore(db)
	ctx := t.Context()

	tpl := &tmodel.Template{
		ID:              ids.New(),
		ProjectID:       projID,
		Name:            "receipt",
		DefaultLanguage: "en",
	}
	if err := ts.Put(ctx, tpl); err != nil {
		t.Fatalf("put template: %v", err)
	}

	v := &tmodel.Version{ID: ids.New(), TemplateID: tpl.ID}
	if err := ts.PutVersion(ctx, projID, v); err != nil {
		t.Fatalf("put version: %v", err)
	}

	// Both reserved names, and a caller variable alongside them, so the
	// test also shows the injection does not displace ordinary data.
	if err := ts.PutLocalization(ctx, projID, &tmodel.Localization{
		ID:        ids.New(),
		VersionID: v.ID,
		Language:  "en",
		Subject:   "Receipt for {{ name }}",
		HTML: `<p>Thanks {{ name }}.</p>` +
			`<a href="{{ mailyard_web_view_url }}">online</a>` +
			`<a href="{{ mailyard_unsubscribe_url }}">out</a>`,
	}); err != nil {
		t.Fatalf("put localization: %v", err)
	}

	if err := ts.SetActiveVersion(ctx, projID, tpl.ID, v.ID); err != nil {
		t.Fatalf("activate: %v", err)
	}

	// Lenient is false, which is what a transactional send uses and what
	// made this fail before: a missing key was an error, and the
	// reserved names were missing keys.
	svc := &Service{Store: &store.Store{
		Template:   ts,
		Stylesheet: stylesheetdomain.NewStore(db),
	}}

	out, _, err := svc.RenderTemplate(ctx, projID, &TemplateRef{
		ID:   tpl.ID,
		Data: map[string]any{"name": "Ada"},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if !strings.Contains(out.Subject, "Ada") {
		t.Errorf("caller data lost: %q", out.Subject)
	}

	// Placeholders, not URLs - the message has no id yet. Send finishes
	// the job, which is what TestResolveSystemLinks covers.
	if !coretracking.HasSystemSentinels(out.HTML) {
		t.Errorf("reserved variables did not render to placeholders: %s", out.HTML)
	}
}
