// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package campaign is the bulk send: a template rendered per
// subscriber of a list, fanned out into per-recipient Message rows
// that the campaign runner drains into the email queue.
package campaign

import "time"

// Campaign statuses. draft -> (scheduled ->) sending -> sent, with
// paused and cancelled as operator exits. The runner only touches
// campaigns in sending.
const (
	StatusDraft     = "draft"
	StatusScheduled = "scheduled"
	StatusSending   = "sending"
	StatusPaused    = "paused"
	StatusSent      = "sent"
	StatusCancelled = "cancelled"
)

// Variant is one A/B test arm: an alternative subject and optionally
// a different template, delivered to SplitPercentage of the audience.
type Variant struct {
	Name            string `json:"name"             validate:"required,min=1,max=50"`
	Subject         string `json:"subject"          validate:"omitempty,max=1000"`
	TemplateID      string `json:"template_id"      validate:"omitempty,uuid"`
	SplitPercentage int    `json:"split_percentage" validate:"required,min=1,max=100"`
}

// Campaign is one bulk send definition.
type Campaign struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	CreatedBy string `json:"created_by,omitempty"`
	Name      string `json:"name"`

	// Subject is the fallback when the template localization renders
	// an empty subject.
	Subject   string `json:"subject,omitempty"`
	FromEmail string `json:"from_email"`
	FromName  string `json:"from_name,omitempty"`

	// ReplyTo is the Reply-To address, empty for none. A newsletter
	// sent from a no-reply mailbox names where a reader's answer goes.
	ReplyTo string `json:"reply_to,omitempty"`

	TemplateID string `json:"template_id"`
	Language   string `json:"language,omitempty"`

	// TemplateData is the campaign-level render data. Per subscriber
	// it is merged with the subscriber's custom fields plus the
	// reserved keys email and name.
	TemplateData map[string]any `json:"template_data,omitempty"`
	Status       string         `json:"status"`
	ListID       string         `json:"list_id"`

	// SMTPGroupID routes the whole campaign to a named server pool.
	// Empty uses the project's default group. Separating pools is the
	// usual reason to have them: a campaign burning an IP should not
	// take transactional mail down with it.
	SMTPGroupID string `json:"smtp_group_id,omitempty"`

	// SendRate caps throughput in emails per minute. 0 = unthrottled.
	SendRate int `json:"send_rate"`

	// SendAtLocalTime delivers at the scheduled wall-clock time in
	// each subscriber's timezone (subscribers without a timezone get
	// the scheduled instant as-is).
	SendAtLocalTime bool      `json:"send_at_local_time"`
	ABTestEnabled   bool      `json:"ab_test_enabled"`
	ABVariants      []Variant `json:"ab_variants,omitempty"`

	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`

	// NextBatchAt is runner state: when the next batch is due
	// (throttling and the claim lease live here).
	NextBatchAt *time.Time `json:"-"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   *time.Time `json:"updated_at,omitempty"`
}

// Message statuses. pending rows await a batch, queued rows have an
// email in the delivery queue, the rest mirror the email's fate
// (skipped = recipient suppressed or campaign cancelled).
const (
	MsgPending = "pending"
	MsgQueued  = "queued"
	MsgSent    = "sent"
	MsgFailed  = "failed"
	MsgSkipped = "skipped"
)

// Message is one recipient of one campaign. OpenedAt and ClickedAt
// are first-event stamps written by the tracking endpoints.
type Message struct {
	ID           string `json:"id"`
	CampaignID   string `json:"campaign_id"`
	SubscriberID string `json:"subscriber_id"`

	// Email is the subscriber's address, joined in by the console list
	// query and never stored. An id tells the operator nothing about
	// who a message went to.
	Email        string     `json:"email,omitempty"`
	EmailID      string     `json:"email_id,omitempty"`
	Status       string     `json:"status"`
	ErrorMessage string     `json:"error_message,omitempty"`
	Variant      string     `json:"variant,omitempty"`
	DeliverAt    *time.Time `json:"deliver_at,omitempty"`
	SentAt       *time.Time `json:"sent_at,omitempty"`
	OpenedAt     *time.Time `json:"opened_at,omitempty"`
	ClickedAt    *time.Time `json:"clicked_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

// TrackedLink is one rewritten URL of a campaign: the /tracking/click/ redirect
// resolves hash back to OriginalURL and counts the click.
type TrackedLink struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`

	// CampaignID groups a campaign's links so click_count is the
	// campaign total. Empty for a link in a transactional send, which
	// groups per project instead.
	CampaignID  string    `json:"campaign_id,omitempty"`
	OriginalURL string    `json:"original_url"`
	Hash        string    `json:"hash"`
	ClickCount  int       `json:"click_count"`
	CreatedAt   time.Time `json:"created_at"`
}

// Tracking event types.
const (
	EventOpen        = "open"
	EventClick       = "click"
	EventUnsubscribe = "unsubscribe"
)

// DayCount is one day's tally in an analytics time series. Day is
// YYYY-MM-DD.
type DayCount struct {
	Day   string `json:"day"`
	Count int    `json:"count"`
}

// TrackingEvent is the raw per-hit log behind the first-event stamps
// on Message.
type TrackingEvent struct {
	ID string `json:"id"`

	// EmailID is the subject of the event and is always set - it is
	// the one identifier both campaign and transactional mail have.
	EmailID string `json:"email_id"`

	// CampaignMessageID is set only when the email belongs to a
	// campaign, so campaign reporting keeps working unchanged.
	CampaignMessageID string    `json:"campaign_message_id,omitempty"`
	EventType         string    `json:"event_type"`
	TrackedLinkID     string    `json:"tracked_link_id,omitempty"`
	IP                string    `json:"ip,omitempty"`
	UserAgent         string    `json:"user_agent,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}
