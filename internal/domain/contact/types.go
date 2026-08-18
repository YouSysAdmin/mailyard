// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package contact

import (
	cmodel "github.com/yousysadmin/mailyard/internal/models/contact"
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

// ListResponse is one offset page of contacts. Unlike the message
// logs this list is bounded by how many people the project has mailed,
// so it carries a total.
type ListResponse struct {
	Contacts []*cmodel.Contact `json:"contacts"`
	Total    int               `json:"total"`
	Limit    int               `json:"limit"`
	Offset   int               `json:"offset"`
}

// GetResponse is one contact.
type GetResponse struct {
	Contact *cmodel.Contact `json:"contact"`
}
