// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package submission

import (
	"context"
	"log/slog"
	"net"
	"net/smtp"
	"sync/atomic"
	"testing"

	akmodel "github.com/yousysadmin/mailyard/internal/models/apikey"
	scmodel "github.com/yousysadmin/mailyard/internal/models/smtpcredential"
)

// revocableKeys answers the stored key as revoked once the test says so.
type revocableKeys struct {
	fakeKeys
	revoked atomic.Bool
}

func (r *revocableKeys) Get(ctx context.Context, projID, id string) (*akmodel.Key, error) {
	k, err := r.fakeKeys.Get(ctx, projID, id)
	if k != nil && r.revoked.Load() {
		c := *k
		c.Revoked = true

		return &c, err
	}

	return k, err
}

// revocableCreds answers the stored credential as revoked once the
// test says so.
type revocableCreds struct {
	fakeCreds
	revoked atomic.Bool
}

func (r *revocableCreds) Get(ctx context.Context, projID, id string) (*scmodel.Credential, error) {
	c, err := r.fakeCreds.Get(ctx, projID, id)
	if c != nil && r.revoked.Load() {
		cp := *c
		cp.Revoked = true

		return &cp, err
	}

	return c, err
}

// serve starts the listener over b and returns its address.
func serve(t *testing.T, b *Backend) string {
	t.Helper()

	srv := NewServer(b, "127.0.0.1:0", "test", nil)
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		t.Fatal(err)
	}

	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	return ln.Addr().String()
}

// assertRevocationEndsSession authenticates, starts and abandons one
// transaction, revokes, and expects the next MAIL to be refused.
func assertRevocationEndsSession(t *testing.T, addr, username, password string, revoke func(), sender *fakeSender) {
	t.Helper()

	c, err := smtp.Dial(addr)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = c.Close() }()

	if err := c.Auth(smtp.PlainAuth("", username, password, "127.0.0.1")); err != nil {
		t.Fatalf("auth: %v", err)
	}

	if err := c.Mail("ann@example.com"); err != nil {
		t.Fatalf("mail before revoke: %v", err)
	}

	if err := c.Reset(); err != nil {
		t.Fatalf("rset: %v", err)
	}

	revoke()

	err = c.Mail("ann@example.com")
	if code := smtpCode(err); code != 535 {
		t.Fatalf("mail after revoke: %v (code %d), want 535", err, code)
	}

	// The session is no longer authenticated, so a retry is refused too.
	if code := smtpCode(c.Mail("ann@example.com")); code < 500 {
		t.Errorf("second mail after revoke: code %d, want a 5xx", code)
	}

	_ = c.Quit()

	if sender.lastReq != nil {
		t.Error("a revoked credential reached Send on a held connection")
	}
}

// Revoking an api key stops a connection that authenticated with it
// before the revoke.
func TestARevokedKeyStopsAHeldSession(t *testing.T) {
	token, prefix, hash, err := akmodel.Generate()
	if err != nil {
		t.Fatal(err)
	}

	keys := &revocableKeys{fakeKeys: fakeKeys{key: &akmodel.Key{
		ID: "eea04bb8-feb6-49e8-8e3d-69758376292a", ProjectID: "proj-1", CreatedBy: "user-1",
		KeyPrefix: prefix, KeyHash: hash, Permissions: canSend,
	}}}
	sender := &fakeSender{}
	addr := serve(t, &Backend{
		Keys:           keys,
		Sender:         sender,
		Projects:       &fakeProjects{},
		Log:            slog.New(slog.DiscardHandler),
		MaxMessageSize: 1 << 20,
	})

	assertRevocationEndsSession(t, addr, "mailyard", token, func() { keys.revoked.Store(true) }, sender)
}

// Revoking a submission credential stops a connection that
// authenticated with it before the revoke.
func TestARevokedCredentialStopsAHeldSession(t *testing.T) {
	username, password, hash, err := scmodel.Generate()
	if err != nil {
		t.Fatal(err)
	}

	creds := &revocableCreds{fakeCreds: fakeCreds{cred: &scmodel.Credential{
		ID: "cred-1", ProjectID: "proj-2", CreatedBy: "user-2",
		Username: username, PasswordHash: hash,
	}}}
	sender := &fakeSender{}
	addr := serve(t, &Backend{
		Credentials:    creds,
		Keys:           &fakeKeys{},
		Sender:         sender,
		Projects:       &fakeProjects{},
		Log:            slog.New(slog.DiscardHandler),
		MaxMessageSize: 1 << 20,
	})

	assertRevocationEndsSession(t, addr, username, password, func() { creds.revoked.Store(true) }, sender)
}
