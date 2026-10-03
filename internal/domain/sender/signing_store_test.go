// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package sender

import (
	"strings"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/crypto"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	smodel "github.com/yousysadmin/mailyard/internal/models/sender"
)

// The private half is sealed on the way in, opened on the way out, and
// never rides on the sender row that every From picker reads.
func TestASigningKeyIsSealedAndListedWithoutItsMaterial(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	dbtest.Schema(t, db, `
        INSERT INTO projects (id, name, slug, default_language, created_at)
        VALUES ('e66e7a4d-9e6c-4884-869a-cf9ffcf22181', 'Test', 'test', 'en', now())`)
	cr := crypto.New("test-key-test-key-test-key-test-key-test")
	s := NewStore(db, cr)
	ctx := t.Context()

	m := &smodel.Sender{ID: ids.New(), ProjectID: "e66e7a4d-9e6c-4884-869a-cf9ffcf22181", Email: "billing@example.com", Name: "Billing"}
	if err := s.Put(ctx, m); err != nil {
		t.Fatal(err)
	}

	expires := time.Now().UTC().Add(10 * 24 * time.Hour).Truncate(time.Millisecond)
	if added, err := s.AddSigning(ctx, &smodel.SigningKey{
		SenderID: m.ID, ProjectID: m.ProjectID, Kind: smodel.SigningSMIME,
		PrivateKey:  "-----BEGIN PRIVATE KEY-----\nsecret\n-----END PRIVATE KEY-----\n",
		PublicKey:   "-----BEGIN CERTIFICATE-----\npublic\n-----END CERTIFICATE-----\n",
		Fingerprint: "ABCD", Algorithm: "RSA-2048", Subject: "CN=Billing", Issuer: "CN=CA",
		NotAfter: &expires, Sign: true,
	}); err != nil || !added {
		t.Fatalf("add: %v %v", added, err)
	}

	// A second key for the same sender is refused, never a replacement.
	if added, err := s.AddSigning(ctx, &smodel.SigningKey{
		SenderID: m.ID, ProjectID: m.ProjectID, Kind: smodel.SigningPGP,
		PrivateKey: "other", PublicKey: "other", Fingerprint: "EEEE", Sign: true,
	}); err != nil || added {
		t.Fatalf("second add: %v %v, want refused", added, err)
	}

	var sealed string
	if err := db.QueryRowContext(ctx, `SELECT private_key FROM sender_signing_keys`).Scan(&sealed); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(sealed, "secret") {
		t.Fatal("the private key is stored in the clear")
	}

	got, err := s.Get(ctx, m.ProjectID, m.ID)
	if err != nil || got == nil || got.Signing == nil {
		t.Fatalf("get: %v, %+v", err, got)
	}

	if got.Signing.PrivateKey != "" || got.Signing.PublicKey != "" {
		t.Error("the sender row carries key material")
	}

	if got.Signing.Kind != "smime" || got.Signing.Fingerprint != "ABCD" || !got.Signing.Sign || got.Signing.NotAfter == nil ||
		!got.Signing.NotAfter.Equal(expires) {
		t.Errorf("description %+v", got.Signing)
	}

	list, err := s.List(ctx, m.ProjectID)
	if err != nil || len(list) != 1 || list[0].Signing == nil {
		t.Fatalf("list: %v, %+v", err, list)
	}

	key, err := s.GetSigning(ctx, m.ProjectID, m.ID)
	if err != nil || key == nil {
		t.Fatalf("get signing: %v", err)
	}

	if !strings.Contains(key.PrivateKey, "secret") || !strings.Contains(key.PublicKey, "public") {
		t.Errorf("the material did not come back: %+v", key)
	}

	// Another project sees nothing of it.
	if other, err := s.GetSigning(ctx, ids.New(), m.ID); err != nil || other != nil {
		t.Errorf("another project read the key: %v, %+v", err, other)
	}

	if err := s.SetSigningFlags(ctx, m.ProjectID, m.ID, false, true); err != nil {
		t.Fatal(err)
	}

	got, _ = s.Get(ctx, m.ProjectID, m.ID)
	if got.Signing.Sign || !got.Signing.AttachKey {
		t.Errorf("flags %+v", got.Signing)
	}

	expiring, err := s.SigningExpiringBefore(ctx, time.Now().Add(30*24*time.Hour))
	if err != nil || len(expiring) != 1 || expiring[0].SenderEmail != "billing@example.com" {
		t.Errorf("expiring: %v, %+v", err, expiring)
	}

	if err := s.DeleteSigning(ctx, m.ProjectID, m.ID); err != nil {
		t.Fatal(err)
	}

	got, _ = s.Get(ctx, m.ProjectID, m.ID)
	if got.Signing != nil {
		t.Error("the key survived its deletion")
	}
}
