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

// The worker's write moves an enabled server only, once, and only in
// its own project. A disabled server is a person's decision and stays.
func TestMarkInvalidMovesAnEnabledServerOnce(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	ctx := t.Context()

	projA, projB := ids.New(), ids.New()
	for _, p := range []string{projA, projB} {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO projects (id, name, slug, owner_id, created_at)
			VALUES ($1, 'p', $2, NULL, now())`, p, p); err != nil {
			t.Fatalf("seed project: %v", err)
		}
	}

	s := NewStore(db, crypto.New("0123456789abcdef0123456789abcdef"))
	put := func(proj, status string) string {
		srv := &ssmodel.Server{
			ID: ids.New(), ProjectID: proj, Name: ids.New(), Host: "smtp.example.net",
			Port: 587, Encryption: "starttls", Status: status, CreatedAt: time.Now().UTC(),
		}
		if err := s.Put(ctx, srv); err != nil {
			t.Fatalf("Put: %v", err)
		}

		return srv.ID
	}

	enabled := put(projA, ssmodel.StatusEnabled)
	disabled := put(projA, ssmodel.StatusDisabled)

	moved, err := s.MarkInvalid(ctx, projB, enabled, "535 bad credentials")
	if err != nil || moved {
		t.Fatalf("another project: moved %v err %v, want false", moved, err)
	}

	moved, err = s.MarkInvalid(ctx, projA, enabled, "535 bad credentials")
	if err != nil || !moved {
		t.Fatalf("first: moved %v err %v, want true", moved, err)
	}

	if moved, _ = s.MarkInvalid(ctx, projA, enabled, "535 bad credentials"); moved {
		t.Error("second call moved the row again")
	}

	if moved, _ = s.MarkInvalid(ctx, projA, disabled, "535 bad credentials"); moved {
		t.Error("a disabled server was marked invalid")
	}

	got, err := s.Get(ctx, projA, enabled)
	if err != nil || got == nil {
		t.Fatalf("Get: %v", err)
	}

	if got.Status != ssmodel.StatusInvalid || got.ValidationError != "535 bad credentials" || got.ValidatedAt == nil {
		t.Errorf("row = status %q error %q validated %v", got.Status, got.ValidationError, got.ValidatedAt)
	}
}
