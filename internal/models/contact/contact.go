// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package contact models an address this project has actually sent
// to, with the delivery tallies for it.
//
// Contacts are not an audience. They are a byproduct of sending: the
// system creates and updates them, and the API exposes reads only.
// Anything you would manually curate belongs in
// internal/models/subscriber instead - see docs/contacts/contact-lists.
package contact

import "time"

// Contact is one recipient address and its delivery history.
type Contact struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`

	// Email is the lowercase address, unique within the project.
	Email string `json:"email"`

	// Name is the display name last seen on a recipient header, if
	// any. Best effort - most sends carry a bare address.
	Name string `json:"name,omitempty"`

	// SentCount and FailCount tally terminal delivery outcomes, not
	// queued messages.
	SentCount int `json:"sent_count"`
	FailCount int `json:"fail_count"`

	// Suppressed is resolved at read time from the suppression list
	// rather than stored, so it can never disagree with the list
	// that actually governs sending.
	Suppressed   bool       `json:"suppressed"`
	LastSentAt   *time.Time `json:"last_sent_at,omitempty"`
	LastFailedAt *time.Time `json:"last_failed_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}
