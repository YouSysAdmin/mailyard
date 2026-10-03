// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
)

// The tag and template filters match regardless of case, like every
// filter in the API.
func TestTheTagAndTemplateFiltersIgnoreCase(t *testing.T) {
	s := newClaimStore(t)
	ctx := t.Context()
	const projID = "e66e7a4d-9e6c-4884-869a-cf9ffcf22181"
	row := &emailmodel.Email{
		ID: ids.New(), ProjectID: projID, Sender: "a@b.test", Recipients: []string{"c@d.test"},
		Subject: "s", TextBody: "t", Status: emailmodel.StatusSent, MaxAttempts: 3,
		TemplateName: "Welcome", Tags: []string{"Onboarding"},
	}
	if err := s.Put(ctx, row); err != nil {
		t.Fatalf("Put: %v", err)
	}

	for _, f := range []Filter{
		{Tag: "onboarding"}, {Tag: "ONBOARDING"}, {Tag: "Onboarding"},
		{Template: "welcome"}, {Template: "WELCOME"},
	} {
		got, err := s.List(ctx, projID, f)
		if err != nil || len(got) != 1 {
			t.Errorf("List(%+v) = %d rows, %v, want the row", f, len(got), err)
		}
	}

	for _, f := range []Filter{{Tag: "onboard"}, {Template: "welcom"}} {
		if got, err := s.List(ctx, projID, f); err != nil || len(got) != 0 {
			t.Errorf("List(%+v) = %d rows, %v, want none - these are not substring filters", f, len(got), err)
		}
	}
}
