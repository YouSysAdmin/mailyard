// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"errors"
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
