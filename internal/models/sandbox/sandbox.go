// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package sandbox models mail a developer submitted that Mailyard
// captured instead of delivering.
//
// Nothing here ever reaches a recipient. That is not a failure mode
// to be reported, it is the request being honored: the credential the
// message arrived with says capture, so capture is the successful
// outcome and the client is told 250 / 201 exactly as if it had been
// sent.
package sandbox

import "time"

// Sources a message can arrive by.
const (
	// SourceSubmission is the SMTP submission listener.
	SourceSubmission = "submission"

	// SourceAPI is POST /api/v1/emails/send.
	SourceAPI = "api"
)

// Attachment is one attachment as it appears in a listing: name, type
// and size, never the bytes.
//
// The bytes live once, inside Raw. A download re-parses the message
// rather than reading a second copy, which is what keeps a sandbox
// delete to a single statement with no blob store to leave orphans in.
type Attachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type,omitempty"`
	Size        int64  `json:"size"`
}

// Email is one captured message.
type Email struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`

	// Source is which surface accepted it, and CredentialID / APIKeyID
	// name the credential that did. A project usually has more than one
	// integration pointed at the sandbox, and "which of them sent this"
	// is a question the raw message cannot answer.
	Source       string `json:"source"`
	CredentialID string `json:"credential_id,omitempty"`
	APIKeyID     string `json:"api_key_id,omitempty"`

	// Sender and Recipients are the SMTP envelope, which is what a
	// receiver would actually have routed on. They are kept separate
	// from the From and To headers, and a developer chasing a Bcc or a
	// return path is asking precisely about the difference.
	Sender     string   `json:"sender"`
	Recipients []string `json:"recipients"`

	Subject     string            `json:"subject,omitempty"`
	TextBody    string            `json:"text_body,omitempty"`
	HTMLBody    string            `json:"html_body,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Attachments []Attachment      `json:"attachments,omitempty"`

	// Raw is the whole message as bytes. Never serialized into a list
	// response - it is fetched on its own endpoint, because a page of
	// fifty messages would otherwise carry fifty full MIME trees.
	Raw  []byte `json:"-"`
	Size int64  `json:"size"`

	ClientIP string `json:"client_ip,omitempty"`

	// ExpiresAt is when retention may remove this message. Nil means
	// kept until something else removes it - the per-project cap, or a
	// hand delete.
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	ReceivedAt time.Time  `json:"received_at"`
	CreatedAt  time.Time  `json:"created_at"`
}
