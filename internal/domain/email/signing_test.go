// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"context"
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/mailsign"
	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	smodel "github.com/yousysadmin/mailyard/internal/models/sender"
)

// fakeSenders holds one registered address and, optionally, its key.
type fakeSenders struct {
	store.SenderStore
	sender *smodel.Sender
	key    *smodel.SigningKey
}

func (f *fakeSenders) GetByEmail(_ context.Context, _ string, email string) (*smodel.Sender, error) {
	if f.sender != nil && f.sender.Email == email {
		return f.sender, nil
	}

	return nil, nil
}

func (f *fakeSenders) GetSigning(context.Context, string, string) (*smodel.SigningKey, error) {
	return f.key, nil
}

func pgpSender(t *testing.T, attach bool) *fakeSenders {
	t.Helper()
	m, err := mailsign.GeneratePGP("Billing", "billing@example.com")
	if err != nil {
		t.Fatal(err)
	}

	desc, _ := mailsign.Describe(m)
	key := &smodel.SigningKey{
		SenderID: "s1", ProjectID: "proj-a", Kind: smodel.SigningPGP,
		PrivateKey: m.Private, PublicKey: m.Public, Fingerprint: desc.Fingerprint,
		Sign: true, AttachKey: attach,
	}
	public := *key
	public.PrivateKey, public.PublicKey = "", ""

	return &fakeSenders{
		sender: &smodel.Sender{ID: "s1", ProjectID: "proj-a", Email: "billing@example.com", Signing: &public},
		key:    key,
	}
}

// The accept-time decision reads the sender's switch and the caller's
// opt-out, and nothing else.
func TestSigningIsDecidedBySenderSwitchAndCallerOptOut(t *testing.T) {
	on := pgpSender(t, true).sender
	off := pgpSender(t, true).sender
	off.Signing.Sign = false
	cases := []struct {
		name    string
		reg     *smodel.Sender
		disable bool
		want    string
	}{
		{"no registered sender", nil, false, ""},
		{"registered without a key", &smodel.Sender{Email: "x@example.com"}, false, ""},
		{"key, switched on", on, false, "pgp"},
		{"key, caller declines", on, true, ""},
		{"key, switched off", off, false, ""},
	}
	for _, c := range cases {
		if got := signingFor(c.reg, c.disable); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// signAs hands the builder the signer, and for PGP with attachment on,
// the Autocrypt key and the key file named the way Thunderbird names
// one. The result verifies - the builder test proves that part.
func TestSignAsAttachesTheSendersKey(t *testing.T) {
	st := &store.Store{Sender: pgpSender(t, true)}
	msg := &smtpclient.Message{From: "Billing <Billing@Example.com>", To: []string{"a@example.com"}, Subject: "s", Text: "t"}
	if err := signAs(t.Context(), st, "proj-a", msg.From, msg); err != nil {
		t.Fatal(err)
	}

	if msg.Signing == nil || msg.Signing.Signer == nil {
		t.Fatal("no signer attached")
	}

	if msg.Signing.AutocryptKey == "" || msg.Signing.PublicKey == nil {
		t.Fatal("attach_key on, but no Autocrypt key or key file")
	}

	if !strings.HasPrefix(msg.Signing.PublicKey.Filename, "OpenPGP_0x") || !strings.HasSuffix(msg.Signing.PublicKey.Filename, ".asc") ||
		msg.Signing.PublicKey.ContentType != "application/pgp-keys" {
		t.Errorf("key file %+v", msg.Signing.PublicKey)
	}

	if _, err := msg.Build(); err != nil {
		t.Fatalf("a signed build: %v", err)
	}

	// Attachment off: signed, nothing else.
	st = &store.Store{Sender: pgpSender(t, false)}
	msg = &smtpclient.Message{From: "billing@example.com", To: []string{"a@example.com"}, Subject: "s", Text: "t"}
	if err := signAs(t.Context(), st, "proj-a", msg.From, msg); err != nil {
		t.Fatal(err)
	}

	if msg.Signing.AutocryptKey != "" || msg.Signing.PublicKey != nil {
		t.Error("attach_key off, but the key travels")
	}
}

// A key removed after the message was accepted is a message sent
// unsigned, not a message stuck in the queue: the removal was asked
// for. A key that is there and will not open is an error.
func TestSignAsToleratesARemovedKeyAndNotABrokenOne(t *testing.T) {
	gone := pgpSender(t, true)
	gone.sender.Signing, gone.key = nil, nil
	msg := &smtpclient.Message{From: "billing@example.com", To: []string{"a@example.com"}, Subject: "s", Text: "t"}
	if err := signAs(t.Context(), &store.Store{Sender: gone}, "proj-a", msg.From, msg); err != nil {
		t.Fatalf("removed key: %v", err)
	}

	if msg.Signing != nil {
		t.Error("a removed key still signed")
	}

	broken := pgpSender(t, true)
	broken.key.PrivateKey = "-----BEGIN PGP PRIVATE KEY BLOCK-----\n\nnope\n-----END PGP PRIVATE KEY BLOCK-----\n"
	if err := signAs(t.Context(), &store.Store{Sender: broken}, "proj-a", msg.From, msg); err == nil {
		t.Error("a key that does not open was not an error")
	}
}
