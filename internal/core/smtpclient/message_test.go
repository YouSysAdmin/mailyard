// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpclient

import (
	"strings"
	"testing"
)

// A line break in an address or a custom header must not become a
// second header. Validation refuses it upstream - this pins the last
// writer, which is what a value reaching it some other way meets.
func TestBuildNeverEmitsAnInjectedHeader(t *testing.T) {
	m := &Message{
		From:    "you@example.com (x\r\nBcc: harvest@evil.test)",
		To:      []string{"a@example.com\nBcc: b@evil.test"},
		Subject: "s",
		Text:    "body",
		Headers: map[string]string{"X-Custom\r\nBcc": "v\r\nReply-To: c@evil.test"},
		// Caller-supplied values the builder writes as headers itself.
		ListUnsubscribeURL:    "https://x/\r\nBcc: d@evil.test",
		ListUnsubscribeMailto: "u@x.test\r\nBcc: e@evil.test",
		MessageID:             "id@x.test>\r\nBcc: f@evil.test",
		EmailID:               "0195\r\nBcc: g@evil.test",
	}
	raw := string(m.Build())
	for _, bad := range []string{"\r\nBcc:", "\nBcc:", "\r\nReply-To:"} {
		if strings.Contains(raw, bad) {
			t.Fatalf("injected header %q survived Build:\n%s", bad, raw)
		}
	}
}

// The envelope is delivery, the header is display. A Bcc recipient is
// an RCPT TO the client left out of its headers, and rebuilding To from
// the envelope printed it for every other recipient.
func TestBuildKeepsTheClientsToHeaderOverTheEnvelope(t *testing.T) {
	m := &Message{
		From:     "you@example.com",
		To:       []string{"a@example.com", "hidden@example.com"},
		HeaderTo: "a@example.com",
		Cc:       "c@example.com",
		Subject:  "s",
		Text:     "body",
	}
	raw := string(m.Build())
	if strings.Contains(raw, "hidden@example.com") {
		t.Fatalf("a Bcc recipient was printed in the headers:\n%s", raw)
	}

	if !strings.Contains(raw, "To: a@example.com\r\n") || !strings.Contains(raw, "Cc: c@example.com\r\n") {
		t.Fatalf("client To/Cc headers were not written:\n%s", raw)
	}
}

// Reply-To is a field, not a custom header, so it is written by the
// builder once and passes through headerSafe like every other address.
func TestBuildWritesReplyToOnceAndSafely(t *testing.T) {
	m := &Message{
		From:    "noreply@example.com",
		To:      []string{"a@example.com"},
		ReplyTo: "Support <help@example.com>\r\nBcc: e@evil.test",
		Subject: "s",
		Text:    "body",
	}
	raw := string(m.Build())
	if strings.Count(raw, "Reply-To:") != 1 {
		t.Fatalf("Reply-To written %d times:\n%s", strings.Count(raw, "Reply-To:"), raw)
	}

	if strings.Contains(raw, "\r\nBcc:") {
		t.Fatalf("a line break in Reply-To became a header:\n%s", raw)
	}

	m.ReplyTo = ""
	if strings.Contains(string(m.Build()), "Reply-To") {
		t.Fatal("an empty ReplyTo wrote a header")
	}
}
