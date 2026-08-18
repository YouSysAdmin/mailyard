package tracking

import (
	"strings"
	"testing"
	"time"
)

func signer() *Signer { return NewSigner("https://mail.example.com/", "secret") }

func TestEnabled(t *testing.T) {
	if !signer().Enabled() {
		t.Fatal("signer with base url and key must be enabled")
	}

	if NewSigner("", "k").Enabled() || NewSigner("https://x", "").Enabled() {
		t.Fatal("missing base url or key must disable tracking")
	}
}

func TestOpenAndClickSignatures(t *testing.T) {
	s := signer()
	openURL := s.OpenURL("msg-1")
	if !strings.HasPrefix(openURL, "https://mail.example.com/tracking/open/msg-1.gif?sig=") {
		t.Fatalf("open url shape: %s", openURL)
	}

	sig := openURL[strings.Index(openURL, "sig=")+4:]
	if !s.VerifyOpen("msg-1", sig) {
		t.Error("open sig must verify")
	}

	if s.VerifyOpen("msg-2", sig) {
		t.Error("open sig must not verify for another message")
	}

	clickURL := s.ClickURL("msg-1", "abcd1234")
	csig := clickURL[strings.Index(clickURL, "sig=")+4:]
	if !s.VerifyClick("msg-1", "abcd1234", csig) {
		t.Error("click sig must verify")
	}

	if s.VerifyClick("msg-1", "ffff0000", csig) {
		t.Error("click sig must not verify for another hash")
	}
}

func TestUnsubscribeToken(t *testing.T) {
	s := signer()
	url := s.UnsubscribeURL("msg-9")
	tok := url[strings.LastIndex(url, "/")+1:]
	id, err := s.VerifyUnsubscribeToken(tok)
	if err != nil || id != "msg-9" {
		t.Fatalf("got %q err %v", id, err)
	}

	if _, err := s.VerifyUnsubscribeToken(tok + "x"); err == nil {
		t.Error("tampered token must fail")
	}

	if _, err := NewSigner("https://mail.example.com", "other").VerifyUnsubscribeToken(tok); err == nil {
		t.Error("wrong key must fail")
	}
}

func TestWebViewTokenExpiry(t *testing.T) {
	s := signer()
	url := s.WebViewURL("em-1")
	tok := url[strings.LastIndex(url, "/")+1:]
	id, err := s.VerifyWebViewToken(tok)
	if err != nil || id != "em-1" {
		t.Fatalf("got %q err %v", id, err)
	}

	// Hand-craft an expired token.
	expired := s.token("view:em-1:" + itoa(time.Now().Add(-time.Hour).Unix()))
	if _, err := s.VerifyWebViewToken(expired); err == nil {
		t.Error("expired token must fail")
	}

	// An unsubscribe token must not verify as a web view token.
	if _, err := s.VerifyWebViewToken(s.token("unsub:em-1")); err == nil {
		t.Error("wrong-kind token must fail")
	}
}

func itoa(n int64) string {
	var b []byte
	if n == 0 {
		return "0"
	}

	neg := n < 0
	if neg {
		n = -n
	}

	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}

	if neg {
		return "-" + string(b)
	}

	return string(b)
}

func TestProcessHTML(t *testing.T) {
	s := signer()
	html := `<html><body>
<a href="https://shop.example.com/product?id=1">Buy</a>
<a HREF='https://shop.example.com/product?id=1'>Buy again</a>
<a href="https://other.example.com/page">Other</a>
<a href="mailto:x@example.com">Mail</a>
<a href="/__mailyard_unsubscribe__">Unsubscribe</a>
<a href="https://mail.example.com/tracking/unsubscribe/tok123">Already tracked</a>
</body></html>`
	out, links := s.ProcessHTML(html, TrackOpts{
		EmailID: "msg-1", LinkScope: "camp-1", Opens: true, Clicks: true,
	})

	if len(links) != 2 {
		t.Fatalf("links = %d, want 2 unique (%+v)", len(links), links)
	}

	if strings.Contains(out, `href="https://shop.example.com`) {
		t.Error("external link not rewritten")
	}

	if !strings.Contains(out, "mailto:x@example.com") {
		t.Error("mailto must stay untouched")
	}

	if !strings.Contains(out, `/__mailyard_unsubscribe__`) {
		t.Error("sentinel must stay untouched")
	}

	if !strings.Contains(out, "https://mail.example.com/tracking/unsubscribe/tok123") {
		t.Error("own tracking link must stay untouched")
	}

	if !strings.Contains(out, "/tracking/open/msg-1.gif") || !strings.Contains(out, `</body>`) {
		t.Error("open pixel must be injected before body close")
	}

	if disabled, dl := NewSigner("", "").ProcessHTML(html, TrackOpts{
		EmailID: "m", LinkScope: "c", Opens: true, Clicks: true,
	}); disabled != html || dl != nil {
		t.Error("disabled signer must be a no-op")
	}
}

