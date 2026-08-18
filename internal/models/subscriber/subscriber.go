// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package subscriber is the marketing audience member: an email
// address with a lifecycle status and free-form custom fields that
// templates render against. One row per (project, email).
package subscriber

import "time"

// Statuses. Only subscribed members receive campaigns. bounced and
// complained are set by delivery feedback, unsubscribed by the
// subscriber (or an operator).
const (
	StatusSubscribed   = "subscribed"
	StatusUnsubscribed = "unsubscribed"
	StatusBounced      = "bounced"
	StatusComplained   = "complained"
)

// ValidStatuses enumerates assignable statuses for input validation.
var ValidStatuses = map[string]struct{}{
	StatusSubscribed: {}, StatusUnsubscribed: {}, StatusBounced: {}, StatusComplained: {},
}

// Subscriber is one audience member. CustomFields is a flat JSON
// object merged into campaign template data (reserved keys email and
// name always win).
type Subscriber struct {
	ID             string         `json:"id"`
	ProjectID      string         `json:"project_id"`
	Email          string         `json:"email"`
	Name           string         `json:"name,omitempty"`
	Status         string         `json:"status"`
	CustomFields   map[string]any `json:"custom_fields,omitempty"`
	Timezone       string         `json:"timezone,omitempty"`
	Language       string         `json:"language,omitempty"`
	SubscribedAt   *time.Time     `json:"subscribed_at,omitempty"`
	UnsubscribedAt *time.Time     `json:"unsubscribed_at,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      *time.Time     `json:"updated_at,omitempty"`
}
