// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package submission

import (
	"bufio"
	"context"
	"encoding/base64"
	"log/slog"
	"net"
	"net/smtp"
	"strings"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/domain/email"
	akmodel "github.com/yousysadmin/mailyard/internal/models/apikey"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
	scmodel "github.com/yousysadmin/mailyard/internal/models/smtpcredential"

	"github.com/yousysadmin/mailyard/internal/core/iplimit"
)

// fakeKeys implements store.APIKeyStore over a single in-memory key.
type fakeKeys struct {
	key *akmodel.Key
}

func (f *fakeKeys) Get(context.Context, string, string) (*akmodel.Key, error) { return nil, nil }
func (f *fakeKeys) GetByPrefix(_ context.Context, prefix string) (*akmodel.Key, error) {
	if f.key != nil && f.key.KeyPrefix == prefix {
		return f.key, nil
	}

	return nil, nil
}
func (f *fakeKeys) List(context.Context, string) ([]*akmodel.Key, error)   { return nil, nil }
func (f *fakeKeys) Put(context.Context, *akmodel.Key) error                { return nil }
func (f *fakeKeys) Revoke(context.Context, string, string) error           { return nil }
func (f *fakeKeys) Delete(context.Context, string, string) error           { return nil }
func (f *fakeKeys) TouchLastUsed(context.Context, string, time.Time) error { return nil }
func (f *fakeKeys) Count(context.Context, string) (int, error)             { return 0, nil }

// fakeCreds implements store.SMTPCredentialStore over a single
// in-memory credential.
type fakeCreds struct {
	cred *scmodel.Credential
}

func (f *fakeCreds) Get(context.Context, string, string) (*scmodel.Credential, error) {
	return nil, nil
}
func (f *fakeCreds) GetByUsername(_ context.Context, username string) (*scmodel.Credential, error) {
	if f.cred != nil && f.cred.Username == username {
		return f.cred, nil
	}

	return nil, nil
}
func (f *fakeCreds) List(context.Context, string) ([]*scmodel.Credential, error) { return nil, nil }
func (f *fakeCreds) Put(context.Context, *scmodel.Credential) error              { return nil }
func (f *fakeCreds) Revoke(context.Context, string, string) error                { return nil }
func (f *fakeCreds) Delete(context.Context, string, string) error                { return nil }
func (f *fakeCreds) TouchLastUsed(context.Context, string, time.Time) error      { return nil }
func (f *fakeCreds) Count(context.Context, string) (int, error)                  { return 0, nil }

// fakeSender records the request and returns a canned outcome.
type fakeSender struct {
	lastWS  string
	lastKey string
	lastReq *email.SendRequest
	fail    error
}

func (f *fakeSender) Send(_ context.Context, projID, _, apiKeyID string, req *email.SendRequest) (*emailmodel.Email, []string, error) {
	f.lastWS = projID
	f.lastKey = apiKeyID
	f.lastReq = req
	if f.fail != nil {
		return nil, nil, f.fail
	}

	return &emailmodel.Email{ID: "em-1", ProjectID: projID, Recipients: req.To}, nil, nil
}

// canSend is the permission list a submission credential needs.
//
// Spelled out in every test that expects a successful AUTH, because
// an empty list used to mean send and now means nothing - a test
// passing nil here would fail at AUTH rather than where it looks.
var canSend = []string{"emails:write"}

// startServer boots the listener on a random loopback port and returns
// its address plus the fakes for assertions.
func startServer(t *testing.T, sender *fakeSender, perms []string, revoked bool) (string, string) {
	t.Helper()
	token, prefix, hash, err := akmodel.Generate()
	if err != nil {
		t.Fatal(err)
	}

	keys := &fakeKeys{key: &akmodel.Key{
		ID: "eea04bb8-feb6-49e8-8e3d-69758376292a", ProjectID: "proj-1", CreatedBy: "user-1",
		KeyPrefix: prefix, KeyHash: hash, Permissions: perms, Revoked: revoked,
	}}
	b := &Backend{
		Keys:           keys,
		Sender:         sender,
		Log:            slog.New(slog.DiscardHandler),
		MaxMessageSize: 1 << 20,
	}
	srv := NewServer(b, "127.0.0.1:0", "test", nil)
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		t.Fatal(err)
	}

	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	return ln.Addr().String(), token
}

