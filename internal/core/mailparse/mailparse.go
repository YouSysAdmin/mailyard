// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package mailparse turns a raw RFC 5322 message into a normalized
// struct: decoded headers, a text body, an html body and attachments.
//
// Every surface that accepts wire-format mail comes through here - the
// submission listener, the inbound MX listener and the sandbox - so that
// a message means the same thing however it arrived.
//
// NOTHING HERE REFUSES A MESSAGE FOR BEING MALFORMED, and that is the
// governing decision. Mail on the internet is broken in every way it can
// be: absent dates, unparseable content types, charsets that lie,
// headers that are not UTF-8. A parser that returned an error for each
// of those would reject a large slice of real, deliverable mail. So each
// of them has a documented fallback and the message goes through. The
// two things that DO fail are a message that is not a message at all,
// and a MIME tree built to exhaust the machine reading it - see walk.go.
package mailparse

import (
	"bytes"
	"fmt"
	"net/mail"
	"strings"
	"time"
)

const contentTypeTextPlain = "text/plain"

// Attachment is one file carried by a message, already decoded.
type Attachment struct {
	Filename    string
	ContentType string
	Content     []byte
	Size        int64
}

// Email is the normalized shape Parse produces.
//
// Headers keeps the first value of every header under its canonical
// name, decoded. Raw is the message exactly as it arrived, which is what
// gets stored: this struct is an interpretation, and the bytes are the
// record.
type Email struct {
	MessageID   string
	From        string
	To          []string
	Cc          []string
	Subject     string
	Date        time.Time
	TextBody    string
	HTMLBody    string
	Headers     map[string]string
	Attachments []Attachment
	Raw         []byte
}

// Parse reads a raw RFC 5322 message.
func Parse(raw []byte) (*Email, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("read message: %w", err)
	}

	out := &Email{Raw: raw, Headers: make(map[string]string, len(msg.Header))}
	for name, values := range msg.Header {
		// The first value only. A header repeated with different
		// contents is either a bug or an attack, and picking the first
		// is what every reader of this struct assumes.
		if len(values) > 0 {
			out.Headers[name] = headerText(values[0])
		}
	}

	out.describe(msg)

	media, params := mediaType(msg.Header.Get("Content-Type"), contentTypeTextPlain)
	if !strings.HasPrefix(media, "multipart/") {
		body, err := readText(msg.Body, msg.Header.Get("Content-Transfer-Encoding"), params["charset"])
		if err != nil {
			return nil, err
		}

		out.addBody(media, body)

		return out, nil
	}

	// The one malformation this does refuse. A multipart container with
	// no boundary has no readable content at all - there is no fallback
	// to choose, because nothing says where the parts begin.
	boundary := params["boundary"]
	if boundary == "" {
		return nil, fmt.Errorf("multipart with no boundary")
	}

	walk := &collector{into: out, partsLeft: maxMIMEParts}
	if err := walk.gather(msg.Body, boundary, 1); err != nil {
		return nil, err
	}

	return out, nil
}

// describe fills in the fields that come from headers.
//
// Read from Headers where the value is plain text, since that map is
// already decoded, and from msg.Header where it has to be parsed as an
// address or a date - those want the value as it was sent.
func (e *Email) describe(msg *mail.Message) {
	// net/mail canonicalizes to Message-Id, so that is the only spelling
	// this map can hold. The angle brackets are RFC 5322 syntax rather
	// than part of the identifier, and every comparison downstream is
	// against a bare id.
	e.MessageID = strings.Trim(e.Headers["Message-Id"], "<>")
	e.Subject = e.Headers["Subject"]

	e.From = oneAddress(msg.Header.Get("From"))
	e.To = addressList(msg.Header.Get("To"))
	e.Cc = addressList(msg.Header.Get("Cc"))

	// An absent or unreadable Date becomes the time it was read. A zero
	// time would sort to the beginning of every list and read as 1 Jan
	// year 1 on screen, which is worse than being approximately right.
	if sent, err := mail.ParseDate(msg.Header.Get("Date")); err == nil {
		e.Date = sent
	} else {
		e.Date = time.Now().UTC()
	}
}

// addBody files a decoded body under the type that described it.
//
// A message may carry more than one part of the same type - a
// multipart/mixed of two text/plain parts is legal and does happen - so
// a second one is appended rather than dropped or allowed to overwrite
// the first. Anything that is neither text nor html and was not claimed
// as an attachment is discarded: there is nowhere to put it.
func (e *Email) addBody(media, body string) {
	switch media {
	case contentTypeTextPlain:
		e.TextBody = joinBody(e.TextBody, body)
	case "text/html":
		e.HTMLBody = joinBody(e.HTMLBody, body)
	}
}

func joinBody(existing, next string) string {
	if existing == "" {
		return next
	}

	return existing + "\n" + next
}

// oneAddress reduces a From header to a bare address.
//
// The header as sent is kept when it cannot be parsed, because an
// unparseable From is still evidence of who sent this and throwing it
// away leaves the reader with nothing at all.
func oneAddress(v string) string {
	if v == "" {
		return ""
	}

	if addr, err := mail.ParseAddress(v); err == nil {
		return addr.Address
	}

	return v
}

// addressList reduces To or Cc to bare addresses.
//
// Unlike From this answers nil on a parse failure rather than the raw
// header: the field is a LIST, and one string that happens to contain
// commas would be read downstream as a single recipient.
func addressList(v string) []string {
	if v == "" {
		return nil
	}

	addrs, err := mail.ParseAddressList(v)
	if err != nil {
		return nil
	}

	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.Address)
	}

	return out
}
