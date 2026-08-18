// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package plan models platform-wide usage packages assigned to
// projects. Every limit uses zero for unlimited. Plans are managed
// by platform admins and are not tenant resources.
package plan

import "time"

// Plan is one set of limits a project can be assigned. Every limit
// reads zero as unlimited.
type Plan struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`

	// IsDefault marks the plan applied to projects without an
	// explicit assignment. At most one plan holds the flag.
	IsDefault bool `json:"is_default"`

	// Email volume limits, counted per project over rolling
	// windows from the emails table.
	HourlyEmailLimit int `json:"hourly_email_limit"`
	DailyEmailLimit  int `json:"daily_email_limit"`

	// Resource caps checked at create time.
	MaxAPIKeys     int `json:"max_api_keys"`
	MaxSMTPServers int `json:"max_smtp_servers"`
	MaxDomains     int `json:"max_domains"`
	MaxSubscribers int `json:"max_subscribers"`

	// MaxSandboxMessages is the ring buffer for captured mail, applied
	// on every capture. It bounds the sandbox table, which is the one
	// thing a developer under test can fill without limit.
	MaxSandboxMessages int `json:"max_sandbox_messages"`

	// MaxSandboxRetentionDays is a CEILING on the window a project may
	// choose, not the window itself - the project picks that
	// (projects.sandbox_retention_days) and this is how far it may go.
	MaxSandboxRetentionDays int        `json:"max_sandbox_retention_days"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               *time.Time `json:"updated_at,omitempty"`
}
