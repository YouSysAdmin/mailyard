// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import "testing"

func TestNormalizeUnsubscribeLinks(t *testing.T) {
	cases := []struct {
		name       string
		in         SendRequest
		wantErr    bool
		wantURL    string
		wantMailto string
	}{
		{
			name:    "an https target and a mailto travel together",
			in:      SendRequest{ListUnsubscribeURL: " https://app.example.com/u/abc ", ListUnsubscribeMailto: "mailto:unsub@example.com", ListUnsubscribePost: true},
			wantURL: "https://app.example.com/u/abc", wantMailto: "mailto:unsub@example.com",
		},
		{
			// The scheme is what makes the header actionable, and a bare
			// address is the likeliest thing a caller sends.
			name: "a bare address is completed to a mailto",
			in:   SendRequest{ListUnsubscribeMailto: "unsub@example.com"},
			// The absent URL is deliberate: post is off here, so there is
			// nothing for one-click to point at.
			wantMailto: "mailto:unsub@example.com",
		},
		{
			name:       "a mailto keeps its query, which is where the subject line lives",
			in:         SendRequest{ListUnsubscribeMailto: "mailto:unsub@example.com?subject=unsubscribe"},
			wantMailto: "mailto:unsub@example.com?subject=unsubscribe",
		},
		{
			name: "http is allowed, because a self-hosted install may not have TLS in front of it yet",
			in:   SendRequest{ListUnsubscribeURL: "http://localhost:9000/u/abc"}, wantURL: "http://localhost:9000/u/abc",
		},
		{
			name: "a mailto in the URL field is refused rather than silently moved",
			in:   SendRequest{ListUnsubscribeURL: "mailto:unsub@example.com"}, wantErr: true,
		},
		{
			name: "a URL with no host is not a URL",
			in:   SendRequest{ListUnsubscribeURL: "example.com/unsubscribe"}, wantErr: true,
		},
		{
			// The whole reason these fields are checked at all: they
			// become header bytes.
			name: "a line break cannot be smuggled into the header",
			in:   SendRequest{ListUnsubscribeURL: "https://example.com/u\r\nX-Injected: yes"}, wantErr: true,
		},
		{
			// Legal per RFC 3986 and worth accepting, but it should not
			// be what an operator reads back out of the email log.
			name:       "an uppercase scheme is folded",
			in:         SendRequest{ListUnsubscribeMailto: "MAILTO:unsub@example.com"},
			wantMailto: "mailto:unsub@example.com",
		},
		{
			name: "the mailto field will not take a random scheme",
			in:   SendRequest{ListUnsubscribeMailto: "https://example.com/u"}, wantErr: true,
		},
		{
			name: "the mailto field will not take a malformed address",
			in:   SendRequest{ListUnsubscribeMailto: "mailto:not an address"}, wantErr: true,
		},
		{
			name: "one-click with nothing to POST to is refused",
			in:   SendRequest{ListUnsubscribeMailto: "mailto:unsub@example.com", ListUnsubscribePost: true}, wantErr: true,
		},
		{
			name: "a managed list and a caller link contradict each other",
			in:   SendRequest{UnsubscribeListID: "17102f28-18cf-4464-8b73-07520856cd4a", ListUnsubscribeURL: "https://example.com/u"}, wantErr: true,
		},
		{
			name: "a managed list on its own is fine, it mints its own link later",
			in:   SendRequest{UnsubscribeListID: "17102f28-18cf-4464-8b73-07520856cd4a"},
		},
		{
			name: "no unsubscribe at all stays no unsubscribe",
			in:   SendRequest{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := tc.in
			err := normalizeUnsubscribeLinks(&req)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("accepted %+v, want a rejection", tc.in)
				}

				return
			}

			if err != nil {
				t.Fatalf("rejected: %v", err)
			}

			if req.ListUnsubscribeURL != tc.wantURL {
				t.Errorf("url is %q, want %q", req.ListUnsubscribeURL, tc.wantURL)
			}

			if req.ListUnsubscribeMailto != tc.wantMailto {
				t.Errorf("mailto is %q, want %q", req.ListUnsubscribeMailto, tc.wantMailto)
			}
		})
	}
}

func TestTakeUnsubscribeHeaders(t *testing.T) {
	cases := []struct {
		name       string
		headers    map[string]string
		wantURL    string
		wantMailto string
		wantClick  bool
	}{
		{
			name: "the common pair, in the order RFC 2369 shows it",
			headers: map[string]string{
				"List-Unsubscribe":      "<mailto:unsub@example.com>, <https://app.example.com/u/abc>",
				"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
			},
			wantURL: "https://app.example.com/u/abc", wantMailto: "mailto:unsub@example.com", wantClick: true,
		},
		{
			name:    "a single https target",
			headers: map[string]string{"List-Unsubscribe": "<https://app.example.com/u/abc>"},
			wantURL: "https://app.example.com/u/abc",
		},
		{
			// Header names are case insensitive and clients disagree
			// about the spacing.
			name: "odd casing and spacing still count",
			headers: map[string]string{
				"list-unsubscribe":      "  <MAILTO:unsub@example.com>  ",
				"LIST-UNSUBSCRIBE-POST": "list-unsubscribe = one-click",
			},
			wantMailto: "MAILTO:unsub@example.com", wantClick: true,
		},
		{
			// RFC 2369 permits other schemes and none of them are worth
			// refusing a message over.
			name:    "an unusable scheme is dropped, not fatal",
			headers: map[string]string{"List-Unsubscribe": "<ftp://example.com/u>, <https://example.com/u>"},
			wantURL: "https://example.com/u",
		},
		{
			name:    "nothing there is nothing taken",
			headers: map[string]string{"X-Thing": "value"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, m, click := TakeUnsubscribeHeaders(tc.headers)
			if u != tc.wantURL {
				t.Errorf("url is %q, want %q", u, tc.wantURL)
			}

			if m != tc.wantMailto {
				t.Errorf("mailto is %q, want %q", m, tc.wantMailto)
			}

			if click != tc.wantClick {
				t.Errorf("one-click is %v, want %v", click, tc.wantClick)
			}

			// Whatever was found must be gone: the builder is the only
			// thing allowed to emit these, and a forwarded copy would be
			// a second unsanitized header saying something different.
			for k := range tc.headers {
				if k != "X-Thing" {
					t.Errorf("header %q survived the lift", k)
				}
			}
		})
	}
}
