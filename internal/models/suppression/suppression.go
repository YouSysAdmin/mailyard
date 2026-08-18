// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package suppression is the do-not-send list entry. One row per
// (project, email) - re-suppressing updates the kind and reason.
package suppression

import "time"

// Kinds record why an address is blocked.
const (
	KindHard      = "hard"
	KindBounce    = "bounce"
	KindComplaint = "complaint"
	KindManual    = "manual"

	// KindListUnsubscribe is written when a recipient uses a
	// one-click link scoped to an unsubscribe list. Rows of this
	// kind carry an UnsubscribeListID and block only that list.
	KindListUnsubscribe = "list_unsubscribe"
)

// ValidKinds enumerates every kind a row can carry, which is what the
// list FILTER accepts. It is one wider than what a caller may CREATE
// (the oneof tag on the create input): list_unsubscribe rows are
// written by the hosted one-click link, and the list endpoint showed
// that kind while refusing it as a filter value.
var ValidKinds = map[string]struct{}{
	KindHard: {}, KindBounce: {}, KindComplaint: {}, KindManual: {},
	KindListUnsubscribe: {},
}

// Suppression is one blocked address, global or scoped to a list.
type Suppression struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Email     string `json:"email"`
	Kind      string `json:"kind"`
	Reason    string `json:"reason,omitempty"`

	// UnsubscribeListID scopes the block to one opt-out list. Empty
	// means a global block covering every send from the project.
	// A row is unique on (project, email, list), so an address can
	// be globally blocked and separately opted out of one list.
	UnsubscribeListID string    `json:"unsubscribe_list_id,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}

// Global reports whether this row blocks every send rather than one list.
func (s *Suppression) Global() bool { return s.UnsubscribeListID == "" }
