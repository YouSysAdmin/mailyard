// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"time"

	"github.com/yousysadmin/mailyard/internal/core/emailverify"
	"github.com/yousysadmin/mailyard/internal/core/render"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
	sandboxmodel "github.com/yousysadmin/mailyard/internal/models/sandbox"
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

// sendInput is the POST /api/emails/send body. SendAt is RFC 3339.
// DryRun runs every validation without persisting anything.
type sendInput struct {
	From        string                  `json:"from"        validate:"required,max=320"           normalize:"trim"`
	ReplyTo     string                  `json:"reply_to"    validate:"omitempty,max=320"          normalize:"trim"`
	To          []string                `json:"to"          validate:"required,min=1,dive,max=320"`
	Subject     string                  `json:"subject"     validate:"required,max=1000"`
	HTML        string                  `json:"html"        validate:"omitempty,max=1048576"`
	Text        string                  `json:"text"        validate:"omitempty,max=1048576"`
	Headers     map[string]string       `json:"headers"     validate:"omitempty,max=20"`
	Attachments []emailmodel.Attachment `json:"attachments" validate:"omitempty,max=10"`
	SendAt      string                  `json:"send_at"     validate:"omitempty"`
	DryRun      bool                    `json:"dry_run"`

	// UnsubscribeListID scopes to send to a transactional opt-out
	// list, so {{ mailyard_unsubscribe_url }} renders a one-click link
	// that blocks only that category of mail.
	UnsubscribeListID string `json:"unsubscribe_list_id" validate:"omitempty,max=64" normalize:"trim"`

	// The RFC 2369 / 8058 List-Unsubscribe targets, for an application
	// that runs its own opt-out. Mailyard carries the header and does
	// nothing else with it: what the recipient's click or mail lands on
	// is the caller's to handle. Mutually exclusive with
	// UnsubscribeListID, which is the opposite arrangement - Mailyard
	// mints the link and holds the list.
	//
	// A send of any size deserves one of the two. Bulk mail without
	// List-Unsubscribe is filtered by Gmail and Yahoo rather than
	// bounced, so the sender sees delivery and the recipient sees
	// nothing.
	ListUnsubscribeURL    string `json:"list_unsubscribe_url"    validate:"omitempty,max=1000" normalize:"trim"`
	ListUnsubscribeMailto string `json:"list_unsubscribe_mailto" validate:"omitempty,max=1000" normalize:"trim"`

	// ListUnsubscribePost adds List-Unsubscribe-Post, which tells the
	// mailbox provider it may unsubscribe the recipient with a single
	// POST to the URL above. Only set it if that endpoint accepts one.
	ListUnsubscribePost bool `json:"list_unsubscribe_post"`

	// SMTPGroup routes the send to a named pool by slug. Empty uses
	// the project's default group.
	SMTPGroup string `json:"smtp_group" validate:"omitempty,max=100" normalize:"normalize"`

	// DisableTracking suppresses the open pixel and click rewriting
	// for this message, whatever the project default is. There is no
	// "force on" counterpart: turning tracking ON is the project
	// owner's decision, not a caller's.
	DisableTracking bool `json:"disable_tracking"`

	// SMTPServerID pins one server exactly and overrides the group.
	// Mostly for testing a specific server end to end.
	SMTPServerID string `json:"smtp_server_id" validate:"omitempty,max=64" normalize:"trim"`

	// Sandbox captures this message instead of sending it.
	//
	// A POINTER, unlike every other flag here, because absent and
	// false have to be told apart. An API key marked sandbox captures
	// regardless of what the body says, and an explicit false from
	// such a key is refused rather than obeyed - which cannot be
	// expressed if a missing field arrives as false. See
	// sandboxIntent.
	Sandbox *bool `json:"sandbox"`

	// SandboxRetentionDays shortens how long the captured message is
	// kept. Longer than the platform window is clamped down to it.
	SandboxRetentionDays int `json:"sandbox_retention_days" validate:"omitempty,min=1,max=3650"`
}

