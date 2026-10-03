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

// smtp_server_id is the server that delivered the message, which is
// what the log documents, not the one the request pinned.
func TestTheServerFilterMatchesTheDeliveringServer(t *testing.T) {
	s := newClaimStore(t)
	ctx := t.Context()
	const projID = "e66e7a4d-9e6c-4884-869a-cf9ffcf22181"
	row := &emailmodel.Email{
		ID: ids.New(), ProjectID: projID, Sender: "a@b.test", Recipients: []string{"c@d.test"},
		Subject: "s", TextBody: "t", Status: emailmodel.StatusSent, MaxAttempts: 3,
	}
	if err := s.Put(ctx, row); err != nil {
		t.Fatalf("Put: %v", err)
	}

	via := ids.New()
	if _, err := s.DB().ExecContext(ctx, `UPDATE emails SET delivered_via = $1 WHERE id = $2`, via, row.ID); err != nil {
		t.Fatalf("stamp delivered_via: %v", err)
	}

	if got, err := s.List(ctx, projID, Filter{SMTPServerID: via}); err != nil || len(got) != 1 {
		t.Errorf("filter on the delivering server = %d rows, %v, want the row", len(got), err)
	}

	if got, err := s.List(ctx, projID, Filter{SMTPServerID: ids.New()}); err != nil || len(got) != 0 {
		t.Errorf("filter on another server = %d rows, %v, want none", len(got), err)
	}
}

// A manual retry requeues the row for the recipients the caller kept.
func TestResetRequeuesForTheGivenRecipients(t *testing.T) {
	s := newClaimStore(t)
	ctx := t.Context()
	const projID = "e66e7a4d-9e6c-4884-869a-cf9ffcf22181"
	row := &emailmodel.Email{
		ID: ids.New(), ProjectID: projID, Sender: "a@b.test", Recipients: []string{"c@d.test", "e@f.test"},
		Subject: "s", TextBody: "t", Status: emailmodel.StatusFailed, MaxAttempts: 3, Attempts: 3,
	}
	if err := s.Put(ctx, row); err != nil {
		t.Fatalf("Put: %v", err)
	}

	ok, err := s.Reset(ctx, projID, row.ID, []string{"e@f.test"})
	if err != nil || !ok {
		t.Fatalf("Reset = %v, %v", ok, err)
	}

	got, err := s.Get(ctx, projID, row.ID)
	if err != nil || got == nil {
		t.Fatalf("Get: %v", err)
	}

	if got.Status != emailmodel.StatusQueued || got.Attempts != 0 || len(got.Recipients) != 1 || got.Recipients[0] != "e@f.test" {
		t.Errorf("after reset: status=%s attempts=%d recipients=%v", got.Status, got.Attempts, got.Recipients)
	}
}
