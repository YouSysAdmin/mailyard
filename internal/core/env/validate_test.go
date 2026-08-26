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

// The tracking signer keys on auth.jwt_secret, so public_url without a
// secret is refused whether auth is on or off. auth.disabled on its own
// is a supported mode and loads.
func TestAuthDisabledWithAPublicURLNeedsAJWTSecret(t *testing.T) {
	_, err := load(t, minimalConfig+`
auth:
  disabled: true
server:
  public_url: https://mail.example.com
`)
	if err == nil || !strings.Contains(err.Error(), "auth.jwt_secret required when server.public_url") {
		t.Fatalf("public_url without a secret: got %v, want a refusal naming the secret", err)
	}

	if _, err := load(t, minimalConfig+"auth:\n  disabled: true\n"); err != nil {
		t.Fatalf("auth.disabled alone must load: %v", err)
	}

	if _, err := load(t, minimalConfig+`
auth:
  disabled: true
  jwt_secret: 0123456789abcdef0123456789abcdef
server:
  public_url: https://mail.example.com
`); err != nil {
		t.Fatalf("auth.disabled with a secret and a public_url must load: %v", err)
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
