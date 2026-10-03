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

	"github.com/yousysadmin/mailyard/internal/core/mailsign"
)

// Attachment is a file carried inline in the message. Content is
// base64 as received from the API and stays base64 all the way into
// the MIME part.
type Attachment struct {
	Filename    string `json:"filename"`
	Content     string `json:"content,omitempty"`
	ContentType string `json:"content_type"`

	// ContentID, when set, makes the part an embedded one: it is written
	// inline under that id, which is what a cid: URL in the body names.
	ContentID string `json:"content_id,omitempty"`
}

// Message is one outbound email, fully rendered. Headers are custom
// headers already filtered for safety by the caller. The
// ListUnsubscribe fields emit RFC 2369 / 8058 headers.
type Message struct {
	From string

	// EnvelopeFrom overrides the SMTP MAIL FROM when non-empty. The
	// From header is untouched - this is the bounce path (what
	// receivers record as Return-Path), not the visible sender.
	EnvelopeFrom string

	// To is the ENVELOPE: every RCPT TO. It is also the To header
	// unless HeaderTo says otherwise.
	To []string

	// HeaderTo and Cc are what the message DISPLAYS, when that is not
	// the envelope. Submission sets them from the client's own headers
	// and the API from its cc and bcc lists: a Bcc recipient is an
	// RCPT TO left out of the headers, and rebuilding To from the
	// envelope printed every Bcc address for every other recipient to
	// read. Empty means To is the header, which is what a send with
	// neither list wants.
	HeaderTo string
	Cc       string

	// ReplyTo is the Reply-To header, empty for none. It is the one
	// address a caller may set that the platform never verifies: it
	// names where a human answer should go, not who sent the mail.
	ReplyTo string

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

	// Signing wraps the body in multipart/signed as the sender address,
	// S/MIME or PGP. Unlike Sign it lives INSIDE Build: the signature
	// is a part of the body, and the DKIM signature then covers it.
	// Nil means an unsigned body.
	Signing *Signing
}

