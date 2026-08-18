// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package unsubscribelist

import (
	ulmodel "github.com/yousysadmin/mailyard/internal/models/unsubscribelist"
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
	Name        string `json:"name"        validate:"required,min=1,max=100" normalize:"trim"`
	PublicName  string `json:"public_name" validate:"omitempty,max=100"      normalize:"trim"`
	Description string `json:"description" validate:"omitempty,max=500"      normalize:"trim"`
}

type updateInput struct {
	Name        string  `json:"name"        validate:"omitempty,min=1,max=100" normalize:"trim"`
	PublicName  *string `json:"public_name" validate:"omitzero,max=100"`
	Description *string `json:"description" validate:"omitzero,max=500"`
	Active      *bool   `json:"active"`
}

// ----------------------------------------------------------------------------
// Responses
// ----------------------------------------------------------------------------

// ListResponse is every opt-out scope in the project.
type ListResponse struct {
	UnsubscribeLists []*ulmodel.List `json:"unsubscribe_lists"`
}

// GetResponse is one opt-out scope.
type GetResponse struct {
	UnsubscribeList *ulmodel.List `json:"unsubscribe_list"`
}