// templateSendInput is the POST /api/emails/send-template body.
type templateSendInput struct {
	From            string                  `json:"from"          validate:"required,max=320" normalize:"trim"`
	ReplyTo         string                  `json:"reply_to"      validate:"omitempty,max=320" normalize:"trim"`
	To              []string                `json:"to"            validate:"required,min=1,dive,max=320"`
	TemplateID      string                  `json:"template_id"   validate:"omitempty,uuid"`
	TemplateName    string                  `json:"template_name" validate:"omitempty,max=100"`
	Language        string                  `json:"language"      validate:"omitempty,min=2,max=10" normalize:"normalize"`
	Data            map[string]any          `json:"data"`
	Headers         map[string]string       `json:"headers"       validate:"omitempty,max=20"`
	Attachments     []emailmodel.Attachment `json:"attachments"   validate:"omitempty,max=10"`
	SendAt          string                  `json:"send_at"       validate:"omitempty"`
	DryRun          bool                    `json:"dry_run"`
	DisableTracking bool                    `json:"disable_tracking"`

	// Same routing selectors as a plain send. See sendInput.
	SMTPGroup    string `json:"smtp_group"     validate:"omitempty,max=100" normalize:"normalize"`
	SMTPServerID string `json:"smtp_server_id" validate:"omitempty,max=64"  normalize:"trim"`

	// Same opt-out scope as a plain send, and the reason it is here:
	// a template may reference {{ mailyard_unsubscribe_url }}, and
	// without this there is no list to bind the link to.
	UnsubscribeListID string `json:"unsubscribe_list_id" validate:"omitempty,max=64" normalize:"trim"`

	// Same caller-supplied unsubscribe targets as a plain send. See
	// sendInput, which is also where these are carried from.
	ListUnsubscribeURL    string `json:"list_unsubscribe_url"    validate:"omitempty,max=1000" normalize:"trim"`
	ListUnsubscribeMailto string `json:"list_unsubscribe_mailto" validate:"omitempty,max=1000" normalize:"trim"`
	ListUnsubscribePost   bool   `json:"list_unsubscribe_post"`

	// Same sandbox controls as a plain send. See sendInput for why
	// Sandbox is a pointer.
	Sandbox              *bool `json:"sandbox"`
	SandboxRetentionDays int   `json:"sandbox_retention_days" validate:"omitempty,min=1,max=3650"`
}

// batchInput is the POST /api/emails/batch body. With a template ref
// every item renders it against its own data. Without one each item
// carries its own subject and body.
type batchInput struct {
	From         string           `json:"from"          validate:"required,max=320" normalize:"trim"`
	ReplyTo      string           `json:"reply_to"      validate:"omitempty,max=320" normalize:"trim"`
	TemplateID   string           `json:"template_id"   validate:"omitempty,uuid"`
	TemplateName string           `json:"template_name" validate:"omitempty,max=100"`
	Language     string           `json:"language"      validate:"omitempty,min=2,max=10" normalize:"normalize"`
	Items        []batchItemInput `json:"items"       validate:"required,min=1,max=100,dive"`
}

type batchItemInput struct {
	To       []string       `json:"to"       validate:"required,min=1,dive,max=320"`
	Language string         `json:"language" validate:"omitempty,min=2,max=10"`
	Data     map[string]any `json:"data"`
	Subject  string         `json:"subject"  validate:"omitempty,max=1000"`
	HTML     string         `json:"html"     validate:"omitempty,max=1048576"`
	Text     string         `json:"text"     validate:"omitempty,max=1048576"`

	// Per item, because an opt-out link identifies the recipient. A
	// batch is where an application sends its bulk mail, and one link
	// shared across a hundred items would unsubscribe whoever clicked
	// it from nothing in particular.
	ListUnsubscribeURL    string `json:"list_unsubscribe_url"    validate:"omitempty,max=1000"`
	ListUnsubscribeMailto string `json:"list_unsubscribe_mailto" validate:"omitempty,max=1000"`
	ListUnsubscribePost   bool   `json:"list_unsubscribe_post"`
}

// renderPreviewInput is the POST /api/emails/preview body: shows what
// a template send would produce, without sending.
type renderPreviewInput struct {
	TemplateID   string         `json:"template_id"   validate:"omitempty,uuid"`
	TemplateName string         `json:"template_name" validate:"omitempty,max=100"`
	Language     string         `json:"language"      validate:"omitempty,min=2,max=10" normalize:"normalize"`
	Data         map[string]any `json:"data"`
}

