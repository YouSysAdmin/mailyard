// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package audit

import (
	"time"

	amodel "github.com/yousysadmin/mailyard/internal/models/audit"
)

// The wire types of this domain: what requests carry in and what
// responses carry out, in one file.
//
// They live here rather than beside the handlers that use them so a
// reader answering "what does this endpoint accept and return" has one
// place to look. The types in internal/models are the stored shapes -
// these are what crosses the wire, and the two are allowed to differ.

// ----------------------------------------------------------------------------
// Responses
// ----------------------------------------------------------------------------

// ListResponse is one offset page of the trail. It echoes the window
// applied rather than a total: the table grows per request, and
// counting it would be a full scan for a number nobody acts on.
type ListResponse struct {
	Events []*amodel.Event `json:"events"`
	Limit  int             `json:"limit"`
	Offset int             `json:"offset"`
}

// EventResponse is one recorded event.
type EventResponse struct {
	Event *amodel.Event `json:"event"`
}

// ExportResponse is the trail over a window, unpaged.
//
// It echoes the WINDOW it applied, because both ends are optional and a
// file whose contents depend on defaults the caller did not pass is a
// file nobody can check. And it says when it was CUT: an export that
// quietly stops at a ceiling is the one truncation that matters, since
// the whole point is having the record somewhere else.
type ExportResponse struct {
	Events    []*amodel.Event `json:"events"`
	From      time.Time       `json:"from"`
	To        time.Time       `json:"to"`
	Count     int             `json:"count"`
	Truncated bool            `json:"truncated"`

	// Cap is what Truncated was measured against, so a caller that hit
	// it can narrow the window rather than guess at the limit.
	Cap int `json:"cap"`
}
