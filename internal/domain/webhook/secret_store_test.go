// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package webhook

import (
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	whmodel "github.com/yousysadmin/mailyard/internal/models/webhook"
)

// The signing secret is SEALED in the column and plaintext to callers.
//
// It used to be written verbatim, so a database dump handed over every
// project's HMAC key - and the wire type's comment claimed a hash was
// stored, which is impossible for a value the dispatcher has to sign
// with. This checks both halves at once: what comes back out is usable,
// and what sits in the column is not.
func TestTheSigningSecretIsSealedAtRest(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := NewStore(db, crypto.New("0123456789abcdef0123456789abcdef"))
	ctx := t.Context()

	proj := ids.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, owner_id, created_at)
		VALUES ($1, 'acme', $2, NULL, now())`, proj, proj); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	const secret = "whsec_a_very_recognisable_signing_key"
	h := &whmodel.Webhook{
		ID:        ids.New(),
		ProjectID: proj,
		URL:       "https://example.test/hook",
		Events:    []string{"email.sent"},
		Secret:    secret,
	}
	if err := s.Put(ctx, h); err != nil {
		t.Fatalf("put webhook: %v", err)
	}

	// Out of the store: the value the receiver was handed, or every
	// signature we produce is unverifiable.
	got, err := s.Get(ctx, proj, h.ID)
	if err != nil {
		t.Fatalf("get webhook: %v", err)
	}

	if got == nil {
		t.Fatal("the webhook did not come back")
	}

	if got.Secret != secret {
		t.Errorf("secret came back as %q, want the plaintext the caller stored", got.Secret)
	}

	// In the column: anything but the plaintext.
	var stored string
	if err := db.QueryRowContext(ctx,
		`SELECT secret FROM webhooks WHERE id = $1`, h.ID).Scan(&stored); err != nil {
		t.Fatalf("read the column: %v", err)
	}

	if stored == secret {
		t.Error("the column holds the signing secret in the clear")
	}

	if strings.Contains(stored, "recognisable") {
		t.Errorf("the column still contains the plaintext: %q", stored)
	}
}

// An edit must not rotate the secret. The receiver is already verifying
// with it, so changing a url cannot silently invalidate every signature.
func TestEditingAWebhookKeepsItsSecret(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := NewStore(db, crypto.New("0123456789abcdef0123456789abcdef"))
	ctx := t.Context()

	proj := ids.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, owner_id, created_at)
		VALUES ($1, 'acme', $2, NULL, now())`, proj, proj); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	const secret = "whsec_original"
	h := &whmodel.Webhook{
		ID: ids.New(), ProjectID: proj, URL: "https://a.test/hook",
		Events: []string{"email.sent"}, Secret: secret,
	}
	if err := s.Put(ctx, h); err != nil {
		t.Fatalf("put: %v", err)
	}

	// The console's edit path: same id, new url, a secret it never read.
	h.URL = "https://b.test/hook"
	h.Secret = "whsec_something_else"
	if err := s.Put(ctx, h); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := s.Get(ctx, proj, h.ID)
	if err != nil || got == nil {
		t.Fatalf("get: %v", err)
	}

	if got.URL != "https://b.test/hook" {
		t.Errorf("url = %q, want the edit applied", got.URL)
	}

	if got.Secret != secret {
		t.Errorf("secret = %q, want the original - an edit rotated the signing key", got.Secret)
	}
}
