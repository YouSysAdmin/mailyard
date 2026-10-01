// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package webhook

import (
	wmodel "github.com/yousysadmin/mailyard/internal/models/webhook"
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
	URL     string   `json:"url"     validate:"required,url,startswith=http,max=2048" normalize:"trim"`
	Events  []string `json:"events"  validate:"required,min=1,max=10"`
	Filters []string `json:"filters" validate:"omitempty,max=20,dive,min=3,max=320"`
}

// ----------------------------------------------------------------------------
// Responses
// ----------------------------------------------------------------------------

// ListResponse is every webhook in the project.
// updateInput changes what is sent and leaves the rest. Filters is a
// list even when empty, so a caller can clear it, and absent when not
// sent.
type updateInput struct {
	URL     string   `json:"url"     validate:"omitempty,url,startswith=http,max=2048" normalize:"trim"`
	Events  []string `json:"events"  validate:"omitempty,min=1,max=10"`
	Filters []string `json:"filters" validate:"omitempty,max=20,dive,min=3,max=320"`
}

// disableInput is the reason recorded on the hook, shown wherever the
// dispatcher's own reason would be.
type disableInput struct {
	Reason string `json:"reason" validate:"omitempty,max=500" normalize:"trim"`
}

type ListResponse struct {
	Webhooks []*wmodel.Webhook `json:"webhooks"`
}

// WebhookResponse is one webhook, without its secret.
type WebhookResponse struct {
	Webhook *wmodel.Webhook `json:"webhook"`
}

// DeliveryResponse is one attempt, the one a test or a redelivery
// just made.
type DeliveryResponse struct {
	Delivery *wmodel.Delivery `json:"delivery"`
}

// CreateResponse carries the signing secret, which appears here and
// nowhere else in the API - the model has it `json:"-"`, so no later
// read returns it.
//
// It is not hashed and cannot be: the dispatcher signs each payload with
// the same value the receiver verifies with, so a one-way hash would
// leave nothing to sign. It is sealed instead, through crypto.Service,
// exactly like the SMTP password - see NewStore. In the clear a
// database dump
// yielded every project's HMAC key and with it the ability to forge
// X-Mailyard-Signature on payloads to a customer's endpoint.
type CreateResponse struct {
	Webhook *wmodel.Webhook `json:"webhook"`
	Secret  string          `json:"secret"`
}

// EnableResponse is the hook after it was put back into rotation.
type EnableResponse struct {
	Webhook *wmodel.Webhook `json:"webhook"`
}

// DeliveriesResponse is one keyset page of delivery attempts.
type DeliveriesResponse struct {
	Deliveries []*wmodel.Delivery `json:"deliveries"`
	NextCursor string             `json:"next_cursor"`
}