// testSendInput is the POST /api/templates/:id/send-test body.
type testSendInput struct {
	From     string         `json:"from"     validate:"required,max=320" normalize:"trim"`
	To       []string       `json:"to"       validate:"required,min=1,max=5,dive,max=320"`
	Language string         `json:"language" validate:"omitempty,min=2,max=10" normalize:"normalize"`
	Data     map[string]any `json:"data"`
}

// verifyInput names the address to check.
type verifyInput struct {
	Email string `json:"email" validate:"required,max=320" normalize:"normalize"`
}

// ----------------------------------------------------------------------------
// Responses
// ----------------------------------------------------------------------------

// The response types of this domain.
//
// They exist so the OpenAPI document can be REFLECTED rather than
// transcribed. While these were fiber.Map literals there was no type
// to read, the description of eight bodies was written by hand, and
// eight of them were wrong - with a test that compared paths and
// noticed nothing.
//
// The json tags must keep producing exactly what the maps produced.
// Note `suppressed_recipients` carries no omitempty on purpose: it is
// built with emptyIfNil so it marshals as [] rather than null, and a
// client should not have to tell those apart.

// SendResponse is a queued message. Suppressed lists recipients
// dropped because they are blocked - to send still succeeded for the
// rest, so an empty array is the ordinary case.
type SendResponse struct {
	Email      *emailmodel.Email `json:"email"`
	Suppressed []string          `json:"suppressed_recipients"`
}

// DryRunResponse is what a send with dry_run set returns: the message
// was validated and rendered, nothing was stored and nothing sent.
type DryRunResponse struct {
	DryRun     bool     `json:"dry_run"`
	Valid      bool     `json:"valid"`
	Recipients int      `json:"recipients"`
	Suppressed []string `json:"suppressed_recipients"`
}

// TemplateDryRunResponse is the template equivalent, which also
// carries what the template rendered to.
type TemplateDryRunResponse struct {
	DryRun   bool           `json:"dry_run"`
	Valid    bool           `json:"valid"`
	Template string         `json:"template"`
	Preview  *render.Output `json:"preview"`
}

// BatchResponse reports what happened to each item. One bad item does
// not sink the rest, which is why this is a 200 whose body carries
// per-item outcomes.
type BatchResponse struct {
	Total    int           `json:"total"`
	Accepted int           `json:"accepted"`
	Results  []BatchResult `json:"results"`
}

// PreviewResponse is a template rendered without being sent.
type PreviewResponse struct {
	Template string         `json:"template"`
	Preview  *render.Output `json:"preview"`
}

// ListResponse is a page of the email log.
type ListResponse struct {
	Emails []*emailmodel.Email `json:"emails"`
}

// StatsResponse counts emails by delivery status.
type StatsResponse struct {
	Counts map[string]int `json:"counts"`
}

// EmailResponse is one message with its full delivery record.
type EmailResponse struct {
	Email *emailmodel.Email `json:"email"`
}

// TrackedLinksResponse maps a message's link hashes to the original
// destinations behind its click redirects. A hash the project never
// registered is absent rather than empty.
type TrackedLinksResponse struct {
	Links map[string]string `json:"links"`
}

// StatusResponse is the cheap poll: delivery state without the
// bodies, headers or attachments.
type StatusResponse struct {
	ID           string     `json:"id"`
	Status       string     `json:"status"`
	Attempts     int        `json:"attempts"`
	ErrorMessage string     `json:"error_message"`
	SentAt       *time.Time `json:"sent_at"`
}

// LimitsResponse reports what a send may carry on this installation,
// so a client can validate before paying for a round trip.
type LimitsResponse struct {
	Limits SendingLimits `json:"limits"`
}

// SendingLimits are the per-message ceilings. Sizes are bytes.
type SendingLimits struct {
	MaxRecipients          int   `json:"max_recipients"`
	MaxAttachments         int   `json:"max_attachments"`
	MaxAttachmentSize      int64 `json:"max_attachment_size"`
	MaxTotalAttachmentSize int64 `json:"max_total_attachment_size"`
}

// VerifyResponse is the verdict on one address.
type VerifyResponse struct {
	Verification emailverify.Result `json:"verification"`
}

// SandboxCaptureResponse is returned when the credential captures
// instead of delivering. Sandboxed is always true and exists so a
// caller can branch without inspecting the rest.
type SandboxCaptureResponse struct {
	SandboxEmail *sandboxmodel.Email `json:"sandbox_email"`
	Sandboxed    bool                `json:"sandboxed"`
}