func submit(addr, password, from string, to []string, msg string) error {
	return submitAs(addr, "mailyard", password, from, to, msg)
}

func submitAs(addr, username, password, from string, to []string, msg string) error {
	c, err := smtp.Dial(addr)
	if err != nil {
		return err
	}

	defer func() { _ = c.Close() }()
	if err := c.Auth(smtp.PlainAuth("", username, password, "127.0.0.1")); err != nil {
		return err
	}

	if err := c.Mail(from); err != nil {
		return err
	}

	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return err
		}
	}

	w, err := c.Data()
	if err != nil {
		return err
	}

	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}

	if err := w.Close(); err != nil {
		return err
	}

	return c.Quit()
}

const testMsg = "From: Ann <ann@example.com>\r\n" +
	"To: bob@example.com\r\n" +
	"Subject: submission test\r\n" +
	"Content-Type: text/plain; charset=UTF-8\r\n" +
	"\r\n" +
	"hello over smtp\r\n"

func TestRelayAcceptsValidKey(t *testing.T) {
	sender := &fakeSender{}
	addr, token := startServer(t, sender, canSend, false)

	err := submit(addr, token, "ann@example.com", []string{"bob@example.com", "carol@example.com"}, testMsg)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	if sender.lastWS != "proj-1" || sender.lastKey != "eea04bb8-feb6-49e8-8e3d-69758376292a" {
		t.Errorf("send attribution = proj %q key %q", sender.lastWS, sender.lastKey)
	}

	req := sender.lastReq
	if req == nil {
		t.Fatal("sender never called")
	}

	// Envelope wins over headers: two RCPT TO but one To header.
	if req.From != "ann@example.com" || len(req.To) != 2 {
		t.Errorf("envelope = from %q to %v", req.From, req.To)
	}

	if req.Subject != "submission test" || !strings.Contains(req.Text, "hello over smtp") {
		t.Errorf("parsed subject %q text %q", req.Subject, req.Text)
	}
}

func TestRelayRejectsBadCredentials(t *testing.T) {
	sender := &fakeSender{}
	addr, token := startServer(t, sender, canSend, false)

	cases := map[string]string{
		"wrong token":      akmodel.Prefix + strings.Repeat("x", 43),
		"not a key":        "hunter2",
		"truncated prefix": token[:8],
	}
	for name, pw := range cases {
		if err := submit(addr, pw, "a@x.com", []string{"b@x.com"}, testMsg); err == nil {
			t.Errorf("%s: auth must fail", name)
		}
	}

	if sender.lastReq != nil {
		t.Error("sender must never be reached without auth")
	}
}

func TestRelayRejectsRevokedKey(t *testing.T) {
	sender := &fakeSender{}
	addr, token := startServer(t, sender, canSend, true)
	if err := submit(addr, token, "a@x.com", []string{"b@x.com"}, testMsg); err == nil {
		t.Error("revoked key must fail auth")
	}
}

func TestRelayRejectsKeyWithoutSendPermission(t *testing.T) {
	sender := &fakeSender{}
	addr, token := startServer(t, sender, []string{"emails:read"}, false)
	if err := submit(addr, token, "a@x.com", []string{"b@x.com"}, testMsg); err == nil {
		t.Error("read-only key must fail auth")
	}
}

func TestRelayMapsRequestErrorToPermanentFailure(t *testing.T) {
	sender := &fakeSender{fail: email.NewRequestError("subject is required")}
	addr, token := startServer(t, sender, canSend, false)
	err := submit(addr, token, "a@x.com", []string{"b@x.com"}, testMsg)
	if err == nil {
		t.Fatal("request error must surface to the client")
	}

	if !strings.Contains(err.Error(), "550") {
		t.Errorf("want a 550 permanent reply, got %v", err)
	}
}

