// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package eventstream

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

// StatsResponse is the live-stream health readout.
//
// Dropped counts events discarded because a subscriber stopped
// reading. A number that climbs steadily means some browser is not
// keeping up - the events are dropped rather than buffered without
// bound, which is the trade a database-backed system makes over
// running a broker.
type StatsResponse struct {
	Subscribers int    `json:"subscribers"`
	Projects    int    `json:"projects"`
	Dropped     uint64 `json:"dropped"`
}
