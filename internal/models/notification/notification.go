// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package notification models an in-app message to a project.
//
// Notifications are addressed to a project, not to a person. What
// they report - a bounce rate climbing, a campaign finishing, an SMTP
// server going invalid - is a fact about the project that whoever
// is looking after it needs to see, and routing per user would mean
// deciding who that is. Read state is therefore per project too: one
// member reading an alert clears it for everyone.
package notification

import "time"

// Severities, in increasing order of "stop what you are doing".
const (
	SeverityInfo    = "info"
	SeverityWarning = "warning"
	SeverityError   = "error"
)

// Types. The set is closed so the console can map each to an icon and
// a destination, and so a typo cannot invent a category nothing
// renders.
const (
	// TypeBounceRate fires when a project's bounce rate crosses the
	// configured threshold.
	TypeBounceRate = "bounce_rate"

	// TypeSMTPInvalid fires when a send marks an SMTP server invalid.
	TypeSMTPInvalid = "smtp_invalid"

	// TypeCampaignDone fires when a campaign finishes sending.
	TypeCampaignDone = "campaign_done"

	// TypeQuota fires when a project approaches its plan limit.
	TypeQuota = "quota"
)

var validSeverities = map[string]struct{}{
	SeverityInfo: {}, SeverityWarning: {}, SeverityError: {},
}

// ValidSeverity reports whether s names a real severity.
func ValidSeverity(s string) bool {
	_, ok := validSeverities[s]

	return ok
}

// Notification is one in-app message.
type Notification struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Type      string `json:"type"`
	Severity  string `json:"severity"`
	Title     string `json:"title"`
	Body      string `json:"body,omitempty"`

	// Link is a console path the console can send the reader to, e.g.
	// "/bounces". Relative on purpose - an absolute URL here would be
	// a redirect target written by whatever raised the notification.
	Link string `json:"link,omitempty"`

	// DedupeKey collapses repeats. A job that runs every fifteen
	// minutes must not file the same alert every time it runs, so it
	// writes a key naming the condition and the window, and the store
	// ignores a second insert with the same key.
	DedupeKey string `json:"-"`

	ReadAt    *time.Time `json:"read_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// Read reports whether the notification has been marked read.
func (n *Notification) Read() bool { return n.ReadAt != nil }
