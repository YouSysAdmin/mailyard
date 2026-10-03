// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain/store"
)

// Captures stored before the envelope was normalised carry the mailbox
// form, and the migration reduces them to bare addresses so the
// address filters find them.
func TestTheMigrationReducesOldEnvelopesToBareAddresses(t *testing.T) {
	s := testStore(t)
	proj := newProject(t, s)
	e := put(t, s, proj, time.Now().UTC(), "old", nil)

	if _, err := s.Exec(t.Context(), `UPDATE sandbox_emails SET sender = ?, recipients = ? WHERE id = ?`,
		`"Acme" <noreply@acme.test>`, `["Jane <jane@example.com>","bob@example.com"]`, e.ID); err != nil {
		t.Fatalf("seed old row: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(dbtest.MigrationsDir(t), "00121_sandbox_emails_bare_envelope.sql"))
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}

	up, _, _ := strings.Cut(string(body), "-- +goose Down")
	//sqlconst:allow the SQL is a committed migration file, not runtime data
	if _, err := s.DB().ExecContext(t.Context(), up); err != nil {
		t.Fatalf("apply migration: %v", err)
	}

	got, err := s.Get(t.Context(), proj, e.ID)
	if err != nil || got == nil {
		t.Fatalf("get: %v %v", got, err)
	}

	if got.Sender != "noreply@acme.test" {
		t.Errorf("sender = %q, want the bare address", got.Sender)
	}

	if len(got.Recipients) != 2 || got.Recipients[0] != "jane@example.com" || got.Recipients[1] != "bob@example.com" {
		t.Errorf("recipients = %v, want bare addresses in order", got.Recipients)
	}

	n, err := s.Count(t.Context(), proj, store.SandboxFilter{Sender: "noreply@acme.test", Exact: true})
	if err != nil || n != 1 {
		t.Errorf("exact sender match found %d (%v), want 1", n, err)
	}
}
