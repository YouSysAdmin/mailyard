// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package setting

import (
	"time"

	"github.com/yousysadmin/mailyard/internal/core/cron"
)

// The wire types of this domain: what requests carry in and what
// responses carry out, in one file.
//
// They live here rather than beside the handlers that use them so a
// reader answering "what does this endpoint accept and return" has one
// place to look. The types in internal/models are the stored shapes -
// these are what crosses the wire, and the two are allowed to differ.

// ----------------------------------------------------------------------------
// Requests
// ----------------------------------------------------------------------------

// updateInput is a bulk write. Only listed keys change, so a client
// can PATCH one value without round-tripping the whole set.
type updateInput struct {
	Settings []settingInput `json:"settings" validate:"required,min=1,max=50,dive"`
}

type settingInput struct {
	Key   string    `json:"key"   validate:"required,max=100" normalize:"trim"`
	Value flexValue `json:"value" validate:"max=1000"`
}

// ----------------------------------------------------------------------------
// Responses
// ----------------------------------------------------------------------------

// ListResponse is the platform settings registry with any overrides
// applied. Only keys in the registry exist, so the table can never
// hold a value nothing reads.
type ListResponse struct {
	Settings []SettingItem `json:"settings"`
}

// SettingItem is one registry entry with the value in force.
//
// A declared type rather than fiber.Map, or apidoc has nothing to
// reflect and the generated clients get []map[string]any.
type SettingItem struct {
	Key         string `json:"key"`
	Type        string `json:"type"`
	Default     string `json:"default"`
	Description string `json:"description"`

	// Value is the override if there is one, the default otherwise.
	Value      string `json:"value"`
	Overridden bool   `json:"overridden"`

	// Unit is a display hint ("days"), absent when there is none.
	Unit string `json:"unit,omitempty"`

	// Where the real editor is, when it is not this list. Sent by the
	// server so the console keeps no copy of the mapping.
	ManagedAt string `json:"managed_at,omitempty"`
	ManagedIn string `json:"managed_in,omitempty"`

	// Edition names a build this key only does something in, absent
	// when it is both. The console compares it with what /auth/info
	// reports and renders a control that governs nothing as a value
	// plus the reason, rather than as a switch.
	Edition string `json:"edition,omitempty"`

	// Absent until somebody writes the key, which is what tells a
	// choice from an untouched default.
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
	UpdatedBy string     `json:"updated_by,omitempty"`
}

// JobsResponse is the scheduled job roster. Which jobs appear depends
// on the node's role: an api node registers only the settings refresh,
// and that is correct rather than a missing job.
type JobsResponse struct {
	Jobs []cron.Status `json:"jobs"`
}
