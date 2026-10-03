// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package subscriber

import (
	"maps"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/validation"
	submodel "github.com/yousysadmin/mailyard/internal/models/subscriber"
)

// An import row updating an existing subscriber keeps what the row
// leaves empty and lays its custom fields over the stored ones.
func TestAnImportRowKeepsWhatItLeavesEmpty(t *testing.T) {
	existing := &submodel.Subscriber{
		ID: "s1", Email: "a@b.test", Name: "Ada", Status: submodel.StatusSubscribed,
		Timezone: "Europe/Lisbon", Language: "pt",
		CustomFields: map[string]any{"plan": "pro", "city": "Porto"},
	}

	got := mergeImported(existing, &upsertInput{Email: "a@b.test", CustomFields: map[string]any{"city": "Lisbon"}})

	if got.ID != "s1" || got.Name != "Ada" || got.Timezone != "Europe/Lisbon" || got.Language != "pt" {
		t.Errorf("merged = %+v, want the stored fields kept", got)
	}

	if want := map[string]any{"plan": "pro", "city": "Lisbon"}; !maps.Equal(got.CustomFields, want) {
		t.Errorf("custom fields = %v, want %v", got.CustomFields, want)
	}

	if existing.CustomFields["city"] != "Porto" {
		t.Error("the stored subscriber was written through")
	}

	got = mergeImported(existing, &upsertInput{Email: "a@b.test", Name: "Ada L", Timezone: "Asia/Tokyo"})
	if got.Name != "Ada L" || got.Timezone != "Asia/Tokyo" {
		t.Errorf("merged = %+v, want the named fields applied", got)
	}
}

// A row is judged on its own, which is what lets the import report it
// instead of refusing the whole file.
func TestAnImportRowIsValidatedAlone(t *testing.T) {
	for _, row := range []upsertInput{
		{Email: "not-an-address"},
		{Email: "a@b.test", Timezone: "Mars/Olympus"},
		{Email: "a@b.test", Status: "gone"},
	} {
		if err := validation.NormalizeAndValidate(&row); err == nil {
			t.Errorf("%+v passed, want it refused", row)
		}
	}

	ok := upsertInput{Email: " A@B.test ", Timezone: "Europe/Lisbon"}
	if err := validation.NormalizeAndValidate(&ok); err != nil {
		t.Errorf("a good row was refused: %v", err)
	}
}
