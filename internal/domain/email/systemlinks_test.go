// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/render"
	coretracking "github.com/yousysadmin/mailyard/internal/core/tracking"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
)

// A template referencing a reserved variable must RENDER, on every
// surface and not only in a campaign.
//
// This is the half that used to be missing. RenderTemplate passed the
// caller's data map through untouched and rendered strict, so
// {{ mailyard_unsubscribe_url }} in a transactional template failed the
// whole send with "map has no entry for key" - while the identical
// template sent by a campaign worked, because the runner injected the
// names itself. Nothing in the template told an author which of the two
// they had written.
func TestAReservedVariableRendersOnAStrictSend(t *testing.T) {
	body := `<a href="{{ mailyard_unsubscribe_url }}">out</a> ` +
		`<a href="{{ mailyard_web_view_url }}">online</a>`

	// The caller's data, exactly as a transactional send supplies it -
	// no reserved names in it anywhere.
	strict := &render.Renderer{MissingKeyBehavior: render.MissingKeyError}
	if _, err := strict.Render(&render.Input{Subject: "hi", HTML: body},
		map[string]any{"name": "Ada"}); err == nil {
		t.Fatal("without the injection a strict render must fail - if this passes, " +
			"the test no longer proves what RenderTemplate is for")
	}

	// What RenderTemplate does now.
	out, err := strict.Render(&render.Input{Subject: "hi", HTML: body},
		coretracking.WithSystemVars(map[string]any{"name": "Ada"}))
	if err != nil {
		t.Fatalf("render with system vars injected: %v", err)
	}

	if !coretracking.HasSystemSentinels(out.HTML) {
		t.Errorf("rendered body carries no placeholder to substitute later: %s", out.HTML)
	}
}

// The second half: placeholders become this message's URLs, or go away.
func TestResolveSystemLinks(t *testing.T) {
	// Root-relative, so html/template's URL filter passes them through
	// rather than writing ZgotmplZ - which is why the render above can
	// be checked for them at all.
	seeded := coretracking.WithSystemVars(nil)
	unsub, _ := seeded[coretracking.VarUnsubscribe].(string)
	webView, _ := seeded[coretracking.VarWebView].(string)

	body := func() *emailmodel.Email {
		return &emailmodel.Email{
			ID:       "9e2f6f11-cdd3-4058-86f2-29f3ad60b06a",
			Subject:  "read it at " + webView,
			HTMLBody: `<a href="` + unsub + `">out</a>`,
			TextBody: "out: " + unsub + " online: " + webView,
		}
	}

	t.Run("tracking on, scoped send", func(t *testing.T) {
		s := &Service{Tracking: coretracking.NewSigner("https://mail.example.com", "secret")}
		e := body()
		s.resolveSystemLinks(e, "https://mail.example.com/tracking/unsubscribe/tok")

		for _, got := range []string{e.Subject, e.HTMLBody, e.TextBody} {
			if strings.Contains(got, "__mailyard_") {
				t.Errorf("placeholder left behind: %s", got)
			}
		}

		if !strings.Contains(e.HTMLBody, "/tracking/unsubscribe/tok") {
			t.Errorf("unsubscribe link not substituted: %s", e.HTMLBody)
		}

		// Bound to THIS message, which is the whole reason the pass waits
		// for the id.
		if !strings.Contains(e.Subject, "/tracking/view/") {
			t.Errorf("web view link not substituted: %s", e.Subject)
		}
	})

	// The common transactional case: no opt-out scope, so there is no
	// correct unsubscribe link. The placeholder must go, not ship.
	t.Run("tracking on, unscoped send", func(t *testing.T) {
		s := &Service{Tracking: coretracking.NewSigner("https://mail.example.com", "secret")}
		e := body()
		s.resolveSystemLinks(e, "")

		if strings.Contains(e.HTMLBody, "__mailyard_") {
			t.Errorf("placeholder shipped as an href: %s", e.HTMLBody)
		}

		if !strings.Contains(e.TextBody, "/tracking/view/") {
			t.Errorf("web view link should still resolve: %s", e.TextBody)
		}
	})

	// No public URL configured. Everything is stripped rather than left
	// pointing at a path that resolves against the reading client.
	t.Run("tracking off", func(t *testing.T) {
		s := &Service{}
		e := body()
		s.resolveSystemLinks(e, "")

		for _, got := range []string{e.Subject, e.HTMLBody, e.TextBody} {
			if strings.Contains(got, "__mailyard_") {
				t.Errorf("placeholder left behind with tracking off: %s", got)
			}
		}
	})

	// A campaign arrives here already substituted, and a message that
	// never used a reserved variable is the overwhelming majority. Both
	// must skip the pass, because minting the web view URL is an HMAC.
	t.Run("nothing to do", func(t *testing.T) {
		s := &Service{Tracking: coretracking.NewSigner("https://mail.example.com", "secret")}
		e := &emailmodel.Email{ID: "9e2f6f11-cdd3-4058-86f2-29f3ad60b06a",
			Subject: "hello", HTMLBody: "<p>hello</p>", TextBody: "hello"}
		s.resolveSystemLinks(e, "https://mail.example.com/tracking/unsubscribe/tok")

		if e.Subject != "hello" || e.HTMLBody != "<p>hello</p>" || e.TextBody != "hello" {
			t.Errorf("a message with no placeholders was rewritten: %+v", e)
		}
	})
}
