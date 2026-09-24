// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package inbound

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/mailparse"
	imodel "github.com/yousysadmin/mailyard/internal/models/inbound"
)

// What was stored comes back out of the rebuilt file: the parser that
// took the message apart reads the same headers, bodies and attachments
// from the reconstruction, embedded images keep the id the body names,
// and the headers the builder writes itself appear once.
func TestARebuiltMessageParsesBackToWhatWasStored(t *testing.T) {
	received := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	e := &imodel.Email{
		ID:         "01a0d542-10ee-7f37-a27a-99811d218ebd",
		Sender:     "bounce@example.com",
		Recipients: []string{"root@cid.test"},
		MessageID:  "abc@example.com",
		Subject:    "Café report",
		TextBody:   "plain part",
		HTMLBody:   `<p>hi</p><img src="cid:ii_logo">`,
		Headers: map[string]string{
			"From":                      "J\u00fcrgen M\u00fcller <someone@example.com>",
			"To":                        "root@cid.test",
			"Date":                      "Thu, 24 Sep 2026 12:00:00 +0300",
			"Subject":                   "Café report",
			"Message-Id":                "<abc@example.com>",
			"Content-Type":              `multipart/related; boundary="theirs"`,
			"Content-Transfer-Encoding": "7bit",
			"Mime-Version":              "1.0",
			"Received":                  "from mx.example.com by us",
			"X-Priority":                "3",
			"X-Note":                    "caf\u00e9",
			"Dkim-Signature":            "v=1; a=rsa-sha256; bh=abc",
			"Arc-Seal":                  "i=1; a=rsa-sha256",
			"Authentication-Results":    "mailyard; dkim=pass",
		},
		Attachments: []imodel.Attachment{
			{Filename: "logo.png", ContentType: "image/png", ContentID: "ii_logo",
				Content: base64.StdEncoding.EncodeToString([]byte("PNGBYTES"))},
			{Filename: "rows.csv", ContentType: "text/csv",
				Content: base64.StdEncoding.EncodeToString([]byte("a,b\n"))},
		},
		ReceivedAt: received,
	}

	raw, err := rebuild(context.Background(), nil, e)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	parsed, err := mailparse.Parse(raw)
	if err != nil {
		t.Fatalf("the rebuilt message does not parse: %v\n%s", err, raw)
	}

	if parsed.Subject != e.Subject {
		t.Errorf("subject = %q, want %q", parsed.Subject, e.Subject)
	}

	if parsed.From != "someone@example.com" {
		t.Errorf("from = %q, want the From header's address, not the envelope", parsed.From)
	}

	if parsed.MessageID != "abc@example.com" {
		t.Errorf("message id = %q, want the stored one", parsed.MessageID)
	}

	if !parsed.Date.Equal(time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("date = %v, want the sender's Date header, not the receipt time", parsed.Date)
	}

	if strings.TrimSpace(parsed.TextBody) != "plain part" || !strings.Contains(parsed.HTMLBody, "cid:ii_logo") {
		t.Errorf("bodies = %q / %q", parsed.TextBody, parsed.HTMLBody)
	}

	if parsed.Headers["X-Priority"] != "3" || parsed.Headers["Received"] == "" {
		t.Errorf("ordinary headers were dropped: %v", parsed.Headers)
	}

	if len(parsed.Attachments) != 2 {
		t.Fatalf("attachments = %d, want 2: %+v", len(parsed.Attachments), parsed.Attachments)
	}

	logo := parsed.Attachments[0]
	if logo.ContentID != "ii_logo" || string(logo.Content) != "PNGBYTES" || logo.Filename != "logo.png" {
		t.Errorf("embedded image came back as %+v", logo)
	}

	if string(parsed.Attachments[1].Content) != "a,b\n" {
		t.Errorf("attachment came back as %+v", parsed.Attachments[1])
	}

	// The stored Content-Type described the sender's tree, which no
	// longer exists. Written alongside the builder's own it would be a
	// second top-level Content-Type naming a boundary nothing carries.
	head, _, _ := strings.Cut(string(raw), "\r\n\r\n")
	for _, name := range []string{"Content-Type:", "Subject:", "Message-ID:", "Date:", "From:", "MIME-Version:"} {
		if n := strings.Count("\r\n"+head, "\r\n"+name); n != 1 {
			t.Errorf("%s appears %d times in the head, want once:\n%s", name, n, head)
		}
	}

	if strings.Contains(head, "theirs") {
		t.Errorf("the sender's boundary leaked into the rebuilt head:\n%s", head)
	}

	// Decoded on the way in, so the head must be 7-bit on the way out.
	for _, r := range head {
		if r > 127 {
			t.Errorf("the rebuilt head carries raw 8-bit bytes:\n%s", head)

			break
		}
	}

	if parsed.Headers["X-Note"] != "caf\u00e9" || !strings.Contains(parsed.Headers["From"], "J\u00fcrgen") {
		t.Errorf("encoded headers did not decode back: %q / %q", parsed.Headers["X-Note"], parsed.Headers["From"])
	}

	// Signatures over the original bytes cannot hold over these.
	for _, gone := range []string{"Dkim-Signature", "Arc-Seal"} {
		if _, ok := parsed.Headers[gone]; ok {
			t.Errorf("%s was carried into a message it cannot verify", gone)
		}
	}

	if parsed.Headers["Authentication-Results"] == "" {
		t.Error("our own verdict at receipt was dropped")
	}
}

// A message that never parsed has nothing to rebuild from but the
// envelope, and still answers a file rather than an error.
func TestARebuiltMessageFallsBackToTheEnvelope(t *testing.T) {
	e := &imodel.Email{
		Sender:     "bounce@example.com",
		Recipients: []string{"root@cid.test"},
		ReceivedAt: time.Now(),
	}

	raw, err := rebuild(context.Background(), nil, e)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	parsed, err := mailparse.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if parsed.From != "bounce@example.com" || len(parsed.To) != 1 || parsed.To[0] != "root@cid.test" {
		t.Errorf("from/to = %q/%v, want the envelope", parsed.From, parsed.To)
	}
}
