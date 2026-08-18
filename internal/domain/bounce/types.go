// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package bounce

import (
	bmodel "github.com/yousysadmin/mailyard/internal/models/bounce"
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

// ingestInput is the POST /api/v1/webhooks/bounce body, for upstream
// providers or MTAs reporting asynchronous bounces.
type ingestInput struct {
	Recipient string `json:"recipient" validate:"required,email,max=320" normalize:"normalize"`
	EmailID   string `json:"email_id"  validate:"omitempty,uuid"`
	Type      string `json:"type"      validate:"omitempty,oneof=hard soft complaint"`
	Reason    string `json:"reason"    validate:"omitempty,max=1000"     normalize:"trim"`
}

// ----------------------------------------------------------------------------
// Responses
// ----------------------------------------------------------------------------

// ListResponse is one keyset page of bounce reports.
type ListResponse struct {
	Bounces    []*bmodel.Bounce `json:"bounces"`
	NextCursor string           `json:"next_cursor"`
}

// IngestResponse reports the recorded bounce and whether it also put
// the address on the suppression list, which depends on the bounce
// being permanent and the project having auto-suppression on.
type IngestResponse struct {
	Bounce     *bmodel.Bounce `json:"bounce"`
	Suppressed bool           `json:"suppressed"`
}

// DeleteResponse says how many reports went.
//
// A COUNT rather than 204, because the caller asked about an address and
// an address has as many reports as there were attempts - "deleted 4"
// tells an operator the history is gone, where silence leaves them
// wondering whether to press it again.
//
// It deliberately says nothing about the suppression list. Erasing the
// history does not unblock the address: the block is a suppression row,
// removed through DELETE /suppressions, and reporting a field about it
// here would suggest this call had touched it.
type DeleteResponse struct {
	Deleted int `json:"deleted"`
}
