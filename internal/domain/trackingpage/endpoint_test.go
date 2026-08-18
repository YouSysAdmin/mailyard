// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package trackingpage

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// The hosted "view in browser" page is the only response that returns
// tenant-authored HTML as a top-level document on the console's own
// origin. The site-wide policy permits script-src 'unsafe-inline' for
// the Vue bundle, so serving an email body under it would execute the
// author's script with the reader's session - a project editor
// mailing an admin a "view in browser" link would be a straight path
// to platform admin.
//
// These assertions are about the PROPERTIES that stop that, not the
// exact string, so the policy can be tuned without silently losing the
// part that matters.
func TestWebViewCSPCannotRunScriptOrReachOurOrigin(t *testing.T) {
	// sandbox with no allow-same-origin: the document lands in an
	// opaque origin, so its requests carry none of our cookies.
	if !strings.HasPrefix(webViewCSP, "sandbox;") {
		t.Errorf("policy must start with a bare sandbox directive, got %q", webViewCSP)
	}

	if strings.Contains(webViewCSP, "allow-same-origin") {
		t.Error("allow-same-origin puts the email back on our origin and undoes the sandbox")
	}

	if strings.Contains(webViewCSP, "allow-scripts") {
		t.Error("allow-scripts re-enables the exact thing this policy exists to stop")
	}

	// No script may execute: there is no script-src, so default-src
	// governs it, and it must be 'none'.
	if !strings.Contains(webViewCSP, "default-src 'none'") {
		t.Errorf("default-src must be 'none' so scripts have no source, got %q", webViewCSP)
	}

	if strings.Contains(webViewCSP, "script-src") {
		t.Error("policy must not carry a script-src at all - default-src 'none' is the rule")
	}

	// Rendering an email still has to work.
	if !strings.Contains(webViewCSP, "img-src") || !strings.Contains(webViewCSP, "style-src") {
		t.Error("images and inline styles are what an email is, they must stay allowed")
	}
}

// The header has to actually reach the response, replacing the
// site-wide policy the security middleware set earlier in the chain.
func TestWebViewCSPOverridesTheSitePolicy(t *testing.T) {
	app := fiber.New()

	// Stand in for internal/server.securityHeaders, which runs first
	// and sets the permissive console policy.
	app.Use(func(c *fiber.Ctx) error {
		c.Set(fiber.HeaderContentSecurityPolicy, "default-src 'self'; script-src 'self' 'unsafe-inline'")

		return c.Next()
	})
	app.Get("/view", func(c *fiber.Ctx) error {
		c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
		c.Set(fiber.HeaderContentSecurityPolicy, webViewCSP)

		return c.SendString("<script>alert(1)</script>")
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/view", nil))
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = resp.Body.Close() }()

	got := resp.Header.Get("Content-Security-Policy")
	if got != webViewCSP {
		t.Fatalf("response policy = %q, want the web-view policy - the site policy leaked through", got)
	}

	if strings.Contains(got, "unsafe-inline") && strings.Contains(got, "script") {
		t.Error("the permissive console script policy survived onto the email response")
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "<script>") {
		t.Error("the body should be served verbatim - the sandbox is what makes it safe, not filtering")
	}
}
