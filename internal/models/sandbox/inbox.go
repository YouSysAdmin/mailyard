// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package sandbox

import "time"

// Inbox is a saved filter over captured mail: a name and the sender
// addresses it collects.
//
// Decided at read time, never stamped on a capture. A row here points
// at nothing and nothing points at it, so editing the address list
// changes which captures the inbox shows - old ones included - and
// deleting it removes no mail. Addresses are stored lowercased and
// compared against the envelope sender case-insensitively.
type Inbox struct {
	ID          string   `json:"id"`
	ProjectID   string   `json:"project_id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Addresses   []string `json:"addresses"`

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}
