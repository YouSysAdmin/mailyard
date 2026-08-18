// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The console holds the sender's images back until somebody asks for them,
// and this is what says so.
//
// Its sibling is TestThePreviewComponentStripsOurTrackingMarkup, which
// covers our markup - the open pixel and our wrapped links. Two tests
// because they are two rules with two reasons: ours must go because
// rendering it counts as a read of the project's own message, and the
// sender's must wait because fetching it tells THEM the message was
// opened. The stripping half is not repeated here.
//
// Why the policy cannot do this job: `img-src` is `* data: blob:` because
// an email's images ARE the email - a logo on a CDN has to render, and
// previews built on the fly (templates, campaigns) cannot be served from a
// route with a policy of their own. A srcdoc frame INHERITS the site
// policy and cannot narrow it per frame, verified in a browser in both
// directions. So the rewriting is the mechanism, not the header.
func TestThePreviewComponentHoldsBackTheSendersImages(t *testing.T) {
	vue := readPreviewComponent(t)

	// src alone is not enough: srcset is used in PREFERENCE to src, and a
	// CSS url() fetches exactly as an img does - blocking one of the three
	// blocks nothing in a real email.
	for _, what := range []struct{ needle, why string }{
		{"data-blocked-src", "an img src would load and tell the sender the message was opened"},
		{"data-blocked-srcset", "srcset is preferred over src, so blocking src alone blocks nothing"},
		{"url(", "a CSS background image fetches exactly as an img does"},
	} {
		if !strings.Contains(vue, what.needle) {
			t.Errorf("HtmlPreview.vue no longer neutralises %s - %s", what.needle, what.why)
		}
	}

	// data: and cid: stay. A data URI is bytes already in the message and
	// fetches nothing, and emails embed logos that way constantly - so
	// blocking them would break the common case to prevent a request that
	// never happens.
	if !strings.Contains(vue, "data:") || !strings.Contains(vue, "cid:") {
		t.Error("HtmlPreview.vue no longer exempts data: and cid:, so embedded images stop rendering")
	}

	// off is the default. A ref initialised to true would leave every
	// assertion above true and the feature wrong.
	if !regexp.MustCompile(`showImages\s*=\s*ref\(false\)`).MatchString(vue) {
		t.Error("showImages does not default to false - a preview must not fetch the sender's " +
			"images until somebody asks, which is what every mail client does")
	}
}

// The site policy has to PERMIT remote images, or the component is holding
// back what the browser would refuse anyway and the arrangement is theatre.
//
// `*` does not cover data: or blob: - they are separate scheme matches - so
// both are named. Emails embed logos as data URIs constantly, and leaving
// that out would break the case this directive was opened up for.
func TestTheConsolePolicyPermitsRemoteImages(t *testing.T) {
	csp := consoleCSP(t)

	if !strings.Contains(csp, "img-src") {
		t.Fatalf("no img-src in the console policy: %q", csp)
	}

	imgSrc := directive(csp, "img-src")

	if !strings.Contains(imgSrc, "*") {
		t.Errorf("img-src is %q - a preview cannot render a logo on a CDN, which is what "+
			"this directive was opened for", imgSrc)
	}

	for _, scheme := range []string{"data:", "blob:"} {
		if !strings.Contains(imgSrc, scheme) {
			t.Errorf("img-src is %q and omits %s - `*` does not cover it, and an embedded "+
				"image would stop rendering", imgSrc, scheme)
		}
	}

	// The directives that must not have been loosened with it.
	//
	// default-src is what keeps an injected script's fetches on our own
	// origin, and script-src is what stops one running at all - a wide
	// img-src is an acceptable trade only because those two stay tight.
	if !strings.Contains(csp, "default-src 'self'") {
		t.Errorf("default-src is no longer 'self': %q", csp)
	}

	if strings.Contains(directive(csp, "script-src"), "unsafe-inline") {
		t.Errorf("script-src carries unsafe-inline again: %q - the inline scripts the binary "+
			"serves are named by hash, computed at boot from the bytes themselves",
			directive(csp, "script-src"))
	}
}

// readPreviewComponent returns the console's preview component source.
func readPreviewComponent(t *testing.T) string {
	t.Helper()

	path := filepath.Join(repoRoot(t), "web", "src", "components", "HtmlPreview.vue")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read HtmlPreview.vue: %v", err)
	}

	return string(body)
}

// consoleCSP assembles the site policy out of securityHeaders.
//
// Read as text rather than by booting a Fiber app: the subject is what the
// source says, and standing a server up to read one header is a slower way
// to check the same string.
func consoleCSP(t *testing.T) string {
	t.Helper()

	path := filepath.Join(repoRoot(t), "internal", "server", "server.go")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read server.go: %v", err)
	}

	_, after, found := strings.Cut(string(body), `c.Set("Content-Security-Policy",`)
	if !found {
		t.Fatal("server.go no longer sets a Content-Security-Policy")
	}

	block, _, found := strings.Cut(after, ")\n")
	if !found {
		t.Fatal("could not find the end of the policy expression")
	}

	var out strings.Builder
	for _, m := range regexp.MustCompile(`"([^"]*)"`).FindAllStringSubmatch(block, -1) {
		out.WriteString(m[1])
	}

	if out.Len() == 0 {
		t.Fatal("read no policy text out of server.go")
	}

	return out.String()
}

// directive returns one directive's value out of a policy string.
func directive(csp, name string) string {
	for part := range strings.SplitSeq(csp, ";") {
		part = strings.TrimSpace(part)
		if rest, ok := strings.CutPrefix(part, name+" "); ok {
			return rest
		}
	}

	return ""
}
