// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package subscriber

import (
	"github.com/gofiber/fiber/v2"
	smodel "github.com/yousysadmin/mailyard/internal/models/subscriber"
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
	Email        string         `json:"email"         validate:"required,email,max=320" normalize:"normalize"`
	Name         string         `json:"name"          validate:"omitempty,max=200"      normalize:"trim"`
	Status       string         `json:"status"        validate:"omitempty,oneof=subscribed unsubscribed bounced complained"`
	CustomFields map[string]any `json:"custom_fields" validate:"omitempty,max=50"`
	Timezone     string         `json:"timezone"      validate:"omitempty,max=64"       normalize:"trim"`
	Language     string         `json:"language"      validate:"omitempty,min=2,max=10" normalize:"normalize"`
}

// importInput is the POST /api/subscribers/import body: bulk upsert
// by email.
type importInput struct {
	Subscribers []upsertInput `json:"subscribers" validate:"required,min=1,max=10000,dive"`
}

// ----------------------------------------------------------------------------
// Responses
// ----------------------------------------------------------------------------

// ListResponse is one offset page of subscribers, with the total that
// bounds it.
type ListResponse struct {
	Subscribers []*smodel.Subscriber `json:"subscribers"`
	Total       int                  `json:"total"`
}

// SubscriberResponse is one subscriber.
type SubscriberResponse struct {
	Subscriber *smodel.Subscriber `json:"subscriber"`
}

// ImportResponse reports what a bulk import did, per row.
//
// Errors names the rows that were refused rather than failing the
// whole import: a single bad address in a ten thousand row file should
// not cost the other nine thousand.
type ImportResponse struct {
	Created int         `json:"created"`
	Updated int         `json:"updated"`
	Skipped int         `json:"skipped"`
	Errors  []fiber.Map `json:"errors"`
}
