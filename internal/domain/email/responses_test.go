// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"encoding/json"
	"testing"
	"time"

	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
)

// These response bodies were fiber.Map literals until the OpenAPI
// document started being reflected from types. Converting them was a
// rename of every key in every body, and a key that changed spelling
// silently breaks every client without failing a single existing test.
//
// So the wire form is pinned here. The live comparison against the
// previous binary covered fourteen endpoints, but not the email log:
// exercising it needs an accepted send, which needs a verified domain
// and a resolvable server. This does the same job deterministically,
// and unlike that comparison it keeps doing it.
func TestResponseKeysAreStable(t *testing.T) {
	sent := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	e := &emailmodel.Email{ID: "9e2f6f11-cdd3-4058-86f2-29f3ad60b06a", Status: emailmodel.StatusSent, Attempts: 2}

	cases := []struct {
		name string
		body any
		want string
	}{
		{
			name: "send",
			body: SendResponse{Email: e, Suppressed: emptyIfNil(nil)},
			// suppressed_recipients must be [] and never null: it is
			// built through emptyIfNil precisely so a client does not
			// have to tell an empty list from a missing one.
			want: `{"email":{"id":"9e2f6f11-cdd3-4058-86f2-29f3ad60b06a","project_id":"","sender":"","recipients":null,"subject":"","tracked":false,"open_count":0,"click_count":0,"status":"sent","attempts":2,"max_attempts":0,"created_at":"0001-01-01T00:00:00Z"},"suppressed_recipients":[]}`,
		},
		{
			name: "dry run",
			body: DryRunResponse{DryRun: true, Valid: true, Recipients: 2, Suppressed: emptyIfNil(nil)},
			want: `{"dry_run":true,"valid":true,"recipients":2,"suppressed_recipients":[]}`,
		},
		{
			name: "status",
			body: StatusResponse{ID: "9e2f6f11-cdd3-4058-86f2-29f3ad60b06a", Status: "sent", Attempts: 2, ErrorMessage: "", SentAt: &sent},
			want: `{"id":"9e2f6f11-cdd3-4058-86f2-29f3ad60b06a","status":"sent","attempts":2,"error_message":"","sent_at":"2026-08-08T12:00:00Z"}`,
		},
		{
			name: "stats",
			body: StatsResponse{Counts: map[string]int{"sent": 3}},
			want: `{"counts":{"sent":3}}`,
		},
		{
			name: "limits",
			body: LimitsResponse{Limits: SendingLimits{MaxRecipients: 50, MaxAttachments: 10,
				MaxAttachmentSize: 1024, MaxTotalAttachmentSize: 2048}},
			want: `{"limits":{"max_recipients":50,"max_attachments":10,"max_attachment_size":1024,"max_total_attachment_size":2048}}`,
		},
		{
			name: "batch",
			body: BatchResponse{Total: 1, Accepted: 1, Results: []BatchResult{{Index: 0, EmailID: "9e2f6f11-cdd3-4058-86f2-29f3ad60b06a", Status: "queued"}}},
			want: `{"total":1,"accepted":1,"results":[{"index":0,"email_id":"9e2f6f11-cdd3-4058-86f2-29f3ad60b06a","status":"queued"}]}`,
		},
		{
			name: "empty list is an array",
			body: ListResponse{Emails: []*emailmodel.Email{}},
			want: `{"emails":[]}`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := json.Marshal(c.body)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			if string(got) != c.want {
				t.Errorf("wire form changed\n got: %s\nwant: %s", got, c.want)
			}
		})
	}
}

// A nil slice would marshal as null, which is why every list-bearing
// response runs its slice through emptyIfNil. This pins the helper
// rather than trusting each call site to remember.
func TestEmptyIfNilNeverYieldsNull(t *testing.T) {
	out, err := json.Marshal(emptyIfNil(nil))
	if err != nil {
		t.Fatal(err)
	}

	if string(out) != "[]" {
		t.Errorf("emptyIfNil(nil) marshals as %s, want []", out)
	}
}
