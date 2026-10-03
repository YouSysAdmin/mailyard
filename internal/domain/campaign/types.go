// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package campaign

import (
	"github.com/yousysadmin/mailyard/internal/core/render"
	cmodel "github.com/yousysadmin/mailyard/internal/models/campaign"
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
	Name         string         `json:"name"               validate:"required,min=1,max=200" normalize:"trim"`
	Subject      string         `json:"subject"            validate:"omitempty,max=1000"     normalize:"trim"`
	FromEmail    string         `json:"from_email"         validate:"required,email,max=254" normalize:"normalize"`
	FromName     string         `json:"from_name"          validate:"omitempty,max=200"      normalize:"trim"`
	ReplyTo      string         `json:"reply_to"           validate:"omitempty,email,max=254" normalize:"normalize"`
	TemplateID   string         `json:"template_id"        validate:"required,uuid"`
	Language     string         `json:"language"           validate:"omitempty,min=2,max=10" normalize:"normalize"`
	TemplateData map[string]any `json:"template_data"      validate:"omitempty,max=100"`
	ListID       string         `json:"list_id"            validate:"required,uuid"`

	// Headers go on every message of the campaign, over the project's
	// defaults. Same rules as a message's own headers.
	Headers map[string]string `json:"headers" validate:"omitempty,max=20"`

	// SMTPGroup names the server pool the campaign sends through, by
	// slug. Empty uses the project's default group. Bulk on its own
	// pool is the usual arrangement, so a campaign cannot take
	// transactional mail down with it.
	SMTPGroup       string           `json:"smtp_group"         validate:"omitempty,max=100" normalize:"normalize"`
	SendRate        int              `json:"send_rate"          validate:"omitempty,min=0,max=100000"`
	SendAtLocalTime bool             `json:"send_at_local_time"`
	ABTestEnabled   bool             `json:"ab_test_enabled"`
	ABVariants      []cmodel.Variant `json:"ab_variants"        validate:"omitempty,max=5,dive"`

	// UnsubscribeDisabled sends the campaign with no List-Unsubscribe
	// headers and no unsubscribe link. Against the bulk-sender rules
	// of Gmail and Yahoo, and offered anyway as a deliberate choice.
	UnsubscribeDisabled bool `json:"unsubscribe_disabled"`

	// DisableSigning sends the campaign without the sender address's
	// signature, when the address carries a key.
	DisableSigning bool `json:"disable_signing"`

	// smtpGroupID is the resolved form of SMTPGroup, filled by
	// validateCampaignRefs. Unexported so it cannot arrive from the
	// request body - a caller naming a group id directly would skip
	// the project scoping the slug lookup performs.
	smtpGroupID string
}

type sendInput struct {
	ScheduledAt string `json:"scheduled_at" validate:"omitempty"`
}

// previewInput picks whose message to render. With no subscriber the
// campaign's own template_data renders alone, exactly as a send
// would treat a subscriber carrying no custom fields. variant names an
// A/B variant and defaults to the first one.
type previewInput struct {
	SubscriberID string `json:"subscriber_id" validate:"omitempty,uuid"`
	Variant      string `json:"variant"       validate:"omitempty,max=50" normalize:"trim"`
}

// ----------------------------------------------------------------------------
// Responses
// ----------------------------------------------------------------------------

// ListResponse is the project's campaigns.
type ListResponse struct {
	Campaigns []*cmodel.Campaign `json:"campaigns"`

	// Total counts what the filters match, whether or not a page was
	// asked for.
	Total int `json:"total"`
}

// CampaignResponse is one campaign.
type CampaignResponse struct {
	Campaign *cmodel.Campaign `json:"campaign"`
}

// CampaignDetailResponse is a campaign with its headline numbers.
//
// These counters are aggregated as the send runs and survive the
// tracking-event retention sweep, unlike the series on Analytics.
type CampaignDetailResponse struct {
	Campaign       *cmodel.Campaign `json:"campaign"`
	Stats          any              `json:"stats"`
	StatsByVariant any              `json:"stats_by_variant"`
	Engagement     Engagement       `json:"engagement"`
}

// Engagement is the unique-recipient view: how many opened and how
// many clicked, not how many times.
type Engagement struct {
	Opened  int `json:"opened"`
	Clicked int `json:"clicked"`

	// Unsubscribed counts recipients who unsubscribed through this
	// campaign's link, whichever scope they chose on the page. Read off
	// campaign_messages.unsubscribed_at, so it survives the event sweep.
	Unsubscribed int `json:"unsubscribed"`

	// Sent is the denominator: recipients the campaign actually
	// delivered to. Not the audience size - a message that failed or
	// was skipped for a suppression was never in a position to be
	// opened, and counting it would report a delivery problem as an
	// engagement problem.
	//
	// No tracked/untracked distinction here, unlike the dashboard:
	// campaigns track unconditionally, because bulk mail needs
	// List-Unsubscribe and the link tallies are the point.
	Sent int `json:"sent"`

	// Rates as percentages, computed here rather than in the console.
	// The dashboard reports the same two numbers for the project, and
	// two places doing the division is two places to round it
	// differently and to disagree about the denominator.
	OpenRate        float64 `json:"open_rate"`
	ClickRate       float64 `json:"click_rate"`
	UnsubscribeRate float64 `json:"unsubscribe_rate"`
}

// engagementOf assembles the counts with their rates.
func engagementOf(sent, opened, clicked, unsubscribed int) Engagement {
	e := Engagement{Opened: opened, Clicked: clicked, Unsubscribed: unsubscribed, Sent: sent}
	if sent > 0 {
		e.OpenRate = float64(opened) / float64(sent) * 100
		e.ClickRate = float64(clicked) / float64(sent) * 100
		e.UnsubscribeRate = float64(unsubscribed) / float64(sent) * 100
	}

	return e
}

// AnalyticsResponse is the deep-dive readout: per-link tallies and the
// daily series a chart needs.
//
// The series reach back only as far as tracking-event retention, which
// is why the headline counters live on the campaign itself.
type AnalyticsResponse struct {
	Links       any `json:"links"`
	OpenSeries  any `json:"open_series"`
	ClickSeries any `json:"click_series"`
}

// PreviewResponse is the campaign rendered for one subscriber, exactly
// as the runner would render it, with the system links stripped since
// no message exists for them to point at.
type PreviewResponse struct {
	Preview *render.Output `json:"preview"`
}

// MessageListResponse is the per-recipient rows of one campaign.
type MessageListResponse struct {
	Messages []*cmodel.Message `json:"messages"`

	// NextCursor resumes after the last row, empty on the last page.
	NextCursor string `json:"next_cursor,omitempty"`
}
