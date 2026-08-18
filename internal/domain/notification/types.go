// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package notification

import (
	nmodel "github.com/yousysadmin/mailyard/internal/models/notification"
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

// ListResponse is one page of in-app alerts.
//
// Unread rides along because the console polls the badge far more
// often than it opens the list, and a second round trip for one
// integer is the request nobody should be making.
type ListResponse struct {
	Notifications []*nmodel.Notification `json:"notifications"`
	Unread        int                    `json:"unread"`
	Limit         int                    `json:"limit"`
	Offset        int                    `json:"offset"`
}

// UnreadResponse is the badge count on its own.
type UnreadResponse struct {
	Unread int `json:"unread"`
}

// ReadResponse confirms one alert was marked read. Read state is
// shared across the project, so this clears it for every member.
type ReadResponse struct {
	Read bool `json:"read"`
}

// MarkedResponse reports how many alerts a mark-all cleared.
type MarkedResponse struct {
	Marked int64 `json:"marked"`
}
