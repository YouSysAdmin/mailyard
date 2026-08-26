// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package inbound models mail received by the MX listener for a
// verified domain.
package inbound

import "time"

const (
	// StatusReceived is a stored, parsed message.
	StatusReceived = "received"

	// StatusRejected is a message refused at ingest time (suppressed
	// sender, duplicate) kept for the audit trail.
	StatusRejected = "rejected"

	// StatusFailed is a message whose MIME tree could not be parsed -
	// the raw bytes are retained for debugging.
	StatusFailed = "failed"
)

// Auth is what SPF, DKIM and DMARC said about a message at ingest.
// Recorded rather than acted on - alignment is the only field worth a
// decision, and refusal needs the operator to have asked for it.
type Auth struct {
	SPF   string `json:"spf"`
	DKIM  string `json:"dkim"`
	DMARC string `json:"dmarc"`

	// DMARCPolicy is what the From domain published (none, quarantine
	// or reject). Empty when there is no record.
	DMARCPolicy string `json:"dmarc_policy,omitempty"`

	// Aligned is the verdict that matters: something the From domain
	// vouches for actually passed.
	Aligned bool `json:"aligned"`

	// ClientIP is the peer the message arrived from, kept because a
	// verdict is meaningless without knowing who was being judged.
	ClientIP string `json:"client_ip,omitempty"`
}

// Trusted reports whether the From domain vouched for this message.
// The console uses it to decide whether to show a warning badge.
func (a *Auth) Trusted() bool { return a != nil && a.Aligned }

// Attachment is one decoded inbound attachment. Content is inline
// base64 unless a blob store is configured, then StorageKey points at
// the offloaded bytes instead.
// Auth is the sender-authentication verdict recorded at receipt.
type Attachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type,omitempty"`
	Size        int64  `json:"size"`
	Content     string `json:"content,omitempty"`
	StorageKey  string `json:"storage_key,omitempty"`
}

// Email is one received message.
type Email struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	DomainID  string `json:"domain_id"`

	// MessageID is the parsed Message-ID header without brackets.
	MessageID string `json:"message_id,omitempty"`

	// DedupHash fingerprints the message: its Message-ID together with
	// sender, recipients, subject and size, so a reused id over other
	// content is not a duplicate. Empty on a row stored before parsing.
	DedupHash string `json:"-"`

	// Sender and Recipients are the SMTP envelope, the source of
	// truth for routing (headers are informational).
	Sender      string            `json:"sender"`
	Recipients  []string          `json:"recipients"`
	Subject     string            `json:"subject,omitempty"`
	TextBody    string            `json:"text_body,omitempty"`
	HTMLBody    string            `json:"html_body,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Attachments []Attachment      `json:"attachments,omitempty"`

	// Raw keeps the wire bytes only when parsing failed.
	Raw []byte `json:"-"`

	// HasRaw tells the console whether a raw download exists. Computed
	// from Raw at read time, never stored.
	HasRaw bool  `json:"has_raw,omitzero"`
	Size   int64 `json:"size"`

	// Auth records what SPF, DKIM and DMARC said about this message.
	// Stored as JSON so the shape can grow without a migration, and
	// kept per message rather than derived later because the checks
	// depend on the connecting IP, which only exists at receipt.
	Auth         *Auth     `json:"auth,omitempty"`
	Status       string    `json:"status"`
	ErrorMessage string    `json:"error_message,omitempty"`
	ReceivedAt   time.Time `json:"received_at"`
	CreatedAt    time.Time `json:"created_at"`
}
