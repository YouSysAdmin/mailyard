// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
)

// A system row is platform mail borrowing the project's pipeline. The
// queue has to see it like any other row, and the project must not:
// not in its log, its counts or its quota, and once it is final the
// body - a reset link - is gone.
func TestASystemRowIsNotTheProjects(t *testing.T) {
	s := newClaimStore(t)
	ctx := t.Context()
	const projID = "e66e7a4d-9e6c-4884-869a-cf9ffcf22181"
	due := time.Now().UTC().Add(-time.Minute)
	row := func(system bool) *emailmodel.Email {
		return &emailmodel.Email{
			ID: ids.New(), ProjectID: projID, Sender: "a@b.test", Recipients: []string{"c@d.test"},
			Subject: "s", HTMLBody: "<a href=\"https://x.test/reset?token=t\">reset</a>", TextBody: "token",
			Status: emailmodel.StatusQueued, MaxAttempts: 3, NextAttemptAt: &due, System: system,
		}
	}
	sys, tenant := row(true), row(false)
	for _, e := range []*emailmodel.Email{sys, tenant} {
		if err := s.Put(ctx, e); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}

	// Only the tenant's row spent the quota.
	if n, err := s.AcceptedSince(ctx, projID, due.Add(-time.Hour)); err != nil || n != 1 {
		t.Fatalf("AcceptedSince = %d, %v, want 1 - the system row must not be counted", n, err)
	}

	// The project's readers do not see it, the unscoped one does.
	if got, err := s.Get(ctx, projID, sys.ID); err != nil || got != nil {
		t.Fatalf("Get(system) = %v, %v, want nil", got, err)
	}

	if got, err := s.Get(ctx, projID, tenant.ID); err != nil || got == nil {
		t.Fatalf("Get(tenant) = %v, %v, want the row", got, err)
	}

	if got, err := s.GetAny(ctx, sys.ID); err != nil || got == nil || !got.System {
		t.Fatalf("GetAny(system) = %+v, %v, want the row with System set", got, err)
	}

	list, err := s.List(ctx, projID, Filter{})
	if err != nil || len(list) != 1 || list[0].ID != tenant.ID {
		t.Fatalf("List = %d rows, %v, want the tenant row alone", len(list), err)
	}

	counts, err := s.CountByStatus(ctx, projID, nil, nil)
	if err != nil || counts[emailmodel.StatusQueued] != 1 {
		t.Fatalf("CountByStatus = %v, %v, want queued=1", counts, err)
	}

	// The queue claims both, and the flag rides along the RETURNING.
	claimed, err := s.ClaimDue(ctx, time.Now().UTC(), 10)
	if err != nil || len(claimed) != 2 {
		t.Fatalf("ClaimDue = %d rows, %v, want 2", len(claimed), err)
	}

	now := time.Now().UTC()
	for _, c := range claimed {
		if c.ID == sys.ID && !c.System {
			t.Fatal("the claim lost the System flag")
		}

		ok, err := s.Finalize(ctx, c.ID, c.CreatedAt, c.ClaimedAt, emailmodel.StatusSent, "", "", &now)
		if err != nil || !ok {
			t.Fatalf("Finalize(%s) = %v, %v", c.ID, ok, err)
		}
	}

	// Final means the system body is gone and the tenant's is kept.
	if got, _ := s.GetAny(ctx, sys.ID); got.HTMLBody != "" || got.TextBody != "" || got.Status != emailmodel.StatusSent {
		t.Fatalf("system row after Finalize: status=%s html=%q text=%q, want sent with empty bodies", got.Status, got.HTMLBody, got.TextBody)
	}

	if got, _ := s.GetAny(ctx, tenant.ID); got.HTMLBody == "" || got.TextBody == "" {
		t.Fatal("Finalize cleared a tenant row's body")
	}
}
