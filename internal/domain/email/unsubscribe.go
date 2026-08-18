// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"net/mail"
	"net/url"
	"strings"
)

// maxUnsubscribeLinkLen bounds each List-Unsubscribe target.
//
// The API binding carries its own max, but the relay does not go
// through a DTO - it lifts these straight off a submitted message. So
// the limit lives here, where both paths pass.
const maxUnsubscribeLinkLen = 1000

// reservedHeaderHint turns a refusal into a pointer for the reserved
// headers that do have a caller-facing equivalent. Without it the
// answer to "how do I set List-Unsubscribe" is a flat no, when the
// real answer is "through these fields".
var reservedHeaderHint = map[string]string{
	"list-unsubscribe":      "use list_unsubscribe_url and list_unsubscribe_mailto",
	"list-unsubscribe-post": "use list_unsubscribe_post",
}

// normalizeUnsubscribeLinks checks the caller-supplied RFC 2369
// targets and rewrites them into the form the builder emits.
//
// These become header bytes, so the checks are not cosmetic: a value
// carrying CR or LF would inject a header of the caller's choosing
// into every message. url.Parse already refuses control bytes, but
// this is worth failing twice rather than depending on a property of
// the standard library staying true.
//
// Unlike the rest of Validate this mutates the request, which is the
// point - it is the one place the API path and the relay path both
// pass through, so canonicalizing anywhere else would mean doing it
// twice.
func normalizeUnsubscribeLinks(req *SendRequest) error {
	req.ListUnsubscribeURL = strings.TrimSpace(req.ListUnsubscribeURL)
	req.ListUnsubscribeMailto = strings.TrimSpace(req.ListUnsubscribeMailto)

	if u := req.ListUnsubscribeURL; u != "" {
		if len(u) > maxUnsubscribeLinkLen {
			return reqErrf("list_unsubscribe_url is longer than %d characters", maxUnsubscribeLinkLen)
		}

		if strings.ContainsAny(u, "\r\n") {
			return reqErrf("list_unsubscribe_url contains a line break")
		}

		parsed, err := url.Parse(u)
		if err != nil {
			return reqErrf("list_unsubscribe_url %q is not a valid URL", u)
		}

		// Scheme before host, so a mailto here gets the answer that
		// tells the caller where it belongs. Checked the other way round
		// it fails as "no host", since a mailto has none.
		switch parsed.Scheme {
		case "http", "https":
		case "mailto":
			return reqErrf("list_unsubscribe_url %q is a mailto - put it in list_unsubscribe_mailto", u)
		case "":
			return reqErrf("list_unsubscribe_url %q needs an http:// or https:// prefix", u)
		default:
			return reqErrf("list_unsubscribe_url must be http or https, got %q", parsed.Scheme)
		}

		if parsed.Host == "" {
			return reqErrf("list_unsubscribe_url %q has no host", u)
		}
	}

	if m := req.ListUnsubscribeMailto; m != "" {
		if len(m) > maxUnsubscribeLinkLen {
			return reqErrf("list_unsubscribe_mailto is longer than %d characters", maxUnsubscribeLinkLen)
		}

		if strings.ContainsAny(m, "\r\n") {
			return reqErrf("list_unsubscribe_mailto contains a line break")
		}

		// A bare address is the first thing a caller reaches for and
		// the intent is not ambiguous, so complete it rather than
		// refuse. Left uncompleted it would ship as <user@example.com>,
		// which is a syntactically valid header nobody can act on.
		if !strings.Contains(m, ":") {
			m = "mailto:" + m
		}

		parsed, err := url.Parse(m)
		if err != nil || parsed.Scheme != "mailto" {
			return reqErrf("list_unsubscribe_mailto %q must be a mailto: address", req.ListUnsubscribeMailto)
		}

		if _, err := mail.ParseAddress(parsed.Opaque); err != nil {
			return reqErrf("list_unsubscribe_mailto %q does not carry a valid address", req.ListUnsubscribeMailto)
		}

		// Reassembled rather than stored verbatim so the scheme comes
		// out lowercase. A submitted header may well say MAILTO:, which
		// is legal and which no operator wants to read back.
		req.ListUnsubscribeMailto = parsed.String()
	}

	// RFC 8058 one-click is an HTTPS POST. A mailto target cannot
	// receive one, so the flag without a URL would emit a header
	// promising a capability the message does not have - and mailbox
	// providers do check.
	if req.ListUnsubscribePost && req.ListUnsubscribeURL == "" {
		return reqErrf("list_unsubscribe_post requires an http(s) list_unsubscribe_url: one-click is a POST and a mailto cannot receive one")
	}

	// Both would be a message whose header points somewhere Mailyard
	// does not control while Mailyard is the one filtering the send.
	// Recipients who used the caller's link would keep receiving the
	// mail, because the opt-out never reached the list that gates it.
	if req.UnsubscribeListID != "" && (req.ListUnsubscribeURL != "" || req.ListUnsubscribeMailto != "") {
		return reqErrf("unsubscribe_list_id makes Mailyard mint its own List-Unsubscribe, so it cannot be combined with list_unsubscribe_url or list_unsubscribe_mailto")
	}

	return nil
}

// TakeUnsubscribeHeaders lifts RFC 2369 List-Unsubscribe and
// List-Unsubscribe-Post off a submitted message, splitting the URI
// list into the two slots the builder emits from, and REMOVES both
// from the header map.
//
// Removal is what makes the builder the single emitter. Any path that
// forwards custom headers would otherwise send a second, unsanitized
// copy of a header the recipient's provider reads as a promise about
// how unsubscribing works.
//
// A URI form that is neither http(s) nor mailto is dropped rather than
// rejected: RFC 2369 permits others and refusing the whole message
// over an ftp: target nobody has used since 1998 is the wrong trade.
func TakeUnsubscribeHeaders(headers map[string]string) (httpURL, mailto string, oneClick bool) {
	if headers == nil {
		return "", "", false
	}

	var raw, post string
	for k, v := range headers {
		switch {
		case strings.EqualFold(k, "List-Unsubscribe"):
			raw = v
			delete(headers, k)
		case strings.EqualFold(k, "List-Unsubscribe-Post"):
			post = v
			delete(headers, k)
		}
	}

	for part := range strings.SplitSeq(raw, ",") {
		uri := strings.Trim(strings.TrimSpace(part), "<>")
		switch {
		case strings.HasPrefix(strings.ToLower(uri), "mailto:") && mailto == "":
			mailto = uri
		case (strings.HasPrefix(strings.ToLower(uri), "http://") ||
			strings.HasPrefix(strings.ToLower(uri), "https://")) && httpURL == "":
			httpURL = uri
		}
	}

	// RFC 8058 spells the value List-Unsubscribe=One-Click. Matched
	// loosely because the header is worth honoring from a client that
	// got the spacing or the case wrong, and its only effect is to
	// stamp the canonical form back out.
	oneClick = strings.Contains(strings.ToLower(strings.ReplaceAll(post, " ", "")), "one-click")

	return httpURL, mailto, oneClick
}
