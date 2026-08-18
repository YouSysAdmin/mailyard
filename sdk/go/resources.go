// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package mailyard

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// --- templates -------------------------------------------------------

// ListTemplates returns the project's templates. They are authored in
// the console, so this surface only reads them.
func (c *Client) ListTemplates(ctx context.Context, limit, offset int) ([]Template, error) {
	out, err := do[struct {
		Templates []Template `json:"templates"`
	}](ctx, c, http.MethodGet, "/templates", page(limit, offset), nil)
	if err != nil {
		return nil, err
	}

	return out.Templates, nil
}

// GetTemplate returns one template with its version history, so you
// can pin a version without a second call.
func (c *Client) GetTemplate(ctx context.Context, id string) (*Template, []TemplateVersion, error) {
	out, err := do[struct {
		Template Template          `json:"template"`
		Versions []TemplateVersion `json:"versions"`
	}](ctx, c, http.MethodGet, "/templates/"+url.PathEscape(id), nil, nil)
	if err != nil {
		return nil, nil, err
	}

	return &out.Template, out.Versions, nil
}

// --- suppressions ----------------------------------------------------

// ListSuppressions returns one page of blocked addresses.
//
// Cursor paged: pass the returned cursor back to walk forward, and
// stop when it comes back empty. There is no total by design.
func (c *Client) ListSuppressions(ctx context.Context, f SuppressionFilter) ([]Suppression, string, error) {
	q := url.Values{}
	if f.Kind != "" {
		q.Set("kind", f.Kind)
	}

	if f.Search != "" {
		q.Set("search", f.Search)
	}

	if f.Limit > 0 {
		q.Set("limit", strconv.Itoa(f.Limit))
	}

	if f.Cursor != "" {
		q.Set("cursor", f.Cursor)
	}

	out, err := do[struct {
		Suppressions []Suppression `json:"suppressions"`
		NextCursor   string        `json:"next_cursor"`
	}](ctx, c, http.MethodGet, "/suppressions", q, nil)
	if err != nil {
		return nil, "", err
	}

	return out.Suppressions, out.NextCursor, nil
}

// Suppress blocks an address. Kind may be empty, which means manual.
func (c *Client) Suppress(ctx context.Context, email, kind, reason string) (*Suppression, error) {
	body := struct {
		Email  string `json:"email"`
		Kind   string `json:"kind,omitempty"`
		Reason string `json:"reason,omitempty"`
	}{email, kind, reason}
	out, err := do[struct {
		Suppression Suppression `json:"suppression"`
	}](ctx, c, http.MethodPost, "/suppressions", nil, body)
	if err != nil {
		return nil, err
	}

	return &out.Suppression, nil
}

// Unsuppress unblocks an address.
func (c *Client) Unsuppress(ctx context.Context, email string) error {
	q := url.Values{"email": {email}}
	_, err := do[struct{}](ctx, c, http.MethodDelete, "/suppressions", q, nil)

	return err
}

// --- bounces ---------------------------------------------------------

// ListBounces returns one page of bounce reports, newest first.
func (c *Client) ListBounces(ctx context.Context, f BounceFilter) ([]Bounce, string, error) {
	q := url.Values{}
	if f.Type != "" {
		q.Set("type", f.Type)
	}

	if f.Search != "" {
		q.Set("search", f.Search)
	}

	if f.Limit > 0 {
		q.Set("limit", strconv.Itoa(f.Limit))
	}

	if f.Cursor != "" {
		q.Set("cursor", f.Cursor)
	}

	out, err := do[struct {
		Bounces    []Bounce `json:"bounces"`
		NextCursor string   `json:"next_cursor"`
	}](ctx, c, http.MethodGet, "/bounces", q, nil)
	if err != nil {
		return nil, "", err
	}

	return out.Bounces, out.NextCursor, nil
}

// ReportBounce files a delivery failure you learned about yourself.
//
// The report must be attributable: EmailID has to name a message this
// project sent and Recipient has to be one that message went to.
// Anything else is refused rather than filed against the wrong tenant.
// The second return says whether the address was also suppressed.
func (c *Client) ReportBounce(ctx context.Context, r BounceReport) (*Bounce, bool, error) {
	out, err := do[struct {
		Bounce     Bounce `json:"bounce"`
		Suppressed bool   `json:"suppressed"`
	}](ctx, c, http.MethodPost, "/webhooks/bounce", nil, r)
	if err != nil {
		return nil, false, err
	}

	return &out.Bounce, out.Suppressed, nil
}

