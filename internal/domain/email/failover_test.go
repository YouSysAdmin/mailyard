// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"net/textproto"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/notify"
	"github.com/yousysadmin/mailyard/internal/core/queue"
	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
	"github.com/yousysadmin/mailyard/internal/core/transport"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	dmodel "github.com/yousysadmin/mailyard/internal/models/domain"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
	nmodel "github.com/yousysadmin/mailyard/internal/models/notification"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

// scriptedSend answers per host so a test can say which server fails
// and how, and records the order they were tried in.
type scriptedSend struct {
	byHost map[string]error
	tried  []string
	signed []bool

	// envelopes records MAIL FROM per attempt, so a test can assert
	// the return path was recomputed for the server actually tried.
	envelopes []string
	providers []string
}

func (s *scriptedSend) fn(_ context.Context, spec transport.Spec, msg *smtpclient.Message) error {
	s.tried = append(s.tried, spec.Host)
	s.signed = append(s.signed, msg.Sign != nil)
	s.envelopes = append(s.envelopes, msg.EnvelopeFrom)

	// Recorded so a test can assert the provider traveled with the
	// candidate. Failing over from a SES row to an SMTP one must not
	// carry the provider, for the same reason it must not carry the
	// signature or the return path.
	s.providers = append(s.providers, spec.Provider)

	return s.byHost[spec.Host]
}

func transient() error {
	// Code 0 is a connection-level failure - no SMTP reply at all.
	return &smtpclient.SendError{Stage: "DIAL", Err: errors.New("connection refused")}
}

func permanent() error {
	return &smtpclient.SendError{
		Stage: "RCPT TO", Recipient: "d@x.com", Code: 550,
		Msg: "user unknown", Err: errors.New("550 user unknown"),
	}
}

func failoverProcessor(t *testing.T, servers []*ssmodel.Server, script *scriptedSend) *Processor {
	t.Helper()

	return &Processor{
		Store: &store.Store{
			SMTPServer: &fakeGroupServers{
				count:   len(servers),
				byGroup: map[string][]*ssmodel.Server{"g-default": servers},
			},
			SMTPGroup: &fakeRoutedGroups{byID: map[string]*ssmodel.Group{}},

			// The sender's domain has to be verified TO THIS PROJECT
			// or Process refuses before it reaches a server at all.
			// See RequireVerifiedSender.
			Domain: &fakeDomains{verified: &dmodel.Domain{
				ProjectID: "proj-a", Domain: "example.com", Verified: true,
			}},
			Project: &fakeProjects{},
			Bounce:  &fakeBounces{},
		},
		Log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		send: script.fn,
	}
}

func delivery() *emailmodel.Email {
	return &emailmodel.Email{
		ID: "9e2f6f11-cdd3-4058-86f2-29f3ad60b06a", ProjectID: "proj-a", Sender: "hi@example.com",
		Recipients: []string{"d@x.com"}, Subject: "s", TextBody: "b",
	}
}

func TestFailoverMovesToTheNextServerOnATransientFailure(t *testing.T) {
	script := &scriptedSend{byHost: map[string]error{"first": transient()}}
	p := failoverProcessor(t, []*ssmodel.Server{
		srv("first", func(s *ssmodel.Server) { s.Host = "first" }),
		srv("second", func(s *ssmodel.Server) { s.Host = "second" }),
	}, script)

	out := p.Process(t.Context(), delivery())
	if out.Kind != queue.KindDone {
		t.Fatalf("outcome %v, want done: %v", out.Kind, out.Err)
	}

	if len(script.tried) != 2 || script.tried[0] != "first" || script.tried[1] != "second" {
		t.Fatalf("tried %v, want [first second]", script.tried)
	}
}

// A 5xx is about the message, not the server. Trying the rest of the
// group would earn the same refusal from each and write a bounce row
// per server for what is one bounce.
func TestAPermanentRejectionStopsTheWalk(t *testing.T) {
	script := &scriptedSend{byHost: map[string]error{"first": permanent()}}
	p := failoverProcessor(t, []*ssmodel.Server{
		srv("first", func(s *ssmodel.Server) { s.Host = "first" }),
		srv("second", func(s *ssmodel.Server) { s.Host = "second" }),
	}, script)

	out := p.Process(t.Context(), delivery())
	if out.Kind != queue.KindFail {
		t.Fatalf("outcome %v, want fail", out.Kind)
	}

	if len(script.tried) != 1 {
		t.Fatalf("tried %v, want only the first server", script.tried)
	}
}

