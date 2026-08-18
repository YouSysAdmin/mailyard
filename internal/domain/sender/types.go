// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package sender

import (
	smodel "github.com/yousysadmin/mailyard/internal/models/sender"
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
	Email string `json:"email" validate:"required,email,max=320" normalize:"normalize"`
	Name  string `json:"name"  validate:"omitempty,max=100" normalize:"trim"`
}

// ----------------------------------------------------------------------------
// Responses
// ----------------------------------------------------------------------------

// ListResponse is the project's approved From addresses.
type ListResponse struct {
	Senders []*smodel.Sender `json:"senders"`
}

// SenderResponse is one approved address.
type SenderResponse struct {
	Sender *smodel.Sender `json:"sender"`
}
