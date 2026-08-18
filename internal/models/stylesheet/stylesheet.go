// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package stylesheet is the reusable CSS block template versions
// reference. The CSS is inlined into rendered HTML at send time.
package stylesheet

import "time"

// Stylesheet is one reusable CSS block.
type Stylesheet struct {
	ID        string     `json:"id"`
	ProjectID string     `json:"project_id"`
	Name      string     `json:"name"`
	CSS       string     `json:"css"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}
