// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package mailyard

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Send queues one email.
//
// Delivery is asynchronous: this returns as soon as the message is
// stored, and the returned id is what you poll with Status. Suppressed
// recipients come back in the result rather than failing the call - the
// send still happened for everyone else.
//
// Set req.DryRun and use SendDryRun instead: the server answers a
// different body for it.
func (c *Client) Send(ctx context.Context, req SendRequest) (*SendResult, error) {
	req.DryRun = false
	out, err := do[*SendResult](ctx, c, http.MethodPost, "/emails/send", nil, req)

	return out, err
}

// SendDryRun validates and renders a message without storing or
// sending it.
func (c *Client) SendDryRun(ctx context.Context, req SendRequest) (*DryRunResult, error) {
	req.DryRun = true

	return do[*DryRunResult](ctx, c, http.MethodPost, "/emails/send", nil, req)
}

// SendTemplate renders a stored template and queues the result.
func (c *Client) SendTemplate(ctx context.Context, req TemplateSendRequest) (*SendResult, error) {
	req.DryRun = false

	return do[*SendResult](ctx, c, http.MethodPost, "/emails/send-template", nil, req)
}

// SendBatch queues up to 100 messages in one call.
//
// One bad item does not sink the rest: the call succeeds and the
// result reports each item's outcome, matched back by Index. Check
// Accepted against Total rather than assuming success.
//
// A sandbox-flagged key is refused here rather than delivering for
// real - see the API description.
func (c *Client) SendBatch(ctx context.Context, req BatchRequest) (*BatchResponse, error) {
	return do[*BatchResponse](ctx, c, http.MethodPost, "/emails/batch", nil, req)
}

// Preview renders a template without sending anything.
func (c *Client) Preview(ctx context.Context, req PreviewRequest) (*Preview, error) {
	return do[*Preview](ctx, c, http.MethodPost, "/emails/preview", nil, req)
}

// PreviewRequest names a template and the data to render it against.
type PreviewRequest struct {
	TemplateID   string         `json:"template_id,omitempty"`
	TemplateName string         `json:"template_name,omitempty"`
	Language     string         `json:"language,omitempty"`
	Data         map[string]any `json:"data,omitempty"`
}

// Verify judges whether an address is worth sending to.
//
// Pass fresh to skip the cache and re-run the DNS checks. Note
// MailboxVerified is permanently false: there is deliberately no SMTP
// probe, because probing gets the sending IP blocklisted.
func (c *Client) Verify(ctx context.Context, email string, fresh bool) (*Verification, error) {
	q := url.Values{}
	if fresh {
		q.Set("fresh", "true")
	}

	body := struct {
		Email string `json:"email"`
	}{Email: email}
	out, err := do[struct {
		Verification Verification `json:"verification"`
	}](ctx, c, http.MethodPost, "/emails/verify", q, body)
	if err != nil {
		return nil, err
	}

	return &out.Verification, nil
}

// ListEmails returns a page of the email log, newest first.
func (c *Client) ListEmails(ctx context.Context, f EmailFilter) ([]Email, error) {
	q := url.Values{}
	if f.Status != "" {
		q.Set("status", f.Status)
	}

	if f.Before != nil {
		q.Set("before", f.Before.UTC().Format(time.RFC3339))
	}

	if f.Limit > 0 {
		q.Set("limit", strconv.Itoa(f.Limit))
	}

	out, err := do[struct {
		Emails []Email `json:"emails"`
	}](ctx, c, http.MethodGet, "/emails", q, nil)
	if err != nil {
		return nil, err
	}

	return out.Emails, nil
}

// EmailStats counts emails by delivery status.
func (c *Client) EmailStats(ctx context.Context) (map[string]int, error) {
	out, err := do[struct {
		Counts map[string]int `json:"counts"`
	}](ctx, c, http.MethodGet, "/emails/stats", nil, nil)
	if err != nil {
		return nil, err
	}

	return out.Counts, nil
}

// Limits reports what a send may carry on this installation, so a
// client can validate before paying for a round trip.
func (c *Client) Limits(ctx context.Context) (*Limits, error) {
	out, err := do[struct {
		Limits Limits `json:"limits"`
	}](ctx, c, http.MethodGet, "/emails/limits", nil, nil)
	if err != nil {
		return nil, err
	}

	return &out.Limits, nil
}

// GetEmail returns one message with its full delivery record.
func (c *Client) GetEmail(ctx context.Context, id string) (*Email, error) {
	out, err := do[struct {
		Email Email `json:"email"`
	}](ctx, c, http.MethodGet, "/emails/"+url.PathEscape(id), nil, nil)
	if err != nil {
		return nil, err
	}

	return &out.Email, nil
}

// Status is the cheap poll: delivery state without the bodies. Prefer
// it over GetEmail when you are waiting for a message to finish.
func (c *Client) Status(ctx context.Context, id string) (*EmailStatus, error) {
	return do[*EmailStatus](ctx, c, http.MethodGet, "/emails/"+url.PathEscape(id)+"/status", nil, nil)
}

// Retry requeues a failed message.
func (c *Client) Retry(ctx context.Context, id string) (*Email, error) {
	out, err := do[struct {
		Email Email `json:"email"`
	}](ctx, c, http.MethodPost, "/emails/"+url.PathEscape(id)+"/retry", nil, nil)
	if err != nil {
		return nil, err
	}

	return &out.Email, nil
}