// --- webhooks --------------------------------------------------------

// ListWebhooks returns the project's outgoing webhooks.
func (c *Client) ListWebhooks(ctx context.Context) ([]Webhook, error) {
	out, err := do[struct {
		Webhooks []Webhook `json:"webhooks"`
	}](ctx, c, http.MethodGet, "/webhooks", nil, nil)
	if err != nil {
		return nil, err
	}

	return out.Webhooks, nil
}

// CreateWebhook registers an outgoing webhook and returns it together
// with its signing secret.
//
// The secret is returned HERE AND NOWHERE ELSE - only its hash is
// stored. Save it now or create a new webhook later.
func (c *Client) CreateWebhook(ctx context.Context, r WebhookRequest) (*Webhook, string, error) {
	out, err := do[struct {
		Webhook Webhook `json:"webhook"`
		Secret  string  `json:"secret"`
	}](ctx, c, http.MethodPost, "/webhooks", nil, r)
	if err != nil {
		return nil, "", err
	}

	return &out.Webhook, out.Secret, nil
}

// DeleteWebhook removes a webhook.
func (c *Client) DeleteWebhook(ctx context.Context, id string) error {
	_, err := do[struct{}](ctx, c, http.MethodDelete, "/webhooks/"+url.PathEscape(id), nil, nil)

	return err
}

// WebhookDeliveries returns one page of delivery attempts for a
// webhook, cursor paged.
func (c *Client) WebhookDeliveries(ctx context.Context, id string, limit int, cursor string) ([]WebhookDelivery, string, error) {
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}

	if cursor != "" {
		q.Set("cursor", cursor)
	}

	out, err := do[struct {
		Deliveries []WebhookDelivery `json:"deliveries"`
		NextCursor string            `json:"next_cursor"`
	}](ctx, c, http.MethodGet, "/webhooks/"+url.PathEscape(id)+"/deliveries", q, nil)
	if err != nil {
		return nil, "", err
	}

	return out.Deliveries, out.NextCursor, nil
}

// --- insight ---------------------------------------------------------

// DashboardStats returns the aggregate counts for the project.
func (c *Client) DashboardStats(ctx context.Context) (*AnalyticsSummary, error) {
	out, err := do[struct {
		Stats AnalyticsSummary `json:"stats"`
	}](ctx, c, http.MethodGet, "/dashboard/stats", nil, nil)
	if err != nil {
		return nil, err
	}

	return &out.Stats, nil
}

// Analytics returns the delivery trend. from and to are YYYY-MM-DD and
// may both be empty for the default trailing 30 days.
func (c *Client) Analytics(ctx context.Context, from, to string) (*Analytics, error) {
	q := url.Values{}
	if from != "" {
		q.Set("from", from)
	}

	if to != "" {
		q.Set("to", to)
	}

	return do[*Analytics](ctx, c, http.MethodGet, "/analytics", q, nil)
}

// ListContacts returns one offset page of addresses the project has
// delivered to. Suppressed is resolved on read, so it never disagrees
// with the suppression list.
func (c *Client) ListContacts(ctx context.Context, search string, limit, offset int) ([]Contact, int, error) {
	q := page(limit, offset)
	if search != "" {
		q.Set("search", search)
	}

	out, err := do[struct {
		Contacts []Contact `json:"contacts"`
		Total    int       `json:"total"`
	}](ctx, c, http.MethodGet, "/contacts", q, nil)
	if err != nil {
		return nil, 0, err
	}

	return out.Contacts, out.Total, nil
}

// GetContact returns one contact.
func (c *Client) GetContact(ctx context.Context, id string) (*Contact, error) {
	out, err := do[struct {
		Contact Contact `json:"contact"`
	}](ctx, c, http.MethodGet, "/contacts/"+url.PathEscape(id), nil, nil)
	if err != nil {
		return nil, err
	}

	return &out.Contact, nil
}

// --- unsubscribe lists -----------------------------------------------

