// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package email is the outbound email record. The emails table
// doubles as the delivery queue: status plus next_attempt_at drive
// the worker's claim query, and attempts / claimed_at carry the
// retry and crash-recovery state.
package email

import "time"

// Statuses, the delivery lifecycle. queued and scheduled rows are
// claimable by the worker (scheduled simply has a future
// next_attempt_at). processing is a claimed row in flight. sent,
// failed, and suppressed are terminal (failed can be re-queued by the
// retry endpoint).
const (
	StatusPending    = "pending"
	StatusQueued     = "queued"
	StatusProcessing = "processing"
	StatusSent       = "sent"
	StatusFailed     = "failed"
	StatusSuppressed = "suppressed"
	StatusScheduled  = "scheduled"
	StatusCancelled  = "cancelled"
)

// validStatuses is the set the API will accept as a filter.
var validStatuses = map[string]struct{}{
	StatusPending: {}, StatusQueued: {}, StatusProcessing: {},
	StatusSent: {}, StatusFailed: {}, StatusSuppressed: {}, StatusScheduled: {},
	StatusCancelled: {},
}

// ValidStatus reports whether s names a real delivery state.
func ValidStatus(s string) bool {
	_, ok := validStatuses[s]

	return ok
}

// Attachment is a file carried inline (base64 content) in the stored
// email. Blob storage is a roadmap item.
type Attachment struct {
	Filename    string `json:"filename"`
	Content     string `json:"content,omitempty"`
	ContentType string `json:"content_type,omitempty"`

	// StorageKey points into the blob store when the content was
	// offloaded - Content is empty then and rehydrated at send time.
	StorageKey string `json:"storage_key,omitempty"`

	// Size in bytes of the decoded content, recorded on offload so
	// listings stay meaningful without the bytes.
	Size int64 `json:"size,omitzero"`
}

// Email is one outbound message and its delivery state.
type Email struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	CreatedBy string `json:"created_by,omitempty"`
	APIKeyID  string `json:"api_key_id,omitempty"`

	// CredentialID names the SMTP submission credential a message
	// arrived with, empty on every other path. CreatedBy is whoever
	// MINTED that credential rather than whoever sent this, so the
	// two answer different questions and a screen wanting the second
	// has to read this one.
	CredentialID string `json:"credential_id,omitempty"`

	SMTPServerID string `json:"smtp_server_id,omitempty"`

	// DeliveredVia is the server that actually CARRIED the message,
	// which after a failover walk is not necessarily SMTPServerID -
	// that one is what the sender asked for and keeps meaning that
	// across retries. Empty until a delivery succeeds.
	DeliveredVia string `json:"delivered_via,omitempty"`

	// SMTPGroupID routes the message to a named pool. Empty means the
	// project's default group. Resolved from the caller's slug at
	// accept time, so a queued row never holds a name that can be
	// renamed out from under it.
	SMTPGroupID  string            `json:"smtp_group_id,omitempty"`
	Sender       string            `json:"sender"`
	Recipients   []string          `json:"recipients"`
	Subject      string            `json:"subject"`
	TemplateName string            `json:"template_name,omitempty"`
	HTMLBody     string            `json:"html_body,omitempty"`
	TextBody     string            `json:"text_body,omitempty"`
	Attachments  []Attachment      `json:"attachments,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`

	// Tags are the caller's labels, what the log is filtered by.
	// Metadata is a flat string map the caller attaches and reads back,
	// an order id or a tenant of theirs, carried and never interpreted.
	Tags     []string          `json:"tags,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`

	// RFC 2369 / 8058 List-Unsubscribe headers: stamped on campaign
	// emails by the runner, minted for a send scoped to an opt-out
	// list, or supplied by the caller for an application that runs its
	// own unsubscribe.
	ListUnsubscribeURL string `json:"list_unsubscribe_url,omitempty"`

	// ListUnsubscribeMailto is the mailto: half of the same header.
	// Both may be set: RFC 2369 allows several URIs and the mailto is
	// what a client too old for one-click falls back to.
	ListUnsubscribeMailto string `json:"list_unsubscribe_mailto,omitempty"`

	// UnsubscribeListID names the transactional opt-out scope this
	// send belongs to, empty for an unscoped send.
	UnsubscribeListID string `json:"unsubscribe_list_id,omitempty"`

	// Tracked records that this message went out with tracking
	// applied. Without it a zero OpenCount cannot be told apart from
	// tracking never having been asked for.
	Tracked    bool       `json:"tracked"`
	OpenedAt   *time.Time `json:"opened_at,omitempty"`
	ClickedAt  *time.Time `json:"clicked_at,omitempty"`
	OpenCount  int        `json:"open_count"`
	ClickCount int        `json:"click_count"`

	ListUnsubscribePost bool `json:"list_unsubscribe_post,omitzero"`

	// Signing is the kind of signature this message was accepted to
	// carry - pgp or smime - or empty for none. Decided at accept
	// time from the sender's key, so the log can say what went out.
	// The key itself is read at delivery, which is why a replaced key
	// applies to what is still queued.
	Signing string `json:"signing,omitempty"`

	// System marks platform mail sent through this project's pipeline.
	// The row belongs to the platform, not the project: no quota, no
	// suppressions, no tracking, no webhooks, no contacts, and no
	// project-scoped reader answers it, so it is never on the wire.
	System bool `json:"-"`

	Status        string     `json:"status"`
	ErrorMessage  string     `json:"error_message,omitempty"`
	Attempts      int        `json:"attempts"`
	MaxAttempts   int        `json:"max_attempts"`
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty"`
	ClaimedAt     *time.Time `json:"-"`
	CreatedAt     time.Time  `json:"created_at"`
	ScheduledAt   *time.Time `json:"scheduled_at,omitempty"`
	SentAt        *time.Time `json:"sent_at,omitempty"`
}
