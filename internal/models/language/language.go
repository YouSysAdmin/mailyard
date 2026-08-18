// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package language is the per-project language registry backing
// template localization dropdowns and defaults.
package language

import "time"

// Language is one entry in a project's registry.
type Language struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	IsDefault bool      `json:"is_default"`
	CreatedAt time.Time `json:"created_at"`
}
