// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package inbound

import (
	imodel "github.com/yousysadmin/mailyard/internal/models/inbound"
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

// ListResponse is a page of received mail.
type ListResponse struct {
	InboundEmails []*imodel.Email `json:"inbound_emails"`
}

// StatsResponse counts received mail by status.
type StatsResponse struct {
	Counts map[string]int `json:"counts"`
}

// GetResponse is one received message.
type GetResponse struct {
	InboundEmail *imodel.Email `json:"inbound_email"`
}

// EmittedResponse confirms a message was re-dispatched to the
// project's webhook.
type EmittedResponse struct {
	Emitted bool `json:"emitted"`
}
