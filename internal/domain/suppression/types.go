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
	Kind   string `json:"kind"   validate:"omitempty,oneof=bounce complaint manual"`
	Reason string `json:"reason" validate:"omitempty,max=500"      normalize:"trim"`
}

// importInput is a block list brought in whole, from another
// provider or a spreadsheet. Each entry is what a single create
// takes, and one malformed address refuses the whole body up front
// rather than leaving a list half applied.
type importInput struct {
	Suppressions []createInput `json:"suppressions" validate:"required,min=1,max=1000,dive"`
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

// ImportResponse counts what the import wrote. An address already
// blocked is written again, with the kind and reason sent, so the
// count is the list's length.
type ImportResponse struct {
	Imported int `json:"imported"`
}
