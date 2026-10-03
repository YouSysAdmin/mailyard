// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package env

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// load writes the YAML to a temp file, runs it through Load and then
// Validate - the two steps serve keeps apart - so a test exercises
// the same defaults an operator gets.
func load(t *testing.T, yaml string) (*Config, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "mailyard.yaml")
	if err := os.WriteFile(p, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	c, err := Load(p)
	if err != nil {
		return nil, err
	}

	return c, c.Validate()
}

const minimalConfig = `
database:
  dsn: postgres://u:p@localhost:5432/mailyard
  crypto:
    encryption_key: 0123456789abcdef0123456789abcdef
`

// The tracking signer keys on the encryption key, so auth disabled
// with a public_url needs no jwt_secret. A secret that is given still
// has to meet the floor.
func TestAuthDisabledWithAPublicURLNeedsNoJWTSecret(t *testing.T) {
	if _, err := load(t, minimalConfig+`
auth:
  disabled: true
server:
  public_url: https://mail.example.com
`); err != nil {
		t.Fatalf("public_url with auth disabled and no secret must load: %v", err)
	}

	if _, err := load(t, minimalConfig+"auth:\n  disabled: true\n"); err != nil {
		t.Fatalf("auth.disabled alone must load: %v", err)
	}

	_, err := load(t, minimalConfig+`
auth:
  disabled: true
  jwt_secret: short
server:
  public_url: https://mail.example.com
`)
	if err == nil || !strings.Contains(err.Error(), "auth.jwt_secret must be at least 32") {
		t.Fatalf("short secret: got %v, want the floor", err)
	}
}

// The 32-character floor applies to a secret whenever one is given,
// not only when auth is on: a short one is short whatever derives
// from it.
func TestAShortJWTSecretIsRefusedEvenWithAuthDisabled(t *testing.T) {
	_, err := load(t, minimalConfig+`
auth:
  disabled: true
  jwt_secret: short
`)
	if err == nil || !strings.Contains(err.Error(), "at least 32 characters") {
		t.Fatalf("got %v, want the length floor", err)
	}
}

// SSO-only is a documented setting and has to boot. The bootstrap
// email is required only while local sign-in is on.
func TestLocalSignInOffBoots(t *testing.T) {
	if _, err := load(t, minimalConfig+`
auth:
  jwt_secret: 0123456789abcdef0123456789abcdef
  local:
    enabled: false
`); err != nil {
		t.Fatalf("auth.local.enabled false must load: %v", err)
	}

	_, err := load(t, minimalConfig+`
auth:
  jwt_secret: 0123456789abcdef0123456789abcdef
  local:
    enabled: true
    email: ""
`)
	if err == nil || !strings.Contains(err.Error(), "auth.local.email required") {
		t.Fatalf("local on without an email: got %v", err)
	}
}

// The attachment cache defaults to 64 MiB, 0 turns it off, and a
// negative size is refused.
func TestTheAttachmentCacheSize(t *testing.T) {
	const auth = "auth:\n  jwt_secret: 0123456789abcdef0123456789abcdef\n"
	c, err := load(t, minimalConfig+auth)
	if err != nil {
		t.Fatal(err)
	}

	if c.Worker.AttachmentCacheBytes != 64<<20 {
		t.Fatalf("default %d, want 64 MiB", c.Worker.AttachmentCacheBytes)
	}

	if _, err := load(t, minimalConfig+auth+"worker:\n  attachment_cache_bytes: 0\n"); err != nil {
		t.Fatalf("0 must load: %v", err)
	}

	_, err = load(t, minimalConfig+auth+"worker:\n  attachment_cache_bytes: -1\n")
	if err == nil || !strings.Contains(err.Error(), "worker.attachment_cache_bytes") {
		t.Fatalf("negative: got %v, want it refused", err)
	}
}
