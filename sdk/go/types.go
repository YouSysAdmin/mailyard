// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package mailyard

import "time"

// Delivery statuses an Email can hold.
const (
	StatusPending    = "pending"
	StatusQueued     = "queued"
	StatusScheduled  = "scheduled"
	StatusProcessing = "processing"
	StatusSent       = "sent"
	StatusFailed     = "failed"
	StatusSuppressed = "suppressed"
)

// Attachment is one file on a message. Content is base64 of the raw
// bytes. On a message read back it may be empty when the bytes were
// offloaded to a blob store - the metadata still describes them.
type Attachment struct {
	Filename    string `json:"filename"`
	Content     string `json:"content,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Size        int64  `json:"size,omitzero"`
}

// SendRequest is one outbound message.
//
// Tracking, sandbox and routing are all opt-in per send and all
// interact with project-level settings - see the field comments,
// because the direction of the override differs.
type SendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html,omitempty"`
	Text    string   `json:"text,omitempty"`

	Headers     map[string]string `json:"headers,omitempty"`
	Attachments []Attachment      `json:"attachments,omitempty"`

	// SendAt schedules the message. RFC 3339.
	SendAt string `json:"send_at,omitempty"`

	// DryRun validates and renders without storing or sending. The
	// response is a DryRunResult, so use SendDryRun for it.
	DryRun bool `json:"dry_run,omitzero"`

	// UnsubscribeListID scopes the send to a transactional opt-out
	// list: Mailyard mints the one-click link and filters against the
	// list. Limited to ONE recipient, because the token identifies a
	// single address. Mutually exclusive with the ListUnsubscribe
	// fields below, which are the opposite arrangement.
	UnsubscribeListID string `json:"unsubscribe_list_id,omitempty"`

	// The RFC 2369 / 8058 targets, for an application running its own
	// opt-out. Mailyard carries the header and does nothing else with
	// it.
	ListUnsubscribeURL    string `json:"list_unsubscribe_url,omitempty"`
	ListUnsubscribeMailto string `json:"list_unsubscribe_mailto,omitempty"`
	ListUnsubscribePost   bool   `json:"list_unsubscribe_post,omitzero"`

	// SMTPGroup routes through a named server group by slug. Empty
	// uses the project's default group.
	SMTPGroup string `json:"smtp_group,omitempty"`

	// SMTPServerID pins one server exactly and NEVER falls back.
	SMTPServerID string `json:"smtp_server_id,omitempty"`

	// DisableTracking suppresses the open pixel and click rewriting
	// for this message. There is no "force on" counterpart: enabling
	// tracking is the project owner's decision, not a caller's.
	DisableTracking bool `json:"disable_tracking,omitzero"`

	// Sandbox captures the message instead of delivering it. A
	// POINTER because absent and false differ: a sandbox-flagged key
	// captures whatever this says, and an explicit false from such a
	// key is refused rather than obeyed.
	Sandbox *bool `json:"sandbox,omitempty"`

	// SandboxRetentionDays may only SHORTEN the platform window.
	SandboxRetentionDays int `json:"sandbox_retention_days,omitzero"`
}

// TemplateSendRequest renders a stored template and queues the
// result. Name the template by ID or Name, not both.
type TemplateSendRequest struct {
	From         string         `json:"from"`
	To           []string       `json:"to"`
	TemplateID   string         `json:"template_id,omitempty"`
	TemplateName string         `json:"template_name,omitempty"`
	Language     string         `json:"language,omitempty"`
	Data         map[string]any `json:"data,omitempty"`

	Headers     map[string]string `json:"headers,omitempty"`
	Attachments []Attachment      `json:"attachments,omitempty"`

	SendAt          string `json:"send_at,omitempty"`
	DryRun          bool   `json:"dry_run,omitzero"`
	DisableTracking bool   `json:"disable_tracking,omitzero"`

	SMTPGroup    string `json:"smtp_group,omitempty"`
	SMTPServerID string `json:"smtp_server_id,omitempty"`

	ListUnsubscribeURL    string `json:"list_unsubscribe_url,omitempty"`
	ListUnsubscribeMailto string `json:"list_unsubscribe_mailto,omitempty"`
	ListUnsubscribePost   bool   `json:"list_unsubscribe_post,omitzero"`

	Sandbox              *bool `json:"sandbox,omitempty"`
	SandboxRetentionDays int   `json:"sandbox_retention_days,omitzero"`
}

