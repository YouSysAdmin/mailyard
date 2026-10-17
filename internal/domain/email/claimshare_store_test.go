// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
)

const (
	shareProjectA = "e66e7a4d-9e6c-4884-869a-cf9ffcf22181"
	shareProjectB = "5b0c2f4e-8f61-4d7a-9c3e-2a1d6b7e9f10"
)

func insertQueuedFor(t *testing.T, s *Store, projID, id string, due time.Time) {
	t.Helper()
	if _, err := s.DB().ExecContext(t.Context(), s.Q(`
        INSERT INTO emails (id, project_id, sender, recipients, subject,
                            status, next_attempt_at, created_at)
        VALUES (?, ?, 'a@b.com', '["c@d.com"]', 'subject', 'queued', ?, now())`),
		id, projID, due); err != nil {
		t.Fatal(err)
	}
}

// One project with a backlog due first must not take the whole batch:
// the claim holds it to its share and reaches the other project's row
// in the same statement, and a project at its share gets nothing more.
func TestAClaimHoldsEachProjectToItsShare(t *testing.T) {
	s := newClaimStore(t)
	dbtest.Schema(t, s.DB(), `
        INSERT INTO projects (id, name, slug, default_language, created_at)
        VALUES ('`+shareProjectB+`', 'Other', 'other', 'en', now())`)

	now := time.Now().UTC()
	for i := range 6 {
		insertQueuedFor(t, s, shareProjectA, ids.New(), now.Add(-time.Hour+time.Duration(i)*time.Second))
	}

	insertQueuedFor(t, s, shareProjectB, ids.New(), now.Add(-time.Minute))

	claimed, err := s.ClaimDue(t.Context(), now, 2, 1)
	if err != nil {
		t.Fatal(err)
	}

	perProject := map[string]int{}
	for _, e := range claimed {
		perProject[e.ProjectID]++
	}

	if perProject[shareProjectA] != 1 || perProject[shareProjectB] != 1 {
		t.Fatalf("claimed %v (%v), want one row of each project", idsOf(claimed), perProject)
	}

	again, err := s.ClaimDue(t.Context(), now, 2, 1)
	if err != nil {
		t.Fatal(err)
	}

	if len(again) != 0 {
		t.Fatalf("a project at its share was claimed again: %v", idsOf(again))
	}

	// Without a cap the claim takes rows in due order as before.
	open, err := s.ClaimDue(t.Context(), now, 10, 0)
	if err != nil {
		t.Fatal(err)
	}

	if len(open) != 5 {
		t.Fatalf("uncapped claim took %d rows, want the 5 left", len(open))
	}
}
