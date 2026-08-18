// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package bounce is the delivery-failure record: one row per rejected
// recipient, written by the worker on permanent SMTP failures and by
// the bounce ingest webhook.
package bounce

import "time"

// Types classify the failure.
const (
	TypeHard      = "hard"
	TypeSoft      = "soft"
	TypeComplaint = "complaint"
)

// ValidTypes enumerates assignable types for input validation.
var ValidTypes = map[string]struct{}{
	TypeHard: {}, TypeSoft: {}, TypeComplaint: {},
}

// Bounce is one rejected recipient.
type Bounce struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	EmailID   string    `json:"email_id,omitempty"`
	Recipient string    `json:"recipient"`
	Type      string    `json:"type"`
	Reason    string    `json:"reason,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
