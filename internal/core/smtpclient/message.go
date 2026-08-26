// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpclient

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"strings"
	"time"
)

// Attachment is a file carried inline in the message. Content is
// base64 as received from the API and stays base64 all the way into
// the MIME part.
type Attachment struct {
	Filename    string `json:"filename"`
	Content     string `json:"content,omitempty"`
	ContentType string `json:"content_type"`
}

// Message is one outbound email, fully rendered. Headers are custom
// headers already filtered for safety by the caller. The
// ListUnsubscribe fields emit RFC 2369 / 8058 headers.
type Message struct {
	From string

	// EnvelopeFrom overrides the SMTP MAIL FROM when non-empty. The
	// From header is untouched - this is the bounce path (what
	// receivers record as Return-Path), not the visible sender.
	EnvelopeFrom          string
	To                    []string
	Subject               string
	HTML                  string
	Text                  string
	Attachments           []Attachment
	Headers               map[string]string
	ListUnsubscribeURL    string
	ListUnsubscribeMailto string
	ListUnsubscribePost   bool

	// Date stamps the message. Zero means now, which is what every
	// caller wants - it exists so a test can pin it.
	Date time.Time

	// MessageID overrides the generated id, without angle brackets.
	// Zero means one is derived from the sender's domain.
	MessageID string

	// EmailID stamps HeaderEmailID on the message.
	//
	// This is how a bounce finds its way home. A delivery status
	// notification returns the original headers, so the id comes back
	// with it - and unlike the envelope or the Message-ID, a custom
	// header survives a provider that owns the return path and rewrites
	// what it likes. See HeaderEmailID.
	EmailID string

	// Sign, when set, is applied to the rendered bytes immediately
	// before transmission. DKIM signing hangs off this rather than
	// living inside Build so that Build stays a pure renderer and this
	// package keeps no opinion about signing - the caller decides
	// whether a message gets signed and with whose key.
	Sign func([]byte) ([]byte, error)
}

func (m *Message) date() time.Time {
	if m.Date.IsZero() {
		return time.Now()
	}

	return m.Date
}

// messageID returns the id without angle brackets, generated from the
// sender's domain when the caller did not supply one. The right-hand
// side must be a domain we actually send as, because receivers treat a
// mismatched one as a forgery signal.
func (m *Message) messageID() string {
	if m.MessageID != "" {
		return m.MessageID
	}

	host := "localhost"
	if addr := EnvelopeAddress(m.From); addr != "" {
		if _, h, ok := strings.CutLast(addr, "@"); ok && h != "" {
			host = h
		}
	}

	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("%d@%s", time.Now().UnixNano(), host)
	}

	return base64.RawURLEncoding.EncodeToString(raw[:]) + "@" + host
}

const (
	contentTypeTextPlain = "text/plain"
	contentTypeTextHTML  = "text/html"
)

// HeaderEmailID carries the sending id so a bounce can be attributed
// back to the exact message.
//
// A custom header rather than the two obvious alternatives, and for
// the same reason in both cases - neither survives a provider that
// takes the message over:
//
//   - The ENVELOPE. Encoding the id in the return path (VERP) only
//     works while MAIL FROM is ours. Amazon SES and every comparable
//     provider replace it so bounces come back to them, at which point
//     whatever we wrote is gone.
//   - The message-ID. SES rewrites that too, which is already why
//     smtp_servers carries skip_dkim.
//
// An unknown X- header is the one thing they leave alone, and a DSN
// returns it with the rest of the original headers.
const HeaderEmailID = "X-Mailyard-Email-Id"

// headerSafe strips CR and LF from a header value so it occupies one
// header line. The caller has already refused such input where a
// person could see the refusal, so this only ever changes a value that
// reached the builder some other way.
func headerSafe(v string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(v)
}

// Build renders the RFC 5322 message bytes: headers, then
// multipart/mixed when attachments are present, with a
// multipart/alternative body when both HTML and text exist.
func (m *Message) Build() []byte {
	var b strings.Builder

	// Validation refuses a line break in any of these upstream. This
	// is the last writer, so it also refuses to emit one: a value that
	// somehow arrives with CR or LF loses them rather than becoming a
	// second header in a message the platform then DKIM-signs.
	fmt.Fprintf(&b, "From: %s\r\n", headerSafe(m.From))
	to := make([]string, len(m.To))
	for i, addr := range m.To {
		to[i] = headerSafe(addr)
	}

	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(to, ", "))
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("UTF-8", m.Subject))
	// Date and Message-ID are mandatory originator fields (RFC 5322
	// section 3.6). We emitted neither, and relied on whatever the
	// relay chose to add. That was already a spam signal on its own,
	// and it breaks DKIM in a specific way: both are in the signed
	// header set, so a header added after signing is unsigned, and a
	// receiver that sees an unsigned Date on a signed message has no
	// way to detect a replay.
	fmt.Fprintf(&b, "Date: %s\r\n", m.date().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-ID: <%s>\r\n", m.messageID())
	if m.EmailID != "" {
		fmt.Fprintf(&b, "%s: %s\r\n", HeaderEmailID, m.EmailID)
	}

	b.WriteString("MIME-Version: 1.0\r\n")

	// RFC 2369 / 8058 List-Unsubscribe. Mailto first, then the https
	// URL. The one-click POST header applies to the https target only.
	var luParts []string
	if m.ListUnsubscribeMailto != "" {
		luParts = append(luParts, "<"+m.ListUnsubscribeMailto+">")
	}

	if m.ListUnsubscribeURL != "" {
		luParts = append(luParts, "<"+m.ListUnsubscribeURL+">")
	}

	if len(luParts) > 0 {
		fmt.Fprintf(&b, "List-Unsubscribe: %s\r\n", strings.Join(luParts, ", "))
		if m.ListUnsubscribePost && m.ListUnsubscribeURL != "" {
			b.WriteString("List-Unsubscribe-Post: List-Unsubscribe=One-Click\r\n")
		}
	}

	for key, value := range m.Headers {
		fmt.Fprintf(&b, "%s: %s\r\n", headerSafe(key), headerSafe(value))
	}

	if len(m.Attachments) > 0 {
		mixedBoundary := newBoundary()
		fmt.Fprintf(&b, "Content-Type: multipart/mixed; boundary=%q\r\n\r\n", mixedBoundary)
		fmt.Fprintf(&b, "--%s\r\n", mixedBoundary)
		m.writeBody(&b)
		for _, att := range m.Attachments {
			fmt.Fprintf(&b, "\r\n--%s\r\n", mixedBoundary)
			writeAttachment(&b, att)
		}

		fmt.Fprintf(&b, "\r\n--%s--\r\n", mixedBoundary)
	} else {
		m.writeBody(&b)
	}

	return []byte(b.String())
}

