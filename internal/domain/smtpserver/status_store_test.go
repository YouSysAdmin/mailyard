// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpserver

import (
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

// An edit never moves a server in or out of rotation, a test never
// switches a disabled server on or marks it invalid, and only a dial
// change replaces an invalid verdict - leaving the server invalid.
func TestStatusMovesOnlyThroughATest(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	ctx := t.Context()

	proj := ids.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, owner_id, created_at)
		VALUES ($1, 'p', $2, NULL, now())`, proj, proj); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	s := NewStore(db, crypto.New("0123456789abcdef0123456789abcdef"))
	put := func(status string) *ssmodel.Server {
		srv := &ssmodel.Server{
			ID: ids.New(), ProjectID: proj, Name: ids.New(), Host: "smtp.example.net",
			Port: 587, Encryption: "starttls", Status: status, CreatedAt: time.Now().UTC(),
		}
		if err := s.Put(ctx, srv); err != nil {
			t.Fatalf("Put: %v", err)
		}

		return srv
	}

	status := func(id string) (string, string) {
		got, err := s.Get(ctx, proj, id)
		if err != nil || got == nil {
			t.Fatalf("Get: %v", err)
		}

		return got.Status, got.ValidationError
	}

	// An edit carrying a stale status writes nothing to the columns.
	srv := put(ssmodel.StatusEnabled)
	if _, err := s.MarkInvalid(ctx, proj, srv.ID, "535 bad credentials"); err != nil {
		t.Fatal(err)
	}

	srv.Priority = 5
	if err := s.Put(ctx, srv); err != nil {
		t.Fatal(err)
	}

	if st, reason := status(srv.ID); st != ssmodel.StatusInvalid || reason != "535 bad credentials" {
		t.Errorf("after a priority edit: %q %q, want invalid with the old reason", st, reason)
	}

	if err := s.NoteSettingsChanged(ctx, proj, srv.ID); err != nil {
		t.Fatal(err)
	}

	if st, reason := status(srv.ID); st != ssmodel.StatusInvalid || reason != ssmodel.SettingsChangedNote {
		t.Errorf("after a dial edit: %q %q, want invalid with the settings note", st, reason)
	}

	if got, err := s.RecordTest(ctx, proj, srv.ID, ""); err != nil || got != ssmodel.StatusEnabled {
		t.Errorf("passing test: %q %v, want enabled", got, err)
	}

	// A disabled server keeps its status through both outcomes.
	off := put(ssmodel.StatusDisabled)
	if got, _ := s.RecordTest(ctx, proj, off.ID, "dial tcp: refused"); got != ssmodel.StatusDisabled {
		t.Errorf("failing test on a disabled server left %q", got)
	}

	if got, _ := s.RecordTest(ctx, proj, off.ID, ""); got != ssmodel.StatusDisabled {
		t.Errorf("passing test on a disabled server left %q", got)
	}

	if err := s.NoteSettingsChanged(ctx, proj, off.ID); err != nil {
		t.Fatal(err)
	}

	if _, reason := status(off.ID); reason == ssmodel.SettingsChangedNote {
		t.Error("the settings note landed on a server that is not invalid")
	}

	// A failing test takes an enabled server out.
	on := put(ssmodel.StatusEnabled)
	if got, _ := s.RecordTest(ctx, proj, on.ID, "535 no"); got != ssmodel.StatusInvalid {
		t.Errorf("failing test on an enabled server left %q", got)
	}

	if got, err := s.RecordTest(ctx, ids.New(), on.ID, ""); err != nil || got != "" {
		t.Errorf("another project: %q %v, want no row", got, err)
	}
}
