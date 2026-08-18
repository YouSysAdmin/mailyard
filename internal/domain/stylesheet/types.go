// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package stylesheet

import (
	tmodel "github.com/yousysadmin/mailyard/internal/models/stylesheet"
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

type upsertInput struct {
	Name string `json:"name" validate:"required,min=1,max=100" normalize:"trim"`
	CSS  string `json:"css"  validate:"omitempty,max=262144"`
}

// ----------------------------------------------------------------------------
// Responses
// ----------------------------------------------------------------------------

// ListResponse is the project's stylesheets.
type ListResponse struct {
	Stylesheets []*tmodel.Stylesheet `json:"stylesheets"`
}

// StylesheetResponse is one stylesheet.
type StylesheetResponse struct {
	Stylesheet *tmodel.Stylesheet `json:"stylesheet"`
}