// Signing is what a message signed as its sender carries, set by the
// caller that holds the sender's key.
type Signing struct {
	Signer mailsign.Signer

	// AutocryptKey is the keydata of an Autocrypt header, empty for
	// none. PGP only: the header is how a client learns the key.
	AutocryptKey string

	// PublicKey, when set, is attached inside the signed entity, so it
	// is covered by the signature. PGP only, for the clients that read
	// a key from a file rather than from the header.
	PublicKey *Attachment
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
// multipart/alternative body when both HTML and text exist. The
// only error is a signature that could not be made, and a caller
// treats that as a message that cannot be sent, never as one that
// goes out unsigned.
func (m *Message) Build() ([]byte, error) {
	var b strings.Builder

	// Validation refuses a line break in any of these upstream. This
	// is the last writer, so it also refuses to emit one: a value that
	// somehow arrives with CR or LF loses them rather than becoming a
	// second header in a message the platform then DKIM-signs.
	//
	// Every field goes through writeHeader, which folds a long one, and
	// an address list with a non-ASCII name is encoded on the way.
	writeHeader(&b, "From", addressHeader(m.From))
	headerTo := m.HeaderTo
	if headerTo == "" {
		to := make([]string, len(m.To))
		for i, addr := range m.To {
			to[i] = headerSafe(addr)
		}

		headerTo = strings.Join(to, ", ")
	}

	writeHeader(&b, "To", addressHeader(headerTo))
	if m.Cc != "" {
		writeHeader(&b, "Cc", addressHeader(m.Cc))
	}

	if m.ReplyTo != "" {
		writeHeader(&b, "Reply-To", addressHeader(m.ReplyTo))
	}

	writeHeader(&b, "Subject", mime.QEncoding.Encode("UTF-8", m.Subject))
	// Date and Message-ID are mandatory originator fields (RFC 5322
	// section 3.6). We emitted neither, and relied on whatever the
	// relay chose to add. That was already a spam signal on its own,
	// and it breaks DKIM in a specific way: both are in the signed
	// header set, so a header added after signing is unsigned, and a
	// receiver that sees an unsigned Date on a signed message has no
	// way to detect a replay.
	fmt.Fprintf(&b, "Date: %s\r\n", m.date().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-ID: <%s>\r\n", headerSafe(m.messageID()))
	if m.EmailID != "" {
		fmt.Fprintf(&b, "%s: %s\r\n", HeaderEmailID, headerSafe(m.EmailID))
	}

	b.WriteString("MIME-Version: 1.0\r\n")

	// RFC 2369 / 8058 List-Unsubscribe. Mailto first, then the https
	// URL. The one-click POST header applies to the https target only.
	// Through headerSafe like every other header here: these two are
	// caller-supplied strings, and not every path to Build runs
	// normalizeUnsubscribeLinks first.
	var luParts []string
	if m.ListUnsubscribeMailto != "" {
		luParts = append(luParts, "<"+headerSafe(m.ListUnsubscribeMailto)+">")
	}

	if m.ListUnsubscribeURL != "" {
		luParts = append(luParts, "<"+headerSafe(m.ListUnsubscribeURL)+">")
	}

	if len(luParts) > 0 {
		writeHeader(&b, "List-Unsubscribe", strings.Join(luParts, ", "))
		if m.ListUnsubscribePost && m.ListUnsubscribeURL != "" {
			b.WriteString("List-Unsubscribe-Post: List-Unsubscribe=One-Click\r\n")
		}
	}

	for key, value := range m.Headers {
		writeHeader(&b, key, unstructured(value))
	}

	if m.Signing != nil && m.Signing.AutocryptKey != "" {
		m.writeAutocrypt(&b)
	}

	var embedded, files []Attachment
	for _, att := range m.Attachments {
		if att.ContentID != "" {
			embedded = append(embedded, att)
		} else {
			files = append(files, att)
		}
	}

	if m.Signing == nil || m.Signing.Signer == nil {
		m.writeEntity(&b, embedded, files, false)

		return []byte(b.String()), nil
	}

	if m.Signing.PublicKey != nil {
		files = append(files, *m.Signing.PublicKey)
	}

	if err := m.writeSigned(&b, embedded, files); err != nil {
		return nil, err
	}

	return []byte(b.String()), nil
}

// writeEntity emits the body as one MIME entity, headers and all:
// multipart/mixed when files are attached, the related or alternative
// body on its own otherwise. This is what a signature covers, which is
// why it is written by one function and not inline in Build.
//
// qp forces every text part into quoted-printable. A signed entity
// needs it: a bare line ending in a space, or a line a hop re-wraps,
// changes the bytes the signature was made over (RFC 3156 section 3).
func (m *Message) writeEntity(b *strings.Builder, embedded, files []Attachment, qp bool) {
	if len(files) == 0 {
		m.writeRelated(b, embedded, qp)

		return
	}

	mixedBoundary := newBoundary()
	fmt.Fprintf(b, "Content-Type: multipart/mixed; boundary=%q\r\n\r\n", mixedBoundary)
	fmt.Fprintf(b, "--%s\r\n", mixedBoundary)
	m.writeRelated(b, embedded, qp)
	for _, att := range files {
		fmt.Fprintf(b, "\r\n--%s\r\n", mixedBoundary)
		writeAttachment(b, att)
	}

	fmt.Fprintf(b, "\r\n--%s--\r\n", mixedBoundary)
}

// writeSigned emits the RFC 1847 multipart/signed wrapper: the entity
// as the first part, byte for byte what the signer saw, and the
// detached signature as the second.
//
// The entity is rendered on its own first and canonicalized to CRLF
// before signing, because the signature covers the bytes the receiver
// gets and an SMTP DATA writer turns a bare LF into CRLF on the way.
func (m *Message) writeSigned(b *strings.Builder, embedded, files []Attachment) error {
	var inner strings.Builder
	m.writeEntity(&inner, embedded, files, true)
	entity := canonicalCRLF(inner.String())

	part, err := m.Signing.Signer.Sign([]byte(entity))
	if err != nil {
		return err
	}

	boundary := newBoundary()
	fmt.Fprintf(b, "Content-Type: multipart/signed; protocol=%q; micalg=%s; boundary=%q\r\n\r\n",
		m.Signing.Signer.Protocol(), m.Signing.Signer.MicAlg(), boundary)
	fmt.Fprintf(b, "--%s\r\n", boundary)
	b.WriteString(entity)
	fmt.Fprintf(b, "\r\n--%s\r\n", boundary)
	fmt.Fprintf(b, "Content-Type: %s\r\n", part.ContentType)
	fmt.Fprintf(b, "Content-Transfer-Encoding: %s\r\n", part.TransferEncoding)
	fmt.Fprintf(b, "Content-Disposition: %s\r\n\r\n",
		mime.FormatMediaType("attachment", map[string]string{"filename": part.Filename}))
	if part.TransferEncoding == "base64" {
		writeBase64Lines(b, base64.StdEncoding.EncodeToString(part.Body))
	} else {
		b.WriteString(canonicalCRLF(string(part.Body)))
	}

	fmt.Fprintf(b, "\r\n--%s--\r\n", boundary)

	return nil
}

// writeAutocrypt emits the Autocrypt header (autocrypt.org level 1):
// the sender address and its key, folded, because the key is a few
// kilobytes of base64 and a header line has a length.
func (m *Message) writeAutocrypt(b *strings.Builder) {
	addr := EnvelopeAddress(m.From)
	if addr == "" {
		return
	}

	fmt.Fprintf(b, "Autocrypt: addr=%s; keydata=\r\n", headerSafe(addr))
	key := headerSafe(m.Signing.AutocryptKey)
	for len(key) > 76 {
		b.WriteString(" " + key[:76] + "\r\n")
		key = key[76:]
	}

	b.WriteString(" " + key + "\r\n")
}

// canonicalCRLF turns every bare LF into CRLF and leaves CRLF alone.
func canonicalCRLF(s string) string {
	if !strings.Contains(s, "\n") {
		return s
	}

	var out strings.Builder
	out.Grow(len(s) + len(s)/40)
	for i := range len(s) {
		if s[i] == '\n' && (i == 0 || s[i-1] != '\r') {
			out.WriteByte('\r')
		}

		out.WriteByte(s[i])
	}

	return out.String()
}

// writeRelated emits the body, wrapped in multipart/related with the
// embedded parts when there are any. A cid: URL resolves only inside
// the related container the body sits in (RFC 2387), so an embedded
// image written as a plain sibling shows as a broken image and a loose
// file in clients that follow it.
func (m *Message) writeRelated(b *strings.Builder, embedded []Attachment, qp bool) {
	if len(embedded) == 0 {
		m.writeBody(b, qp)

		return
	}

	boundary := newBoundary()
	fmt.Fprintf(b, "Content-Type: multipart/related; boundary=%q\r\n\r\n", boundary)
	fmt.Fprintf(b, "--%s\r\n", boundary)
	m.writeBody(b, qp)
	for _, att := range embedded {
		fmt.Fprintf(b, "\r\n--%s\r\n", boundary)
		writeAttachment(b, att)
	}

	fmt.Fprintf(b, "\r\n--%s--\r\n", boundary)
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
func (m *Message) writeBody(b *strings.Builder, qp bool) {
	altBoundary := newBoundary()
	switch {
	case m.HTML != "" && m.Text != "":
		fmt.Fprintf(b, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", altBoundary)
		fmt.Fprintf(b, "--%s\r\n", altBoundary)
		writeTextPart(b, contentTypeTextPlain, m.Text, qp)
		fmt.Fprintf(b, "\r\n--%s\r\n", altBoundary)
		writeTextPart(b, contentTypeTextHTML, m.HTML, qp)
		fmt.Fprintf(b, "\r\n--%s--\r\n", altBoundary)
	case m.HTML != "":
		writeTextPart(b, contentTypeTextHTML, m.HTML, qp)
	default:
		writeTextPart(b, contentTypeTextPlain, m.Text, qp)
	}
}

// writeTextPart writes a text body part. Non-ASCII bodies are
// quoted-printable encoded: with no Content-Transfer-Encoding the
// part defaults to 7bit, so raw UTF-8 would be a lie the next hop is
// free to mangle. qp forces the encoding for an ASCII body too, which
// a signed entity needs.
func writeTextPart(b *strings.Builder, mediaType, body string, qp bool) {
	fmt.Fprintf(b, "Content-Type: %s; charset=\"UTF-8\"\r\n", mediaType)
	if !qp && isASCII(body) {
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
	// FormatMediaType wants a bare type and answers "" for one carrying
	// a parameter, so the type is parsed first and the name added to
	// its parameters.
	mediaType, params, err := mime.ParseMediaType(att.ContentType)
	if err != nil || mediaType == "" {
		mediaType, params = "application/octet-stream", map[string]string{}
	}

	params["name"] = att.Filename
	fmt.Fprintf(b, "Content-Type: %s\r\n", mime.FormatMediaType(mediaType, params))
	b.WriteString("Content-Transfer-Encoding: base64\r\n")
	disposition := "attachment"
	if att.ContentID != "" {
		disposition = "inline"
		// The id goes inside angle brackets, so one carrying a bracket or
		// a space of its own would end the token early and match nothing.
		id := strings.Map(func(r rune) rune {
			if r == '<' || r == '>' || r == ' ' || r == '\t' {
				return -1
			}

			return r
		}, headerSafe(att.ContentID))
		fmt.Fprintf(b, "Content-ID: <%s>\r\n", id)
	}

	fmt.Fprintf(b, "Content-Disposition: %s\r\n\r\n",
		mime.FormatMediaType(disposition, map[string]string{"filename": att.Filename}))
	writeBase64Lines(b, att.Content)
}

// writeBase64Lines re-wraps already-encoded content at 76 columns per
// RFC 2045.
func writeBase64Lines(b *strings.Builder, content string) {
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

		if att.ContentType != "" {
			if _, _, err := mime.ParseMediaType(att.ContentType); err != nil {
				return fmt.Errorf("attachment %q has an invalid content type %q", att.Filename, att.ContentType)
			}
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