// BatchRequest queues up to 100 messages in one call. With a template
// reference every item renders it against its own Data. Without one,
// each item carries its own subject and body.
type BatchRequest struct {
	From         string      `json:"from"`
	TemplateID   string      `json:"template_id,omitempty"`
	TemplateName string      `json:"template_name,omitempty"`
	Language     string      `json:"language,omitempty"`
	Items        []BatchItem `json:"items"`
}

// BatchItem is one message in a batch.
type BatchItem struct {
	To       []string       `json:"to"`
	Language string         `json:"language,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
	Subject  string         `json:"subject,omitempty"`
	HTML     string         `json:"html,omitempty"`
	Text     string         `json:"text,omitempty"`

	// Per item, because an opt-out link identifies the recipient. One
	// link shared across a hundred items unsubscribes nobody in
	// particular.
	ListUnsubscribeURL    string `json:"list_unsubscribe_url,omitempty"`
	ListUnsubscribeMailto string `json:"list_unsubscribe_mailto,omitempty"`
	ListUnsubscribePost   bool   `json:"list_unsubscribe_post,omitzero"`
}

// Email is one outbound message and its delivery state.
type Email struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`

	Sender       string            `json:"sender"`
	Recipients   []string          `json:"recipients"`
	Subject      string            `json:"subject"`
	TemplateName string            `json:"template_name,omitempty"`
	HTMLBody     string            `json:"html_body,omitempty"`
	TextBody     string            `json:"text_body,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	Attachments  []Attachment      `json:"attachments,omitempty"`

	Status        string     `json:"status"`
	ErrorMessage  string     `json:"error_message,omitempty"`
	Attempts      int        `json:"attempts"`
	MaxAttempts   int        `json:"max_attempts"`
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty"`

	// Tracked is what makes the counters readable: without it a zero
	// OpenCount cannot be told apart from tracking never being on.
	Tracked    bool       `json:"tracked"`
	OpenedAt   *time.Time `json:"opened_at,omitempty"`
	ClickedAt  *time.Time `json:"clicked_at,omitempty"`
	OpenCount  int        `json:"open_count"`
	ClickCount int        `json:"click_count"`

	UnsubscribeListID     string `json:"unsubscribe_list_id,omitempty"`
	ListUnsubscribeURL    string `json:"list_unsubscribe_url,omitempty"`
	ListUnsubscribeMailto string `json:"list_unsubscribe_mailto,omitempty"`

	SMTPServerID string `json:"smtp_server_id,omitempty"`
	SMTPGroupID  string `json:"smtp_group_id,omitempty"`
	DeliveredVia string `json:"delivered_via,omitempty"`
	APIKeyID     string `json:"api_key_id,omitempty"`

	CreatedAt   time.Time  `json:"created_at"`
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
	SentAt      *time.Time `json:"sent_at,omitempty"`
}

// SendResult is what a successful send returns. Suppressed lists
// recipients dropped because they are blocked - the send still
// succeeded for the rest, so an empty slice is the normal case.
type SendResult struct {
	Email      Email    `json:"email"`
	Suppressed []string `json:"suppressed_recipients"`
}

// DryRunResult is what a send with DryRun set returns. Nothing was
// stored and nothing was sent.
type DryRunResult struct {
	DryRun     bool     `json:"dry_run"`
	Valid      bool     `json:"valid"`
	Recipients int      `json:"recipients"`
	Suppressed []string `json:"suppressed_recipients"`
}

// BatchResponse reports what happened to each item.
type BatchResponse struct {
	Total    int           `json:"total"`
	Accepted int           `json:"accepted"`
	Results  []BatchResult `json:"results"`
}

// BatchResult is one item's outcome. Error is empty on success, and
// Index matches the item's position in the request.
type BatchResult struct {
	Index      int      `json:"index"`
	EmailID    string   `json:"email_id,omitempty"`
	Status     string   `json:"status,omitempty"`
	Suppressed []string `json:"suppressed_recipients,omitempty"`
	Error      string   `json:"error,omitempty"`
}

// EmailStatus is the cheap poll: delivery state without the bodies.
type EmailStatus struct {
	ID           string     `json:"id"`
	Status       string     `json:"status"`
	Attempts     int        `json:"attempts"`
	ErrorMessage string     `json:"error_message,omitempty"`
	SentAt       *time.Time `json:"sent_at,omitempty"`
}

// RenderedMessage is a template rendered without being sent.
type RenderedMessage struct {
	Subject string `json:"subject"`
	HTML    string `json:"html"`
	Text    string `json:"text"`
}

// Preview is the rendered message plus the template it came from.
type Preview struct {
	Template string          `json:"template"`
	Preview  RenderedMessage `json:"preview"`
}

