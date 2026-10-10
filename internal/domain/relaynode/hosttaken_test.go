// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relaynode

import (
	"context"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	nodemodel "github.com/yousysadmin/mailyard/internal/models/relaynode"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

type oneNode struct{ store.RelayNodeStore }

func (oneNode) ListAll(context.Context) ([]*nodemodel.Node, error) {
	return []*nodemodel.Node{{ID: pendingNodeID, ServerID: pendingServerID}}, nil
}

type sharedAt struct {
	store.SharedSMTPStore
	host string
}

func (s sharedAt) Get(context.Context, string) (*ssmodel.Shared, error) {
	return &ssmodel.Shared{Server: ssmodel.Server{ID: pendingServerID, Host: s.host}}, nil
}

// An IP literal is one identity however it is spelled, because the
// certificate carries the parsed address. A stored host in any
// spelling is matched too.
func TestAnIPLiteralIsTakenInEverySpelling(t *testing.T) {
	cases := []struct {
		stored, asked string
		taken         bool
	}{
		{"10.0.0.5", "::ffff:10.0.0.5", true},
		{"10.0.0.5", "::FFFF:10.0.0.5", true},
		{"2001:db8::1", "2001:0DB8:0:0:0:0:0:1", true},
		{"2001:0db8:0:0:0:0:0:1", "2001:db8::1", true},
		{"::ffff:10.0.0.5", "10.0.0.5", true},
		{"relay.example.test", "RELAY.example.test.", true},
		{"10.0.0.5", "10.0.0.6", false},
		{"2001:db8::1", "2001:db8::2", false},
	}

	for _, tc := range cases {
		h := &Handler{Runtime: &env.Runtime{
			Store: &store.Store{RelayNode: oneNode{}, SharedSMTP: sharedAt{host: tc.stored}},
		}}

		host, err := validHost(tc.asked)
		if err != nil {
			t.Fatalf("validHost(%q): %v", tc.asked, err)
		}

		taken, err := h.hostTaken(t.Context(), host)
		if err != nil {
			t.Fatal(err)
		}

		if taken != tc.taken {
			t.Errorf("stored %q, asked %q: taken=%v, want %v", tc.stored, tc.asked, taken, tc.taken)
		}
	}
}
