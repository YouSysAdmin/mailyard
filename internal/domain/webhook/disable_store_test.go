// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package webhook

import (
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	whmodel "github.com/yousysadmin/mailyard/internal/models/webhook"
)

// Disable reports a change once, and an attempt finishing after its
// hook was deleted files nothing and raises no error.
func TestDisableReportsOnceAndAGoneHookFilesNothing(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := NewStore(db, crypto.New("0123456789abcdef0123456789abcdef"))
	ctx := t.Context()

	proj := ids.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, owner_id, created_at)
		VALUES ($1, 'acme', $2, NULL, now())`, proj, proj); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	h := &whmodel.Webhook{ID: ids.New(), ProjectID: proj, URL: "https://example.test/hook", Events: []string{"*"}, Secret: "s"}
	if err := s.Put(ctx, h); err != nil {
		t.Fatal(err)
	}

	if changed, err := s.Disable(ctx, ids.New(), h.ID, "x"); err != nil || changed {
		t.Errorf("another project: %v %v, want no change", changed, err)
	}

	if changed, err := s.Disable(ctx, proj, h.ID, "x"); err != nil || !changed {
		t.Fatalf("first: %v %v, want a change", changed, err)
	}

	if changed, _ := s.Disable(ctx, proj, h.ID, "y"); changed {
		t.Error("a second Disable reported a change")
	}

	if err := s.Delete(ctx, proj, h.ID); err != nil {
		t.Fatal(err)
	}

	if err := s.RecordDelivery(ctx, &whmodel.Delivery{WebhookID: h.ID, ProjectID: proj, Event: "email.sent",
		Status: whmodel.DeliveryFailed, Attempt: 2}); err != nil {
		t.Errorf("recording for a deleted hook: %v, want nothing filed and no error", err)
	}

	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM webhook_deliveries WHERE webhook_id = $1`, h.ID).Scan(&n); err != nil || n != 0 {
		t.Errorf("rows for the deleted hook = %d %v", n, err)
	}
}
