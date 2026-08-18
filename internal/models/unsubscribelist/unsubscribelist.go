// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package unsubscribelist models a transactional opt-out scope.
//
// An unsubscribe list is not an audience and carries no membership.
// It is a named scope that a send can reference so the recipient gets
// a one-click link which blocks only that category of mail - receipts
// and password resets keep flowing. Compare
// internal/models/subscriberlist, which is subscriber-keyed and is
// the thing campaigns actually target.
package unsubscribelist

import "time"

// List is one opt-out scope.
type List struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`

	// Name is the internal label, unique per project.
	Name string `json:"name"`

	// PublicName is what the recipient sees on the hosted
	// unsubscribe page. Falls back to Name when empty.
	PublicName  string `json:"public_name,omitempty"`
	Description string `json:"description,omitempty"`

	// Active gates whether new links are minted for this list.
	// Existing suppressions keep applying either way.
	Active bool `json:"active"`

	// SuppressedCount is filled by list reads for display and is
	// never stored.
	SuppressedCount int        `json:"suppressed_count"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       *time.Time `json:"updated_at,omitempty"`
}

// Display returns the name to show a recipient.
func (l *List) Display() string {
	if l.PublicName != "" {
		return l.PublicName
	}

	return l.Name
}
