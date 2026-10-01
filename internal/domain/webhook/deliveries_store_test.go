// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package webhook

import (
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	whmodel "github.com/yousysadmin/mailyard/internal/models/webhook"
)

// The log keeps each attempt's body, narrows by outcome and by event,
// and answers one attempt only through its own webhook.
func TestTheDeliveryLogKeepsTheBodyAndNarrows(t *testing.T) {
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

	hooks := make([]*whmodel.Webhook, 2)
	for i := range hooks {
		hooks[i] = &whmodel.Webhook{ID: ids.New(), ProjectID: proj, URL: "https://example.test/hook",
			Events: []string{"*"}, Secret: "s"}
		if err := s.Put(ctx, hooks[i]); err != nil {
			t.Fatalf("put: %v", err)
		}
	}

	at := time.Now().UTC()
	file := func(h *whmodel.Webhook, event, status string, offset time.Duration) *whmodel.Delivery {
		d := &whmodel.Delivery{WebhookID: h.ID, ProjectID: proj, Event: event, Status: status,
			Attempt: 1, Payload: `{"event":"` + event + `"}`, CreatedAt: at.Add(offset)}
		if err := s.RecordDelivery(ctx, d); err != nil {
			t.Fatalf("record: %v", err)
		}

		return d
	}

	sent := file(hooks[0], "email.sent", whmodel.DeliverySuccess, -3*time.Minute)
	failed := file(hooks[0], "email.failed", whmodel.DeliveryFailed, -2*time.Minute)
	file(hooks[0], "email.sent", whmodel.DeliveryFailed, -time.Minute)
	other := file(hooks[1], "email.sent", whmodel.DeliverySuccess, 0)

	cases := []struct {
		name string
		f    store.DeliveryFilter
		want int
	}{
		{"all", store.DeliveryFilter{Limit: 10}, 3},
		{"failed", store.DeliveryFilter{Status: whmodel.DeliveryFailed, Limit: 10}, 2},
		{"event", store.DeliveryFilter{Event: "email.sent", Limit: 10}, 2},
		{"both", store.DeliveryFilter{Event: "email.sent", Status: whmodel.DeliveryFailed, Limit: 10}, 1},
	}
	for _, tc := range cases {
		rows, err := s.ListDeliveries(ctx, proj, hooks[0].ID, tc.f)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}

		if len(rows) != tc.want {
			t.Errorf("%s: %d rows, want %d", tc.name, len(rows), tc.want)
		}
	}

	got, err := s.GetDelivery(ctx, proj, hooks[0].ID, failed.ID)
	if err != nil || got == nil || got.Payload != failed.Payload {
		t.Fatalf("get: %+v %v", got, err)
	}

	if got, _ := s.GetDelivery(ctx, proj, hooks[0].ID, other.ID); got != nil {
		t.Error("another webhook's attempt was readable through this one")
	}

	if got, _ := s.GetDelivery(ctx, ids.New(), hooks[0].ID, sent.ID); got != nil {
		t.Error("another project read a delivery")
	}
}
