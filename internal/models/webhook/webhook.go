// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package webhook is the outgoing event webhook: a URL that receives
// signed JSON POSTs for the email lifecycle events it subscribes to,
// plus the per-attempt delivery log.
package webhook

import "time"

// Event names emitted by the send pipeline, the campaign runner and
// the inbound listener.
const (
	EventEmailQueued       = "email.queued"
	EventEmailSent         = "email.sent"
	EventEmailFailed       = "email.failed"
	EventEmailSuppressed   = "email.suppressed"
	EventCampaignStarted   = "campaign.started"
	EventCampaignCompleted = "campaign.completed"
	EventInboundReceived   = "inbound.received"
)

// ValidEvents enumerates subscribable events for input validation.
var ValidEvents = map[string]struct{}{
	EventEmailQueued: {}, EventEmailSent: {}, EventEmailFailed: {}, EventEmailSuppressed: {},
	EventCampaignStarted: {}, EventCampaignCompleted: {}, EventInboundReceived: {},
}

// Webhook is one subscription. Secret signs every delivery
// (X-Mailyard-Signature, HMAC-SHA256 over the raw body) and is returned
// exactly once on create. Filters optionally narrow deliveries to
// matching sender addresses (exact or *@domain).
type Webhook struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	CreatedBy string    `json:"created_by,omitempty"`
	URL       string    `json:"url"`
	Events    []string  `json:"events"`
	Filters   []string  `json:"filters"`
	Secret    string    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
}

// Subscribed reports whether the webhook wants this event.
func (w *Webhook) Subscribed(event string) bool {
	for _, e := range w.Events {
		if e == event || e == "*" {
			return true
		}
	}

	return false
}

// Delivery statuses.
const (
	DeliverySuccess = "success"
	DeliveryFailed  = "failed"
)

// Delivery is one POST attempt against a webhook.
type Delivery struct {
	ID           string    `json:"id"`
	WebhookID    string    `json:"webhook_id"`
	ProjectID    string    `json:"project_id"`
	Event        string    `json:"event"`
	Status       string    `json:"status"`
	HTTPStatus   int       `json:"http_status,omitzero"`
	ErrorMessage string    `json:"error_message,omitempty"`
	Attempt      int       `json:"attempt"`
	CreatedAt    time.Time `json:"created_at"`
}