// newBoundary mints a random multipart delimiter.
//
// RFC 2046 requires a boundary that does not occur inside any part,
// and a fixed string cannot promise that: template variables put
// caller data (a subscriber's own name, a campaign merge field)
// straight into the body, and html/template does not escape a line
// like "--mailyard-mixed-boundary" because it contains no HTML
// metacharacters. With a constant delimiter that line ends the part
// and whatever follows is parsed as a new MIME section - an attacker
// picks the headers and the content type of a part in a message the
// platform signs and sends. 128 random bits makes guessing it a
// non-strategy.
//
// base64url rather than hex, and a three-letter prefix, purely for
// length: the Content-Type header carrying it must stay inside the
// 78-byte line RFC 5322 recommends, and "multipart/alternative" plus
// a 32-char hex boundary does not. Both "-" and "_" are bcharsnospace
// characters, so the encoding needs no substitution.
func newBoundary() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// crypto/rand does not fail in practice. If it somehow does, a
		// clock-derived delimiter is still far better than a constant
		// one, and the message must still go out.
		return fmt.Sprintf("mlr_%d", time.Now().UnixNano())
	}

	return "mlr_" + base64.RawURLEncoding.EncodeToString(raw[:])
}

// writeBody emits the text/html body, wrapped in
// multipart/alternative when both variants exist.
func (m *Message) writeBody(b *strings.Builder) {
	altBoundary := newBoundary()
	switch {
	case m.HTML != "" && m.Text != "":
		fmt.Fprintf(b, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", altBoundary)
		fmt.Fprintf(b, "--%s\r\n", altBoundary)
		writeTextPart(b, contentTypeTextPlain, m.Text)
		fmt.Fprintf(b, "\r\n--%s\r\n", altBoundary)
		writeTextPart(b, contentTypeTextHTML, m.HTML)
		fmt.Fprintf(b, "\r\n--%s--\r\n", altBoundary)
	case m.HTML != "":
		writeTextPart(b, contentTypeTextHTML, m.HTML)
	default:
		writeTextPart(b, contentTypeTextPlain, m.Text)
	}
}

// writeTextPart writes a text body part. Non-ASCII bodies are
// quoted-printable encoded: with no Content-Transfer-Encoding the
// part defaults to 7bit, so raw UTF-8 would be a lie the next hop is
// free to mangle.
func writeTextPart(b *strings.Builder, mediaType, body string) {
	fmt.Fprintf(b, "Content-Type: %s; charset=\"UTF-8\"\r\n", mediaType)
	if isASCII(body) {
		b.WriteString("\r\n")
		b.WriteString(body)

		return
	}

	b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
	w := quotedprintable.NewWriter(b)
	_, _ = w.Write([]byte(body))
	_ = w.Close()
}

// writeAttachment emits one base64 attachment part, re-wrapping the
// already-encoded content at 76 columns per RFC 2045.
func writeAttachment(b *strings.Builder, att Attachment) {
	contentType := att.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	fmt.Fprintf(b, "Content-Type: %s\r\n",
		mime.FormatMediaType(contentType, map[string]string{"name": att.Filename}))
	b.WriteString("Content-Transfer-Encoding: base64\r\n")
	fmt.Fprintf(b, "Content-Disposition: %s\r\n\r\n",
		mime.FormatMediaType("attachment", map[string]string{"filename": att.Filename}))
	content := att.Content
	for len(content) > 76 {
		b.WriteString(content[:76])
		b.WriteString("\r\n")
		content = content[76:]
	}

	if len(content) > 0 {
		b.WriteString(content)
	}
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] > 127 {
			return false
		}
	}

	return true
}

// ValidateAttachments checks filenames, base64 validity, and size
// limits (per attachment and total, in decoded bytes).
func ValidateAttachments(attachments []Attachment, maxAttachmentSize, maxTotalSize int64) error {
	var totalSize int64
	for _, att := range attachments {
		if att.Filename == "" {
			return fmt.Errorf("attachment filename is required")
		}

		decoded, err := base64.StdEncoding.DecodeString(att.Content)
		if err != nil {
			return fmt.Errorf("attachment %q has invalid base64 content", att.Filename)
		}

		size := int64(len(decoded))
		if size > maxAttachmentSize {
			return fmt.Errorf("attachment %q exceeds maximum size of %d bytes", att.Filename, maxAttachmentSize)
		}

		totalSize += size
	}

	if totalSize > maxTotalSize {
		return fmt.Errorf("total attachment size exceeds maximum of %d bytes", maxTotalSize)
	}

	return nil
}