func TestRelayRejectsUnauthenticatedMail(t *testing.T) {
	sender := &fakeSender{}
	addr, _ := startServer(t, sender, canSend, false)
	c, err := smtp.Dial(addr)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = c.Close() }()
	if err := c.Mail("a@x.com"); err == nil {
		t.Error("MAIL FROM without AUTH must fail")
	}
}

// startCredServer boots the listener with one SMTP submission credential and
// returns its address plus the plaintext password.
func startCredServer(t *testing.T, sender *fakeSender, revoked bool, allowedIPs []string) (string, string, string) {
	t.Helper()
	username, password, hash, err := scmodel.Generate()
	if err != nil {
		t.Fatal(err)
	}

	b := &Backend{
		Credentials: &fakeCreds{cred: &scmodel.Credential{
			ID: "cred-1", ProjectID: "proj-2", CreatedBy: "user-2",
			Username: username, PasswordHash: hash,
			AllowedIPs: allowedIPs, Revoked: revoked,
		}},
		Keys:           &fakeKeys{},
		Sender:         sender,
		Log:            slog.New(slog.DiscardHandler),
		MaxMessageSize: 1 << 20,
	}
	srv := NewServer(b, "127.0.0.1:0", "test", nil)
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		t.Fatal(err)
	}

	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	return ln.Addr().String(), username, password
}

func TestRelayAcceptsSMTPCredential(t *testing.T) {
	sender := &fakeSender{}
	addr, username, password := startCredServer(t, sender, false, nil)

	if err := submitAs(addr, username, password, "ann@example.com", []string{"bob@example.com"}, testMsg); err != nil {
		t.Fatalf("submit: %v", err)
	}

	if sender.lastWS != "proj-2" {
		t.Errorf("project = %q, want proj-2", sender.lastWS)
	}

	// Credential logins carry no api key, so the email row must not
	// claim one.
	if sender.lastKey != "" {
		t.Errorf("api key id = %q, want empty for a credential login", sender.lastKey)
	}
}

func TestRelayRejectsBadSMTPCredential(t *testing.T) {
	sender := &fakeSender{}
	addr, username, password := startCredServer(t, sender, false, nil)

	cases := map[string][2]string{
		"wrong password":   {username, strings.Repeat("0", 64)},
		"unknown username": {scmodel.UsernamePrefix + "deadbeefdeadbeef", password},
	}
	for name, pair := range cases {
		if err := submitAs(addr, pair[0], pair[1], "a@x.com", []string{"b@x.com"}, testMsg); err == nil {
			t.Errorf("%s: auth must fail", name)
		}
	}

	if sender.lastReq != nil {
		t.Error("sender must never be reached without auth")
	}
}

func TestRelayRejectsRevokedSMTPCredential(t *testing.T) {
	sender := &fakeSender{}
	addr, username, password := startCredServer(t, sender, true, nil)
	if err := submitAs(addr, username, password, "a@x.com", []string{"b@x.com"}, testMsg); err == nil {
		t.Error("revoked credential must fail auth")
	}
}

func TestRelayEnforcesCredentialIPAllowlist(t *testing.T) {
	sender := &fakeSender{}
	addr, username, password := startCredServer(t, sender, false, []string{"203.0.113.0/24"})
	if err := submitAs(addr, username, password, "a@x.com", []string{"b@x.com"}, testMsg); err == nil {
		t.Error("loopback caller must fail an allowlist that excludes it")
	}
}

// countingKeys wraps the key store to count how many lookups the
// brute-force guard actually lets through to the database.
type countingKeys struct {
	fakeKeys
	lookups int
}

func (c *countingKeys) GetByPrefix(ctx context.Context, prefix string) (*akmodel.Key, error) {
	c.lookups++

	return c.fakeKeys.GetByPrefix(ctx, prefix)
}

