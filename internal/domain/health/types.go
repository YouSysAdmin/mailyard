// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package health

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

// StatusResponse is the liveness answer.
//
// The json tag is "status", matching the sibling probes. No caller in
// the tree reads this endpoint, which is exactly why a wrong key here
// would survive: one nobody consumes cannot break anything.
type StatusResponse struct {
	Status string `json:"status"`
}

// ReadyResponse is the readiness answer: the verdict plus which
// dependency produced it, so an operator reading a 503 does not have
// to guess.
type ReadyResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}