// Limits reports what a send may carry on this installation.
type Limits struct {
	MaxRecipients          int   `json:"max_recipients"`
	MaxAttachments         int   `json:"max_attachments"`
	MaxAttachmentSize      int64 `json:"max_attachment_size"`
	MaxTotalAttachmentSize int64 `json:"max_total_attachment_size"`
}

// Verification is the verdict on one address.
//
// MailboxVerified is permanently false: probing a mailbox over SMTP
// gets the sending IP blocklisted and providers accept-then-bounce
// anyway. Status "unknown" means a transient DNS failure and is not
// cached.
type Verification struct {
	Email  string `json:"email"`
	Status string `json:"status"`
	Score  int    `json:"score"`
	Reason string `json:"reason,omitempty"`
	Checks struct {
		Syntax      bool   `json:"syntax"`
		MX          bool   `json:"mx"`
		Disposable  bool   `json:"disposable"`
		RoleAccount bool   `json:"role_account"`
		SMTP        string `json:"smtp"`
	} `json:"checks"`
	MailboxVerified bool `json:"mailbox_verified"`

	// Suppressed and PreviouslyBounced are this project's own facts,
	// layered on every call, so they cannot go stale behind a cached
	// verdict.
	Suppressed        bool      `json:"suppressed"`
	PreviouslyBounced bool      `json:"previously_bounced"`
	Cached            bool      `json:"cached"`
	CheckedAt         time.Time `json:"checked_at"`
}

