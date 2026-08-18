// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package sender models approved sender addresses. An address can
// only be registered when its domain is verified by the project,
// and the console offers these in every From selector.
package sender

import "time"

// Sender is one approved From address.
type Sender struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	CreatedBy string `json:"created_by,omitempty"`

	// Email is the full lowercase address (billing@example.com).
	Email string `json:"email"`

	// Name is the optional display name used as "Name <email>".
	Name      string    `json:"name,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