// Through the public surface, not the placeholder strings: what has to
// hold is that whatever WithSystemVars injects is exactly what
// SubstituteSystemLinks later resolves. Naming the strings would let the
// two drift apart and still pass.
func TestSystemVars(t *testing.T) {
	data := WithSystemVars(map[string]any{"name": "Ada", VarUnsubscribe: "shadow attempt"})

	if data["name"] != "Ada" {
		t.Error("user data lost")
	}

	unsub, _ := data[VarUnsubscribe].(string)
	webView, _ := data[VarWebView].(string)
	alias, _ := data[VarWebViewAlias].(string)

	if unsub == "" || webView == "" {
		t.Fatalf("system vars not injected: %v", data)
	}

	if unsub == "shadow attempt" {
		t.Error("user data must not shadow a reserved variable")
	}

	// A name published once has to keep working, so the alias resolves
	// to the same link rather than to one of its own.
	if alias != webView {
		t.Errorf("alias = %q, want the same placeholder as %q", alias, webView)
	}

	body := "Hi <a href=\"" + unsub + "\">bye</a> " + webView
	if !HasSystemSentinels(body) {
		t.Fatal("placeholders not detected")
	}

	out := SubstituteSystemLinks(body, Links{
		WebView:     "https://x/tracking/view/a",
		Unsubscribe: "https://x/tracking/unsubscribe/b",
	})

	if !strings.Contains(out, "https://x/tracking/unsubscribe/b") ||
		!strings.Contains(out, "https://x/tracking/view/a") {
		t.Errorf("substitution wrong: %s", out)
	}

	if HasSystemSentinels(out) || strings.Contains(out, "__mailyard_") {
		t.Errorf("placeholders left behind: %s", out)
	}
}

// What the campaign runner relies on when tracking is off: an empty URL
// REMOVES the placeholder. Left in, it would ship to a subscriber as an
// href of /__mailyard_unsubscribe__, which resolves against the reading
// client and goes nowhere.
func TestSystemLinksWithNoURLsAreRemoved(t *testing.T) {
	data := WithSystemVars(nil)
	body := "a " + data[VarWebView].(string) + " b " + data[VarUnsubscribe].(string) + " c"

	out := SubstituteSystemLinks(body, Links{})

	if out != "a  b  c" {
		t.Errorf("out = %q, want the placeholders gone", out)
	}

	if HasSystemSentinels(out) {
		t.Error("placeholders still present")
	}
}

// Opens and clicks are independently switchable, because senders
// object to them for different reasons: a pixel is a privacy question,
// a rewritten link changes what the recipient sees in the status bar.
func TestProcessHTMLHonoursEachSwitch(t *testing.T) {
	s := NewSigner("https://mail.example.com", "secret")
	html := `<html><body><a href="https://example.com/x">x</a></body></html>`

	cases := []struct {
		name              string
		opts              TrackOpts
		wantPixel, wantCT bool
	}{
		{"both", TrackOpts{EmailID: "9e2f6f11-cdd3-4058-86f2-29f3ad60b06a", LinkScope: "e66e7a4d-9e6c-4884-869a-cf9ffcf22181", Opens: true, Clicks: true}, true, true},
		{"opens only", TrackOpts{EmailID: "9e2f6f11-cdd3-4058-86f2-29f3ad60b06a", LinkScope: "e66e7a4d-9e6c-4884-869a-cf9ffcf22181", Opens: true}, true, false},
		{"clicks only", TrackOpts{EmailID: "9e2f6f11-cdd3-4058-86f2-29f3ad60b06a", LinkScope: "e66e7a4d-9e6c-4884-869a-cf9ffcf22181", Clicks: true}, false, true},
		{"neither", TrackOpts{EmailID: "9e2f6f11-cdd3-4058-86f2-29f3ad60b06a", LinkScope: "e66e7a4d-9e6c-4884-869a-cf9ffcf22181"}, false, false},
		// No email id means nothing to attribute a hit to, so the body
		// must come back untouched rather than carrying a dead pixel.
		{"no email id", TrackOpts{LinkScope: "e66e7a4d-9e6c-4884-869a-cf9ffcf22181", Opens: true, Clicks: true}, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, _ := s.ProcessHTML(html, c.opts)
			if got := strings.Contains(out, "/tracking/open/"); got != c.wantPixel {
				t.Errorf("pixel present = %v, want %v", got, c.wantPixel)
			}

			if got := strings.Contains(out, "/tracking/click/"); got != c.wantCT {
				t.Errorf("click redirect present = %v, want %v", got, c.wantCT)
			}
		})
	}
}

// The link hash is scoped, so the same URL in two campaigns is two
// tallies and the same URL across a project's transactional mail is
// one. Getting this wrong merges unrelated counts.
func TestLinkHashIsScoped(t *testing.T) {
	url := "https://example.com/deal"
	if HashLink("camp-1", url) == HashLink("camp-2", url) {
		t.Error("the same URL in two campaigns hashed alike, so their clicks would merge")
	}

	// Both sides are deliberately the same call: what is being asserted is
	// that the hash is DETERMINISTIC, so a salt or a map iteration
	// creeping in fails here.
	//nolint:staticcheck // SA4000: comparing identical calls is the point
	if HashLink("proj-1", url) != HashLink("proj-1", url) {
		t.Error("the same URL in one scope hashed differently")
	}
}
