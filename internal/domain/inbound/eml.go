// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package inbound

import (
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"net/mail"
	"net/textproto"
	"strings"

	"github.com/yousysadmin/mailyard/internal/core/blob"
	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
	imodel "github.com/yousysadmin/mailyard/internal/models/inbound"
)

// builderHeaders are the headers the message builder writes from the
// message's own fields, or that describe the MIME tree being rebuilt.
// Copying the stored value of one of these would repeat it, or describe
// a body structure the rebuilt message no longer has.
var builderHeaders = map[string]bool{
	"From":                      true,
	"To":                        true,
	"Cc":                        true,
	"Reply-To":                  true,
	"Subject":                   true,
	"Date":                      true,
	"Message-Id":                true,
	"Mime-Version":              true,
	"Content-Type":              true,
	"Content-Transfer-Encoding": true,
	"Content-Disposition":       true,
}

// signatureHeaders were computed over the original bytes and cannot
// hold over rebuilt ones. Carried across they would be signatures that
// fail to verify on a message the log says passed, so the rebuilt
// message carries none. Authentication-Results stays: it is the verdict
// this installation recorded at receipt, not a claim about these bytes.
var signatureHeaders = map[string]bool{
	"Dkim-Signature":             true,
	"Arc-Seal":                   true,
	"Arc-Message-Signature":      true,
	"Arc-Authentication-Results": true,
	"X-Google-Dkim-Signature":    true,
}

// addressHeader re-encodes a decoded address header for the wire. The
// stored map holds display names already RFC 2047-decoded, and the
// builder writes headers as given, so a name with an accent would go
// out as raw 8-bit bytes. A list that does not parse goes out as it is.
func addressHeader(v string) string {
	list, err := mail.ParseAddressList(v)
	if err != nil || len(list) == 0 {
		return v
	}

	out := make([]string, len(list))
	for i, a := range list {
		out[i] = a.String()
	}

	return strings.Join(out, ", ")
}

// rebuild renders a parsed message back into RFC 5322 bytes from what
// was stored: the headers, both bodies and the attachments.
//
// A reconstruction, not the original. The wire bytes are kept only for
// a message that failed to parse, so for every other one this is the
// closest thing to an .eml there is. The header map holds the first
// value of each header, so a repeated Received line is reduced to one,
// and the MIME tree is the builder's rather than the sender's.
func rebuild(ctx context.Context, bs blob.Store, e *imodel.Email) ([]byte, error) {
	msg := &smtpclient.Message{
		From:      addressHeader(e.Headers["From"]),
		To:        e.Recipients,
		HeaderTo:  addressHeader(e.Headers["To"]),
		Cc:        addressHeader(e.Headers["Cc"]),
		ReplyTo:   addressHeader(e.Headers["Reply-To"]),
		Subject:   e.Subject,
		HTML:      e.HTMLBody,
		Text:      e.TextBody,
		MessageID: e.MessageID,
		Date:      e.ReceivedAt,
		Headers:   map[string]string{},
	}
	if msg.From == "" {
		msg.From = e.Sender
	}

	// The builder mints a random Message-ID when given none, and a
	// mail client would then thread two downloads of one message as
	// two messages. A message that arrived without one gets a stable id
	// under a reserved domain instead, recognisably ours and never
	// anybody's. The bytes still differ between downloads - boundaries
	// are random - so this is about identity, not reproducibility.
	if msg.MessageID == "" {
		msg.MessageID = e.ID + "@inbound.invalid"
	}

	if date, err := mail.ParseDate(e.Headers["Date"]); err == nil {
		msg.Date = date
	}

	for name, value := range e.Headers {
		key := textproto.CanonicalMIMEHeaderKey(name)
		if builderHeaders[key] || signatureHeaders[key] {
			continue
		}

		// Decoded on the way in, so encoded on the way out. Plain ASCII
		// comes back untouched.
		msg.Headers[name] = mime.QEncoding.Encode("UTF-8", value)
	}

	for _, a := range e.Attachments {
		raw, err := blob.Load(ctx, bs, a.StorageKey, a.Content, a.Filename)
		if err != nil {
			return nil, fmt.Errorf("load attachment %q: %w", a.Filename, err)
		}

		msg.Attachments = append(msg.Attachments, smtpclient.Attachment{
			Filename:    a.Filename,
			ContentType: a.ContentType,
			ContentID:   a.ContentID,
			Content:     base64.StdEncoding.EncodeToString(raw),
		})
	}

	return msg.Build(), nil
}