// ListUnsubscribeLists returns the project's transactional opt-out
// scopes. Pass a list id as SendRequest.UnsubscribeListID and Mailyard
// mints the one-click link and filters against that list.
func (c *Client) ListUnsubscribeLists(ctx context.Context) ([]UnsubscribeList, error) {
	out, err := do[struct {
		Lists []UnsubscribeList `json:"unsubscribe_lists"`
	}](ctx, c, http.MethodGet, "/unsubscribe-lists", nil, nil)
	if err != nil {
		return nil, err
	}

	return out.Lists, nil
}

// GetUnsubscribeList returns one opt-out scope.
func (c *Client) GetUnsubscribeList(ctx context.Context, id string) (*UnsubscribeList, error) {
	out, err := do[struct {
		List UnsubscribeList `json:"unsubscribe_list"`
	}](ctx, c, http.MethodGet, "/unsubscribe-lists/"+url.PathEscape(id), nil, nil)
	if err != nil {
		return nil, err
	}

	return &out.List, nil
}

// --- inbound ---------------------------------------------------------

// ListInbound returns a page of mail received by the MX listener.
func (c *Client) ListInbound(ctx context.Context, status string, limit int) ([]InboundEmail, error) {
	q := url.Values{}
	if status != "" {
		q.Set("status", status)
	}

	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}

	out, err := do[struct {
		Emails []InboundEmail `json:"inbound_emails"`
	}](ctx, c, http.MethodGet, "/inbound-emails", q, nil)
	if err != nil {
		return nil, err
	}

	return out.Emails, nil
}

// InboundStats counts received mail by status.
func (c *Client) InboundStats(ctx context.Context) (map[string]int, error) {
	out, err := do[struct {
		Counts map[string]int `json:"counts"`
	}](ctx, c, http.MethodGet, "/inbound-emails/stats", nil, nil)
	if err != nil {
		return nil, err
	}

	return out.Counts, nil
}

// GetInbound returns one received message. Check Auth.Aligned rather
// than the individual verdicts: a valid signature from some other
// domain is not authentication.
func (c *Client) GetInbound(ctx context.Context, id string) (*InboundEmail, error) {
	out, err := do[struct {
		Email InboundEmail `json:"inbound_email"`
	}](ctx, c, http.MethodGet, "/inbound-emails/"+url.PathEscape(id), nil, nil)
	if err != nil {
		return nil, err
	}

	return &out.Email, nil
}

// --- subscriber lists ------------------------------------------------

// Subscribe adds an address to a static list, creating the subscriber
// when the address is new. Subscribing twice is not an error.
func (c *Client) Subscribe(ctx context.Context, r SubscribeRequest) (*Subscriber, error) {
	out, err := do[struct {
		Subscriber Subscriber `json:"subscriber"`
	}](ctx, c, http.MethodPost, "/subscriber-lists/subscribe", nil, r)
	if err != nil {
		return nil, err
	}

	return &out.Subscriber, nil
}

// Unsubscribe removes an address from a list.
func (c *Client) Unsubscribe(ctx context.Context, listID, email, reason string) error {
	body := struct {
		Email  string `json:"email"`
		Reason string `json:"reason,omitempty"`
	}{email, reason}
	_, err := do[struct{}](ctx, c, http.MethodPost,
		"/subscriber-lists/"+url.PathEscape(listID)+"/unsubscribe", nil, body)

	return err
}

// Resubscribe re-adds an address that had opted out.
func (c *Client) Resubscribe(ctx context.Context, listID, email string) error {
	body := struct {
		Email string `json:"email"`
	}{email}
	_, err := do[struct{}](ctx, c, http.MethodPost,
		"/subscriber-lists/"+url.PathEscape(listID)+"/resubscribe", nil, body)

	return err
}

// --- meta ------------------------------------------------------------

// Export returns the project's full data snapshot.
//
// Needs a key with the admin scope: one call returns every tenant
// record at once. No secret is included. The document is returned as
// raw JSON because its sections are composed from every domain and
// pinning them to types here would be a second definition to keep in
// step.
func (c *Client) Export(ctx context.Context) (map[string]any, error) {
	out, err := do[struct {
		Export map[string]any `json:"export"`
	}](ctx, c, http.MethodGet, "/data/export", nil, nil)
	if err != nil {
		return nil, err
	}

	return out.Export, nil
}
