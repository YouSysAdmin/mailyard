// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package inbound

import (
	"testing"

	"github.com/yousysadmin/mailyard/internal/domain/store"
	imodel "github.com/yousysadmin/mailyard/internal/models/inbound"
)

// The address searches are substrings without regard to case, ANDed
// with each other and with the status, and a wildcard in the term is a
// literal. The sender is the From header, and the return path is never
// searched.
func TestTheInboundLogSearchesTheSender(t *testing.T) {
	s, proj, ctx := dedupStore(t)
	app := arrived(proj, "", "")
	app.Sender = "App <App@Example.test>"
	app.BounceAddress = "bounces@acme.test"
	app.Recipients = []string{"qa@acme.test", "ops@acme.test"}
	cron := arrived(proj, "", "")
	cron.Sender = "cron@other.test"
	for _, e := range []*imodel.Email{app, cron} {
		if err := s.Put(ctx, e); err != nil {
			t.Fatalf("put: %v", err)
		}
	}

	cases := []struct {
		name string
		f    store.InboundFilter
		want int
	}{
		{"sender substring", store.InboundFilter{Sender: "example"}, 1},
		{"sender case", store.InboundFilter{Sender: "APP@"}, 1},
		{"display name", store.InboundFilter{Sender: "app <"}, 1},
		{"return path is not searched", store.InboundFilter{Sender: "bounces@"}, 0},
		{"recipient substring", store.InboundFilter{Recipient: "OPS@"}, 1},
		{"both", store.InboundFilter{Sender: "app", Recipient: "qa@"}, 1},
		{"both, one misses", store.InboundFilter{Sender: "cron", Recipient: "qa@"}, 0},
		{"with status", store.InboundFilter{Status: imodel.StatusRejected, Sender: "app"}, 0},
		{"wildcard is literal", store.InboundFilter{Sender: "%"}, 0},
		{"exact sender inside a mailbox", store.InboundFilter{Sender: "app@example.test", Exact: true}, 1},
		{"exact sender refuses a prefix", store.InboundFilter{Sender: "app@", Exact: true}, 0},
		{"exact sender refuses a superstring", store.InboundFilter{Sender: "notapp@example.test", Exact: true}, 0},
		{"exact recipient", store.InboundFilter{Recipient: "QA@acme.test", Exact: true}, 1},
		{"exact recipient refuses a prefix", store.InboundFilter{Recipient: "qa@", Exact: true}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.List(ctx, proj, tc.f)
			if err != nil {
				t.Fatalf("list: %v", err)
			}

			if len(got) != tc.want {
				t.Fatalf("listed %d, want %d", len(got), tc.want)
			}

			if tc.want == 1 && got[0].ID != app.ID {
				t.Errorf("listed %s, want %s", got[0].Sender, app.Sender)
			}
		})
	}
}
