// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package domains

import (
	dmodel "github.com/yousysadmin/mailyard/internal/models/domain"
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
	Domain string `json:"domain" validate:"required,fqdn,max=253" normalize:"normalize"`
}

// ----------------------------------------------------------------------------
// Responses
// ----------------------------------------------------------------------------

// ListResponse is the project's claimed domains. The DKIM private
// half is sealed at rest and carries json:"-", so it never reaches
// here.
type ListResponse struct {
	Domains []*dmodel.Domain `json:"domains"`
}

// DetailResponse is one domain plus every DNS record the operator has
// to publish, each with its current state, so the console never has to
// assemble them.
//
// A declared type rather than a fiber.Map, for two reasons. Three routes
// answer this shape - Get, Create and Verify - and as a map all three
// document themselves as returning nothing, leaving the generated
// clients with no schema for the one response an operator has to act on.
// Reflection also cannot check a map for secrets, which matters on a
// type carrying a domain row: json:"-" keeps the DKIM private half off
// the wire, and that tag would be the only thing anyone verified.
type DetailResponse struct {
	Domain     *dmodel.Domain `json:"domain"`
	DNSRecords []Record       `json:"dns_records"`
}
