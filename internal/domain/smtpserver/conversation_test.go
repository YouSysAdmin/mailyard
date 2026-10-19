// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpserver

import (
	"testing"
	"time"

	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

// A connection test is held to the conversation deadline exactly as a
// delivery is: on a server the project owns, never on the shared pool.
func TestAProjectServerTestGetsTheConversationDeadline(t *testing.T) {
	for name, tc := range map[string]struct {
		project string
		want    time.Duration
	}{
		"project row": {project: "proj-a", want: 2 * time.Minute},
		"shared pool": {project: "", want: 0},
	} {
		t.Run(name, func(t *testing.T) {
			srv := &ssmodel.Server{ProjectID: tc.project, Host: "mx.example.com", Port: 587}
			spec, err := testSpec(t.Context(), srv, nil, false, "", 2*time.Minute)
			if err != nil {
				t.Fatal(err)
			}

			if spec.ConversationTimeout != tc.want {
				t.Fatalf("conversation timeout %s, want %s", spec.ConversationTimeout, tc.want)
			}
		})
	}
}
