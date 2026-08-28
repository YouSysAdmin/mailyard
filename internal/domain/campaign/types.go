// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package campaign

import (
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
	FromEmail    string         `json:"from_email"         validate:"required,email,max=320" normalize:"normalize"`
	FromName     string         `json:"from_name"          validate:"omitempty,max=200"      normalize:"trim"`
	ReplyTo      string         `json:"reply_to"           validate:"omitempty,email,max=320" normalize:"normalize"`
	TemplateID   string         `json:"template_id"        validate:"required,uuid"`
	Language     string         `json:"language"           validate:"omitempty,min=2,max=10" normalize:"normalize"`
	TemplateData map[string]any `json:"template_data"      validate:"omitempty,max=100"`
	ListID       string         `json:"list_id"            validate:"required,uuid"`

	// SMTPGroup names the server pool the campaign sends through, by
	// slug. Empty uses the project's default group. Bulk on its own
	// pool is the usual arrangement, so a campaign cannot take
	// transactional mail down with it.
	SMTPGroup       string           `json:"smtp_group"         validate:"omitempty,max=100" normalize:"normalize"`
	SendRate        int              `json:"send_rate"          validate:"omitempty,min=0,max=100000"`
	SendAtLocalTime bool             `json:"send_at_local_time"`
	ABTestEnabled   bool             `json:"ab_test_enabled"`
	ABVariants      []cmodel.Variant `json:"ab_variants"        validate:"omitempty,max=5,dive"`

	// smtpGroupID is the resolved form of SMTPGroup, filled by
	// validateCampaignRefs. Unexported so it cannot arrive from the
	// request body - a caller naming a group id directly would skip
	// the project scoping the slug lookup performs.
	smtpGroupID string
}

type sendInput struct {
	ScheduledAt string `json:"scheduled_at" validate:"omitempty"`
}

// ----------------------------------------------------------------------------
// Responses
// ----------------------------------------------------------------------------

// ListResponse is the project's campaigns.
type ListResponse struct {
	Campaigns []*cmodel.Campaign `json:"campaigns"`
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
	OpenRate  float64 `json:"open_rate"`
	ClickRate float64 `json:"click_rate"`
}

// engagementOf assembles the counts with their rates.
func engagementOf(sent, opened, clicked int) Engagement {
	e := Engagement{Opened: opened, Clicked: clicked, Sent: sent}
	if sent > 0 {
		e.OpenRate = float64(opened) / float64(sent) * 100
		e.ClickRate = float64(clicked) / float64(sent) * 100
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

// MessageListResponse is the per-recipient rows of one campaign.
type MessageListResponse struct {
	Messages []*cmodel.Message `json:"messages"`
}
