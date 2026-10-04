// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"strings"
	"testing"

	coretracking "github.com/yousysadmin/mailyard/internal/core/tracking"
	ulmodel "github.com/yousysadmin/mailyard/internal/models/unsubscribelist"
)

// A plain send resolves the reserved names as a template send does: the
// web view link always, the unsubscribe link on a scoped one-recipient
// send, nothing left as text. Other {{ ... }} is the caller's text.
func TestAPlainSendResolvesTheReservedNames(t *testing.T) {
	list := &ulmodel.List{ID: "0198f6a1-3c7e-7b21-9f4d-2a5c8e0b1d33", Name: "shipping", Active: true}

	send := func(listID string) (subject, html, text string) {
		t.Helper()

		req := scopedSend(listID)
		req.Subject = "Shipped {{mailyard_web_view_url}}"
		req.HTML = `<a href="{{ mailyard_web_view_url }}">online</a> <a href="{{ mailyard_mail_web_link }}">alias</a> ` +
			`<a href="{{  mailyard_unsubscribe_url  }}">out</a> {{ order }} {{ mailyard_other }}`
		req.Text = "online: {{ mailyard_web_view_url }}"
		markSystemVars(req)

		e, _, err := acceptingService(&acceptEmails{}, nil, list).Send(t.Context(), "proj-a", "", "", req)
		if err != nil {
			t.Fatal(err)
		}

		return e.Subject, e.HTMLBody, e.TextBody
	}

	subject, html, text := send(list.ID)
	for _, part := range []string{subject, html, text} {
		if strings.Contains(part, "mailyard_web_view_url") || strings.Contains(part, "mailyard_mail_web_link") ||
			strings.Contains(part, "mailyard_unsubscribe_url") || strings.Contains(part, "__mailyard_") {
			t.Errorf("a reserved name or placeholder reached the message: %s", part)
		}
	}

	if strings.Count(html, "https://mail.example.test/tracking/view/") != 2 || !strings.Contains(text, "/tracking/view/") ||
		!strings.Contains(subject, "/tracking/view/") {
		t.Errorf("web view links missing: %q %q %q", subject, html, text)
	}

	if !strings.Contains(html, "/tracking/unsubscribe/") {
		t.Errorf("a scoped send carries no unsubscribe link: %s", html)
	}

	if !strings.Contains(html, "{{ order }}") || !strings.Contains(html, "{{ mailyard_other }}") {
		t.Errorf("text that is not a reserved name was touched: %s", html)
	}

	// Unscoped, there is nothing to unsubscribe from, and the link goes.
	_, html, _ = send("")
	if strings.Contains(html, "/tracking/unsubscribe/") || strings.Contains(html, "mailyard_unsubscribe_url") {
		t.Errorf("an unscoped send: %s", html)
	}

	if !strings.Contains(html, `href="">out`) {
		t.Errorf("the unresolvable link was not emptied: %s", html)
	}
}

// What a sandbox capture keeps: no reserved name and no placeholder.
func TestASandboxCaptureDropsTheReservedNames(t *testing.T) {
	req := scopedSend("")
	req.HTML = `<a href="{{ mailyard_web_view_url }}">online</a>`
	markSystemVars(req)
	stripSystemVars(req)

	if req.HTML != `<a href="">online</a>` {
		t.Fatalf("captured %q", req.HTML)
	}
}

// Marking leaves a body with no reserved name byte for byte alone.
func TestMarkingLeavesOtherTextAlone(t *testing.T) {
	for _, in := range []string{"", "plain", "{{ name }}", "{{mailyard}}", "{ mailyard_web_view_url }", "{{ MAILYARD_WEB_VIEW_URL }}"} {
		if got := coretracking.MarkSystemVars(in); got != in {
			t.Errorf("%q became %q", in, got)
		}
	}
}