// Template is a stored template.
type Template struct {
	ID              string     `json:"id"`
	ProjectID       string     `json:"project_id"`
	Name            string     `json:"name"`
	Description     string     `json:"description,omitempty"`
	DefaultLanguage string     `json:"default_language"`
	ActiveVersionID *string    `json:"active_version_id,omitempty"`
	SampleData      string     `json:"sample_data,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       *time.Time `json:"updated_at,omitempty"`
}

// TemplateVersion is one revision of a template. The renderable
// content lives in the version's localizations, not on the version
// itself - a single-language template is one with one localization.
type TemplateVersion struct {
	ID           string    `json:"id"`
	TemplateID   string    `json:"template_id"`
	Version      int       `json:"version"`
	StylesheetID *string   `json:"stylesheet_id,omitempty"`
	SampleData   string    `json:"sample_data,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// Suppression kinds.
const (
	SuppressionHard      = "hard"
	SuppressionBounce    = "bounce"
	SuppressionComplaint = "complaint"
	SuppressionManual    = "manual"
)

// Suppression is one blocked address. UnsubscribeListID empty means a
// global block covering every send from the project.
type Suppression struct {
	ID                string    `json:"id"`
	ProjectID         string    `json:"project_id"`
	Email             string    `json:"email"`
	Kind              string    `json:"kind"`
	Reason            string    `json:"reason,omitempty"`
	UnsubscribeListID string    `json:"unsubscribe_list_id,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}

// Bounce is one delivery failure report.
type Bounce struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	EmailID   string    `json:"email_id,omitempty"`
	Recipient string    `json:"recipient"`
	Type      string    `json:"type"`
	Reason    string    `json:"reason,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// BounceReport is a feedback report you post yourself. EmailID must
// name a message this project sent, and Recipient must be one that
// message went to - reports that cannot be attributed are refused.
type BounceReport struct {
	Recipient string `json:"recipient"`
	EmailID   string `json:"email_id,omitempty"`
	Type      string `json:"type,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// Webhook is one outgoing notification target. Secret is returned
// only when the webhook is created.
type Webhook struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	URL       string    `json:"url"`
	Events    []string  `json:"events"`
	Filters   []string  `json:"filters"`
	CreatedAt time.Time `json:"created_at"`
}

// WebhookRequest creates a webhook.
type WebhookRequest struct {
	URL     string   `json:"url"`
	Events  []string `json:"events"`
	Filters []string `json:"filters,omitempty"`
}

// WebhookDelivery is one attempt to notify a webhook.
type WebhookDelivery struct {
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

// Contact is an address the project has delivered to, with tallies.
// Written by the delivery worker - the API is read-only by design.
type Contact struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Email     string `json:"email"`
	Name      string `json:"name,omitempty"`
	SentCount int    `json:"sent_count"`
	FailCount int    `json:"fail_count"`

	// Suppressed is resolved from the suppression list at read time,
	// never stored, so it cannot drift from the list that governs
	// sending.
	Suppressed   bool       `json:"suppressed"`
	LastSentAt   *time.Time `json:"last_sent_at,omitempty"`
	LastFailedAt *time.Time `json:"last_failed_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// UnsubscribeList is a transactional opt-out scope.
type UnsubscribeList struct {
	ID              string     `json:"id"`
	ProjectID       string     `json:"project_id"`
	Name            string     `json:"name"`
	PublicName      string     `json:"public_name,omitempty"`
	Description     string     `json:"description,omitempty"`
	Active          bool       `json:"active"`
	SuppressedCount int        `json:"suppressed_count"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       *time.Time `json:"updated_at,omitempty"`
}

// Subscriber is one address on a subscriber list.
type Subscriber struct {
	ID             string         `json:"id"`
	ProjectID      string         `json:"project_id"`
	Email          string         `json:"email"`
	Name           string         `json:"name,omitempty"`
	Status         string         `json:"status"`
	CustomFields   map[string]any `json:"custom_fields,omitempty"`
	Timezone       string         `json:"timezone,omitempty"`
	Language       string         `json:"language,omitempty"`
	SubscribedAt   *time.Time     `json:"subscribed_at,omitempty"`
	UnsubscribedAt *time.Time     `json:"unsubscribed_at,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      *time.Time     `json:"updated_at,omitempty"`
}

// SubscribeRequest adds an address to a static list, creating the
// subscriber when the address is new.
type SubscribeRequest struct {
	ListID       string         `json:"list_id"`
	Email        string         `json:"email"`
	Name         string         `json:"name,omitempty"`
	CustomFields map[string]any `json:"custom_fields,omitempty"`
	Timezone     string         `json:"timezone,omitempty"`
	Language     string         `json:"language,omitempty"`
}

// InboundEmail is a message received by the MX listener.
type InboundEmail struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	DomainID  string `json:"domain_id"`
	MessageID string `json:"message_id,omitempty"`

	Sender      string            `json:"sender"`
	Recipients  []string          `json:"recipients"`
	Subject     string            `json:"subject,omitempty"`
	TextBody    string            `json:"text_body,omitempty"`
	HTMLBody    string            `json:"html_body,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Attachments []Attachment      `json:"attachments,omitempty"`

	// Auth is the SPF, DKIM and DMARC verdict stamped at ingest.
	Auth   *InboundAuth `json:"auth,omitempty"`
	HasRaw bool         `json:"has_raw,omitzero"`
	Size   int64        `json:"size"`

	Status       string    `json:"status"`
	ErrorMessage string    `json:"error_message,omitempty"`
	ReceivedAt   time.Time `json:"received_at"`
	CreatedAt    time.Time `json:"created_at"`
}

// InboundAuth is the sender-authentication verdict.
//
// Aligned is the only field worth acting on: a valid signature from
// some other domain is not authentication.
type InboundAuth struct {
	SPF         string `json:"spf"`
	DKIM        string `json:"dkim"`
	DMARC       string `json:"dmarc"`
	DMARCPolicy string `json:"dmarc_policy,omitempty"`
	Aligned     bool   `json:"aligned"`
	ClientIP    string `json:"client_ip,omitempty"`
}

// AnalyticsSummary is the dashboard aggregate.
type AnalyticsSummary struct {
	Emails      map[string]int `json:"emails"`
	TotalEmails int            `json:"total_emails"`

	// FailureRate is failed over finalized (sent + failed), as a
	// percentage. Queued and scheduled mail is excluded rather than
	// counted as either.
	FailureRate float64        `json:"failure_rate"`
	Inbound     map[string]int `json:"inbound"`
	Resources   map[string]int `json:"resources"`
}

// DayCount is one day of the delivery trend. Days with no traffic are
// present with a zero count, so a chart cannot rescale its axis.
type DayCount struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

// Analytics is the delivery trend over a window.
type Analytics struct {
	DailyCounts     []DayCount     `json:"daily_counts"`
	StatusBreakdown map[string]int `json:"status_breakdown"`
	From            string         `json:"from"`
	To              string         `json:"to"`
}

// EmailFilter narrows a list of emails.
type EmailFilter struct {
	Status string

	// Before returns only rows created before this instant.
	Before *time.Time
	Limit  int
}

// SuppressionFilter narrows a list of suppressions. Search is
// prefix-anchored so the index serves it.
type SuppressionFilter struct {
	Kind   string
	Search string
	Limit  int
	Cursor string
}

// BounceFilter narrows a list of bounces.
type BounceFilter struct {
	Type   string
	Search string
	Limit  int
	Cursor string
}
