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
	}
	raw := string(m.Build())
	for _, bad := range []string{"\r\nBcc:", "\nBcc:", "\r\nReply-To:"} {
		if strings.Contains(raw, bad) {
			t.Fatalf("injected header %q survived Build:\n%s", bad, raw)
		}
	}
}
