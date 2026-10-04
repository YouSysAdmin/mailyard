// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package campaign

import (
	"context"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain/email"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	stylesheetdomain "github.com/yousysadmin/mailyard/internal/domain/stylesheet"
	templatedomain "github.com/yousysadmin/mailyard/internal/domain/template"
	cmodel "github.com/yousysadmin/mailyard/internal/models/campaign"
	submodel "github.com/yousysadmin/mailyard/internal/models/subscriber"
	tmodel "github.com/yousysadmin/mailyard/internal/models/template"
)

// renderFixture stores two templates, the second with an English and a
// Ukrainian localization, and a campaign that A/B tests them.
func renderFixture(t *testing.T) (*email.Service, *cmodel.Campaign) {
	t.Helper()

	db := dbtest.Open(t)
	dbtest.Migrate(t, db)

	projID := ids.New()
	dbtest.Schema(t, db, `
        INSERT INTO projects (id, name, slug, default_language, created_at)
        VALUES ('`+projID+`', 'Test', '`+projID+`', 'en', now())`)

	ts := templatedomain.NewStore(db)
	ctx := t.Context()
	put := func(name string, locs map[string][2]string) string {
		tpl := &tmodel.Template{ID: ids.New(), ProjectID: projID, Name: name, DefaultLanguage: "en"}
		if err := ts.Put(ctx, tpl); err != nil {
			t.Fatal(err)
		}

		v := &tmodel.Version{ID: ids.New(), TemplateID: tpl.ID}
		if err := ts.PutVersion(ctx, projID, v); err != nil {
			t.Fatal(err)
		}

		for lang, sh := range locs {
			if err := ts.PutLocalization(ctx, projID, &tmodel.Localization{
				ID: ids.New(), VersionID: v.ID, Language: lang, Subject: sh[0], HTML: sh[1],
			}); err != nil {
				t.Fatal(err)
			}
		}

		if err := ts.SetActiveVersion(ctx, projID, tpl.ID, v.ID); err != nil {
			t.Fatal(err)
		}

		return tpl.ID
	}

	a := put("a", map[string][2]string{"en": {"A {{ plan }}", "<p>A {{ name }} {{ plan }} {{ email }}</p>"}})
	b := put("b", map[string][2]string{
		"en": {"B {{ name }}", "<p>B en {{ name }} {{ city }}</p>"},
		"uk": {"B uk {{ name }}", "<p>B uk {{ name }} {{ city }}</p>"},
	})

	svc := &email.Service{Store: &store.Store{Template: ts, Stylesheet: stylesheetdomain.NewStore(db)}}
	c := &cmodel.Campaign{
		ID: ids.New(), ProjectID: projID, TemplateID: a, Subject: "fallback", Language: "en",
		TemplateData:  map[string]any{"plan": "gold", "name": "campaign"},
		ABTestEnabled: true,
		ABVariants:    []cmodel.Variant{{Name: "B", TemplateID: b, Subject: "Hi {{ name }}"}},
	}

	return svc, c
}

// What a subscriber is sent: the variant's template and subject, the
// subscriber's language with a fallback, and data layered campaign <
// custom fields < email and name.
func TestASubscriberIsRenderedAsTheCampaignSays(t *testing.T) {
	svc, c := renderFixture(t)
	ctx := t.Context()

	ada := &submodel.Subscriber{Email: "ada@example.com", Name: "Ada", Language: "uk",
		CustomFields: map[string]any{"plan": "silver", "city": "Kyiv"}}
	bob := &submodel.Subscriber{Email: "bob@example.com", Name: "Bob", Language: "fr"}

	cases := []struct {
		name, variant string
		sub           *submodel.Subscriber
		subject, html string
	}{
		{"no variant", "", ada, "A silver", "<p>A Ada silver ada@example.com</p>"},
		{"variant in the subscriber's language", "B", ada, "Hi Ada", "<p>B uk Ada Kyiv</p>"},
		{"variant falling back to the default", "B", bob, "Hi Bob", "<p>B en Bob </p>"},
		{"unknown variant", "Z", bob, "A gold", "<p>A Bob gold bob@example.com</p>"},
		{"preview with nobody", "", nil, "A gold", "<p>A campaign gold </p>"},
	}
	for _, tc := range cases {
		out, tpl, err := renderForSubscriber(ctx, svc.ResolveTemplate, c, tc.variant, tc.sub)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}

		if out.Subject != tc.subject || out.HTML != tc.html || tpl == nil {
			t.Errorf("%s: %q %q, want %q %q", tc.name, out.Subject, out.HTML, tc.subject, tc.html)
		}
	}
}

// A batch renders exactly what the preview renders, and asks the store
// once per template and language however many subscribers share them.
func TestABatchResolvesEachTemplateOnce(t *testing.T) {
	svc, c := renderFixture(t)
	ctx := t.Context()

	calls := 0
	b := &batchMemo{resolve: func(ctx context.Context, projID string, ref *email.TemplateRef) (*email.ResolvedTemplate, error) {
		calls++

		return svc.ResolveTemplate(ctx, projID, ref)
	}}

	subs := []*submodel.Subscriber{
		{Email: "a@example.com", Name: "A", Language: "uk", CustomFields: map[string]any{"city": "Lviv"}},
		{Email: "b@example.com", Name: "B", Language: "uk", CustomFields: map[string]any{"city": "Kyiv"}},
		{Email: "c@example.com", Name: "C", Language: "en"},
		{Email: "d@example.com", Name: "D", Language: "en"},
	}
	for _, variant := range []string{"", "B"} {
		for _, sub := range subs {
			got, _, err := renderForSubscriber(ctx, b.resolveTemplate, c, variant, sub)
			if err != nil {
				t.Fatal(err)
			}

			want, _, err := renderForSubscriber(ctx, svc.ResolveTemplate, c, variant, sub)
			if err != nil {
				t.Fatal(err)
			}

			if got.Subject != want.Subject || got.HTML != want.HTML || got.Text != want.Text {
				t.Errorf("%s %s: batch %q %q, preview %q %q", variant, sub.Email, got.Subject, got.HTML, want.Subject, want.HTML)
			}
		}
	}

	// Template A in uk and en, template B in uk and en.
	if calls != 4 {
		t.Fatalf("%d resolves for two templates in two languages, want 4", calls)
	}
}