// A failed AUTH leaves the connection open (go-smtp answers 454 and
// carries on), and the per-IP limiter is charged once per CONNECTION
// in NewSession. Without a per-connection cap that combination buys
// unlimited credential guesses for the price of one rate-limit token,
// so this pins both halves of the guard: attempts stop being served,
// and each failure spends IP budget.
func TestAuthBruteForceIsCapped(t *testing.T) {
	keys := &countingKeys{}
	b := &Backend{
		Keys:           keys,
		Sender:         &fakeSender{},
		Log:            slog.New(slog.DiscardHandler),
		MaxMessageSize: 1 << 20,

		// Generous budget so the CONNECTION cap is what this test
		// observes, not the limiter.
		Limiter: iplimit.New(1000, time.Minute),
	}
	srv := NewServer(b, "127.0.0.1:0", "test", nil)
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		t.Fatal(err)
	}

	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	// Raw protocol on purpose. Go's net/smtp client gives up after one
	// rejected AUTH, so driving this through it would test the CLIENT
	// and prove nothing about the server - which is what an attacker
	// speaks to directly.
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	br := bufio.NewReader(conn)
	readReply := func() string {
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return ""
			}

			// Skip continuation lines of a multiline greeting/EHLO.
			if len(line) >= 4 && line[3] == '-' {
				continue
			}

			return line
		}
	}
	readReply() // greeting
	_, _ = conn.Write([]byte("EHLO attacker\r\n"))
	readReply()

	const guesses = 20
	accepted := 0
	for range guesses {
		// A well-formed but wrong api key: each guess would reach
		// GetByPrefix if nothing stopped it.
		cred := base64.StdEncoding.EncodeToString(
			[]byte("\x00mailyard\x00" + akmodel.Prefix + "wrongkey"))
		_, _ = conn.Write([]byte("AUTH PLAIN " + cred + "\r\n"))
		reply := readReply()
		if strings.HasPrefix(reply, "235") {
			accepted++
		}
	}

	if accepted > 0 {
		t.Fatalf("%d bogus keys authenticated", accepted)
	}

	if keys.lookups > maxAuthFailures {
		t.Errorf("database consulted %d times for %d guesses, want at most %d",
			keys.lookups, guesses, maxAuthFailures)
	}

	// Each failure spends IP budget, so a limiter sized to the cap
	// refuses the next connection outright.
	tight := iplimit.New(2, time.Minute)
	for range 2 {
		tight.Allow("10.0.0.1")
	}

	if tight.Allow("10.0.0.1") {
		t.Error("limiter did not refuse once its budget was spent")
	}
}

// A control header is an instruction to Mailyard, not part of the
// message. It has to be read AND removed, so it cannot travel onward
// to the recipient's server if headers ever start being forwarded.
func TestTakeControlHeaderReadsAndRemoves(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string]string
		want    bool
	}{
		{"absent", map[string]string{"Subject": "hi"}, false},
		{"bare", map[string]string{"X-Mailyard-Disable-Tracking": ""}, true},
		{"true", map[string]string{"X-Mailyard-Disable-Tracking": "true"}, true},
		{"1", map[string]string{"X-Mailyard-Disable-Tracking": "1"}, true},
		{"explicit no", map[string]string{"X-Mailyard-Disable-Tracking": "false"}, false},
		{"off", map[string]string{"X-Mailyard-Disable-Tracking": "off"}, false},
		// Header names are case insensitive on the wire, so a client
		// that lowercases everything must still be understood.
		{"lowercased", map[string]string{"x-mailyard-disable-tracking": "yes"}, true},
		{"nil map", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := takeControlHeader(c.headers, headerDisableTracking)
			if got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}

			for k := range c.headers {
				if strings.EqualFold(k, headerDisableTracking) {
					t.Errorf("%q survived, so it could reach the outbound server", k)
				}
			}
		})
	}
}
