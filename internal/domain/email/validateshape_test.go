// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"errors"
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/env"
)

// ValidateShape is what the sandbox path runs, so it has to hold the
// line the full Validate holds on a header a caller controls - and it
// needs no store, which is what lets it run there at all.
func TestValidateShapeRefusesAForgedHeaderWithoutAStore(t *testing.T) {
	s := &Service{Sending: env.SendingConfig{MaxRecipients: 2}}
	good := func() *SendRequest {
		return &SendRequest{From: "a@example.com", To: []string{"b@example.com"}, Subject: "s", Text: "t"}
	}

	if err := s.ValidateShape(good()); err != nil {
		t.Fatalf("a plain request: %v", err)
	}

	for name, mutate := range map[string]func(*SendRequest){
		"list_unsubscribe_url":    func(r *SendRequest) { r.ListUnsubscribeURL = "https://x/\r\nBcc: e@evil.test" },
		"list_unsubscribe_mailto": func(r *SendRequest) { r.ListUnsubscribeMailto = "u@x\r\nBcc: e@evil.test" },
		"custom header":           func(r *SendRequest) { r.Headers = map[string]string{"X-A": "1\r\nBcc: e@evil.test"} },
		"from":                    func(r *SendRequest) { r.From = "a@example.com (x\r\nBcc: e@evil.test)" },
		"reply_to":                func(r *SendRequest) { r.ReplyTo = "a@example.com (x\r\nBcc: e@evil.test)" },
		"reply_to not an address": func(r *SendRequest) { r.ReplyTo = "not an address" },
		"reply-to as a header":    func(r *SendRequest) { r.Headers = map[string]string{"Reply-To": "e@evil.test"} },
		"recipient ceiling":       func(r *SendRequest) { r.To = []string{"a@x.test", "b@x.test", "c@x.test"} },
	} {
		r := good()
		mutate(r)
		var re *RequestError
		if err := s.ValidateShape(r); !errors.As(err, &re) {
			t.Errorf("%s: got %v, want a request error", name, err)
		}
	}
}

// Reply-To has a field of its own, so the header is refused with a
// pointer to it rather than a flat no, and the field itself takes a
// display name the way From does.
func TestReplyToIsAFieldNotAHeader(t *testing.T) {
	s := &Service{Sending: env.SendingConfig{MaxRecipients: 2}}
	r := &SendRequest{From: "a@example.com", To: []string{"b@example.com"}, Subject: "s", Text: "t",
		Headers: map[string]string{"reply-to": "c@example.com"}}
	err := s.ValidateShape(r)
	if err == nil || !strings.Contains(err.Error(), "use reply_to") {
		t.Fatalf("a Reply-To header: got %v, want a refusal naming reply_to", err)
	}

	r = &SendRequest{From: "a@example.com", To: []string{"b@example.com"}, Subject: "s", Text: "t",
		ReplyTo: "Support <help@example.com>"}
	if err := s.ValidateShape(r); err != nil {
		t.Fatalf("a named reply_to: %v", err)
	}

	if got := withDisplayRecipients(r)[HeaderReplyTo]; got != r.ReplyTo {
		t.Fatalf("reply_to rides in the header map as %q, want %q", got, r.ReplyTo)
	}
}