func TestEveryServerFailingTransientlyIsOneRetry(t *testing.T) {
	script := &scriptedSend{byHost: map[string]error{
		"first": transient(), "second": transient(),
	}}
	p := failoverProcessor(t, []*ssmodel.Server{
		srv("first", func(s *ssmodel.Server) { s.Host = "first" }),
		srv("second", func(s *ssmodel.Server) { s.Host = "second" }),
	}, script)

	out := p.Process(t.Context(), delivery())
	if out.Kind != queue.KindRetry {
		t.Fatalf("outcome %v, want retry", out.Kind)
	}

	if len(script.tried) != 2 {
		t.Fatalf("tried %v, want both before giving up", script.tried)
	}
}

// skip_dkim is per server, so the decision cannot be made once for
// the whole walk. Carrying a signature into an SES-style server that
// re-signs would deliver exactly the broken signature the flag exists
// to prevent.
func TestSignatureIsDecidedPerServerDuringFailover(t *testing.T) {
	script := &scriptedSend{byHost: map[string]error{"signing": transient()}}
	p := failoverProcessor(t, []*ssmodel.Server{
		srv("signing", func(s *ssmodel.Server) { s.Host = "signing" }),
		srv("resigner", func(s *ssmodel.Server) { s.Host = "resigner"; s.SkipDKIM = true }),
	}, script)

	if out := p.Process(t.Context(), delivery()); out.Kind != queue.KindDone {
		t.Fatalf("outcome %v, want done", out.Kind)
	}

	if len(script.signed) != 2 {
		t.Fatalf("recorded %d sends, want 2", len(script.signed))
	}

	if script.signed[1] {
		t.Error("the skip_dkim server received a signed message")
	}
}

// fakeNotes records what a Raiser filed.
type fakeNotes struct {
	store.NotificationStore
	filed []*nmodel.Notification
}

func (f *fakeNotes) Create(_ context.Context, n *nmodel.Notification) (bool, error) {
	f.filed = append(f.filed, n)

	return true, nil
}

// anyRelayClient lets a node candidate reach the send leg.
type anyRelayClient struct{}

func (anyRelayClient) WorkerTLS(context.Context, string) (*tls.Config, error) {
	return &tls.Config{}, nil
}

func refusedLogin() error {
	return &smtpclient.ConfigError{Err: &textproto.Error{Code: 535, Msg: "5.7.8 bad credentials"}}
}

// A refused login is the server's fault, not the message's: the walk
// goes on, and the server leaves rotation with one notification.
func TestARefusedLoginTakesTheServerOutOfRotation(t *testing.T) {
	script := &scriptedSend{byHost: map[string]error{"first": refusedLogin()}}
	p := failoverProcessor(t, []*ssmodel.Server{
		srv("first", func(s *ssmodel.Server) { s.Host = "first" }),
		srv("second", func(s *ssmodel.Server) { s.Host = "second" }),
	}, script)

	notes := &fakeNotes{}
	raiser := &notify.Raiser{Store: &store.Store{Notification: notes}}
	p.Notify = func() *notify.Raiser { return raiser }

	out := p.Process(t.Context(), delivery())
	if out.Kind != queue.KindDone {
		t.Fatalf("outcome %v, want done: %v", out.Kind, out.Err)
	}

	servers := p.Store.SMTPServer.(*fakeGroupServers)
	if len(servers.invalid) != 1 || servers.invalid[0] != "first" {
		t.Fatalf("marked invalid %v, want [first]", servers.invalid)
	}

	if len(notes.filed) != 1 || notes.filed[0].Type != nmodel.TypeSMTPInvalid {
		t.Fatalf("filed %v, want one smtp_invalid", notes.filed)
	}

	if notes.filed[0].ProjectID != "proj-a" {
		t.Errorf("filed for %q, want proj-a", notes.filed[0].ProjectID)
	}
}

// The shared pool is the platform's and a relay node is ours: neither
// is a project's to lose.
func TestAFaultOnASharedOrNodeServerMarksNothing(t *testing.T) {
	script := &scriptedSend{byHost: map[string]error{
		"pool": refusedLogin(), "node": refusedLogin(),
	}}
	p := failoverProcessor(t, []*ssmodel.Server{
		srv("pool", func(s *ssmodel.Server) { s.Host = "pool"; s.ProjectID = "" }),
		srv("node", func(s *ssmodel.Server) { s.Host = "node"; s.NodeID = "n1" }),
	}, script)
	p.RelayClient = anyRelayClient{}

	if out := p.Process(t.Context(), delivery()); out.Kind != queue.KindRetry {
		t.Fatalf("outcome %v, want retry", out.Kind)
	}

	if len(script.tried) != 2 {
		t.Fatalf("tried %v, want both to reach the send leg", script.tried)
	}

	if got := p.Store.SMTPServer.(*fakeGroupServers).invalid; len(got) != 0 {
		t.Fatalf("marked invalid %v, want none", got)
	}
}
