// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package suppression

import (
	supmodel "github.com/yousysadmin/mailyard/internal/models/suppression"
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

type createInput struct {
	Email  string `json:"email"  validate:"required,email,max=320" normalize:"normalize"`
	Kind   string `json:"kind"   validate:"omitempty,oneof=hard bounce complaint manual"`
	Reason string `json:"reason" validate:"omitempty,max=500"      normalize:"trim"`
}

// ----------------------------------------------------------------------------
// Responses
// ----------------------------------------------------------------------------

// ListResponse is one keyset page of blocked addresses.
//
// There is deliberately no total: COUNT(*) over a table that is never
// pruned is a full index scan per page load. NextCursor being non-empty
// is the answer to "is there more".
type ListResponse struct {
	Suppressions []*supmodel.Suppression `json:"suppressions"`
	NextCursor   string                  `json:"next_cursor"`
}

// CreateResponse is the row that now blocks the address.
type CreateResponse struct {
	Suppression *supmodel.Suppression `json:"suppression"`
}
