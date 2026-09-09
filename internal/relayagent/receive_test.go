// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relayagent

import (
	"errors"
	"log/slog"
	"net"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"
)

func receivedFixture(id string) *Received {
	now := time.Now().UTC()

	return &Received{
		ID: id, EnvelopeFrom: "mailer-daemon@far.test",
		Recipients: []string{"bounces@in.example.com"},
		ClientIP:   "198.51.100.7", HELO: "mx.far.test",
		ReceivedAt: now, NextAttempt: now,
	}
}

// The node answers 250 and the sending MTA walks away, so from that
// moment the spool holds the only copy. A restart that forgot it
// would lose a bounce nobody can ask for again.
func TestReceivedMailSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenSpool(dir)
	if err != nil {
		t.Fatalf("OpenSpool: %v", err)
	}
	m := receivedFixture(ids.New())
	if err := s.PutReceived(m, []byte("Subject: bounce\r\n\r\nbody\r\n")); err != nil {
		t.Fatalf("PutReceived: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	again, err := OpenSpool(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = again.Close() }()

	due, err := again.ReceivedDue(time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("ReceivedDue: %v", err)
	}

	if len(due) != 1 || due[0].ID != m.ID {
		t.Fatalf("after restart got %d entries, want the one we accepted", len(due))
	}

	if due[0].ClientIP != "198.51.100.7" || due[0].HELO != "mx.far.test" {
		t.Errorf("the peer details did not survive: ip %q helo %q", due[0].ClientIP, due[0].HELO)
	}
	body, err := again.ReceivedBody(m.ID)
	if err != nil || !strings.Contains(string(body), "bounce") {
		t.Errorf("body did not survive: %q %v", body, err)
	}
}

// The two queues have separate directories precisely so the sweep can
// be told against which bucket each is an orphan. One directory for
// both would make every received message look like an orphan of the
// outbound queue - and the sweep would delete mail this node had
// already told a stranger's MTA it accepted.
func TestTheOrphanSweepDoesNotEatReceivedMail(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenSpool(dir)
	if err != nil {
		t.Fatalf("OpenSpool: %v", err)
	}
	defer func() { _ = s.Close() }()

	out := queued(ids.New())
	if err := s.Put(out, []byte(rawBody)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	in := receivedFixture(ids.New())
	if err := s.PutReceived(in, []byte("Subject: kept\r\n\r\nbody\r\n")); err != nil {
		t.Fatalf("PutReceived: %v", err)
	}
	orphan := filepath.Join(dir, "received", ids.New()+".eml")
	if err := os.WriteFile(orphan, []byte("nobody will ever forward this"), 0o600); err != nil {
		t.Fatalf("write orphan: %v", err)
	}

	n, err := s.SweepOrphans()
	if err != nil {
		t.Fatalf("SweepOrphans: %v", err)
	}

	if n != 1 {
		t.Errorf("swept %d files, want only the orphan", n)
	}

	if _, err := s.ReceivedBody(in.ID); err != nil {
		t.Errorf("the sweep removed received mail: %v", err)
	}

	if _, err := s.Body(out.ID); err != nil {
		t.Errorf("the sweep removed queued mail: %v", err)
	}

	if _, err := os.Stat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Error("the orphan survived")
	}
}

// A verified name covers everything under it, and nothing that merely
// looks like it. Same rule as the platform, from the same function -
// this is the check standing in front of an open port 25.
func TestTheAcceptListCoversSubdomainsAndNothingElse(t *testing.T) {
	a := &acceptList{}
	a.replace([]string{"Example.com.", "in.other.test"}, "etag-1")

	for _, addr := range []string{
		"user@example.com", "user@mail.example.com", "user@a.b.example.com",
		"user@in.other.test", "user@deep.in.other.test",
	} {
		if !a.covers(addr) {
			t.Errorf("%s should be covered", addr)
		}
	}
	for _, addr := range []string{
		"user@evilexample.com", "user@example.com.evil.net",
		"user@other.test", "user@nothing.test", "no-at-sign", "user@",
	} {
		if a.covers(addr) {
			t.Errorf("%s must not be covered", addr)
		}
	}
}

// Restored before the listener can bind, so a node restarting while
// the control plane is unreachable keeps answering RCPT the way it
// did rather than refusing everything until a heartbeat lands.
func TestTheAcceptListSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenSpool(dir)
	if err != nil {
		t.Fatalf("OpenSpool: %v", err)
	}
	first := &acceptList{}
	first.replace([]string{"in.example.com"}, "etag-1")
	if err := first.store(s, []string{"in.example.com"}, "etag-1"); err != nil {
		t.Fatalf("store: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	again, err := OpenSpool(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = again.Close() }()

	second := &acceptList{}
	second.load(again)
	if !second.ready() {
		t.Fatal("the node came back not knowing what it may accept")
	}

	if !second.covers("x@in.example.com") {
		t.Error("the restored list does not cover what it did")
	}

	if second.etagOf() != "etag-1" {
		t.Errorf("etag = %q, want etag-1 - a lost etag refetches the list on every beat", second.etagOf())
	}
}

// An installation with no verified domains has an empty list and a
// perfectly good etag. Reading that as "I have not been told yet"
// would leave the node answering 451 forever, and reading absence of
// the list as "unchanged" would leave it accepting mail for names
// that were removed. The etag decides, the contents never do.
func TestAnEmptyListIsAnAnswerNotAnAbsence(t *testing.T) {
	a := &acceptList{}
	if a.ready() {
		t.Fatal("a node that has heard nothing must not claim to know")
	}
	a.replace(nil, "etag-empty")
	if !a.ready() {
		t.Error("an empty list is still an answer")
	}

	if a.covers("x@in.example.com") {
		t.Error("an empty list covers nothing")
	}
}

func testReceiveListener(t *testing.T, a *acceptList) (string, *Spool) {
	t.Helper()
	spool := testSpool(t)
	b := &ReceiveBackend{Spool: spool, Accept: a, Log: quietLog(), MaxMessageSize: 1 << 20}
	srv := NewReceiveServer(b, "127.0.0.1:0", "node-test", nil)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	return ln.Addr().String(), spool
}

func deliverTo(addr, from string, to []string, msg string) error {
	c, err := smtp.Dial(addr)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
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

const inboundFixture = "From: MAILER-DAEMON@far.test\r\n" +
	"To: bounces@in.example.com\r\n" +
	"Subject: Undelivered Mail Returned to Sender\r\n\r\n" +
	"report\r\n"

// The whole point: the message is on disk before the sender is told
// 250, and the peer details the bytes cannot carry are recorded with it.
func TestAnAcceptedMessageIsSpooledWithWhatOnlyTheSessionKnew(t *testing.T) {
	a := &acceptList{}
	a.replace([]string{"in.example.com"}, "etag-1")
	addr, spool := testReceiveListener(t, a)

	if err := deliverTo(addr, "", []string{"bounces@in.example.com"}, inboundFixture); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	due, err := spool.ReceivedDue(time.Now().UTC().Add(time.Minute), 10)
	if err != nil {
		t.Fatalf("ReceivedDue: %v", err)
	}

	if len(due) != 1 {
		t.Fatalf("spooled %d messages, want 1", len(due))
	}
	m := due[0]
	if m.EnvelopeFrom != "" {
		t.Errorf("envelope from = %q, want empty - a null return path is what a bounce carries", m.EnvelopeFrom)
	}

	if len(m.Recipients) != 1 || m.Recipients[0] != "bounces@in.example.com" {
		t.Errorf("recipients = %v", m.Recipients)
	}

	if m.ClientIP == "" {
		t.Error("the connecting address was not recorded - the platform computes SPF from it")
	}
	body, err := spool.ReceivedBody(m.ID)
	if err != nil || !strings.Contains(string(body), "Undelivered") {
		t.Errorf("body = %q, err %v", body, err)
	}
}

// 451 and not 550, because "I do not know yet" is not "no such
// domain". A hard refusal here bounces good mail for the minutes
// between this node starting and its first heartbeat landing.
func TestRCPTIsTemporarilyRefusedBeforeTheListArrives(t *testing.T) {
	addr, spool := testReceiveListener(t, &acceptList{})

	err := deliverTo(addr, "a@far.test", []string{"bounces@in.example.com"}, inboundFixture)
	if err == nil || !strings.Contains(err.Error(), "451") {
		t.Fatalf("want a 451, got %v", err)
	}
	due, _ := spool.ReceivedDue(time.Now().UTC().Add(time.Minute), 10)
	if len(due) != 0 {
		t.Errorf("spooled %d messages after refusing the recipient", len(due))
	}
}

func TestRCPTIsPermanentlyRefusedForADomainNobodyVerified(t *testing.T) {
	a := &acceptList{}
	a.replace([]string{"in.example.com"}, "etag-1")
	addr, _ := testReceiveListener(t, a)

	err := deliverTo(addr, "a@far.test", []string{"someone@not.ours.test"}, inboundFixture)
	if err == nil || !strings.Contains(err.Error(), "550") {
		t.Fatalf("want a 550, got %v", err)
	}
}

func TestForwardBackoffGrowsAndIsCapped(t *testing.T) {
	prev := time.Duration(0)
	for attempts := 1; attempts <= 20; attempts++ {
		d := forwardBackoff(attempts)
		if d < prev {
			t.Fatalf("backoff shrank at attempt %d: %s after %s", attempts, d, prev)
		}

		if d > 10*time.Minute {
			t.Fatalf("backoff exceeded the ceiling at attempt %d: %s", attempts, d)
		}
		prev = d
	}
	if prev != 10*time.Minute {
		t.Errorf("backoff never reached the ceiling, stopped at %s", prev)
	}
}

// The node's own scope narrows the platform's list and never widens it.
//
// A node given several tenants' domains by a control plane that knows
// nothing about which SPF records name it would happily take mail for
// all of them. relay_node.domains is how the machine says which ones
// are actually its own, and the platform's list still has to agree -
// which is why this is an AND.
func TestTheNodesOwnScopeNarrowsTheAcceptList(t *testing.T) {
	platform := &acceptList{}
	platform.replace([]string{"a.test", "b.test"}, "etag-1")

	scope := newDomainScope([]string{"a.test"})

	if !scope.covers("hi@a.test") {
		t.Error("a domain this node serves must be accepted")
	}

	// The platform would take it. This node does not carry it.
	if scope.covers("hi@b.test") {
		t.Error("a domain outside the node's scope must be refused even when the platform lists it")
	}

	if !platform.covers("hi@b.test") {
		t.Fatal("fixture is wrong - the platform half must still cover b.test")
	}

	// Subdomains ride along, by the same rule the platform applies.
	if !scope.covers("hi@mail.a.test") {
		t.Error("a subdomain of a scoped domain must be accepted, as the platform's own list would")
	}
}

// An unset scope is not an empty one. Every node that existed before
// this config key must keep accepting whatever the platform hands it.
func TestAnUnsetScopeRestrictsNothing(t *testing.T) {
	if !newDomainScope(nil).covers("hi@anything.test") {
		t.Error("an empty relay_node.domains must leave the decision to the platform")
	}

	var absent *domainScope
	if !absent.covers("hi@anything.test") {
		t.Error("a backend built without a scope must not refuse everybody")
	}
}

// A malformed MAIL FROM is answered 501 INSIDE go-smtp - no hook of
// ours runs - so a worker session that dies there would leave this
// log empty while the platform's log blames this node. What this side
// can always say is that a session ended with nothing handed over,
// and whether it got past MAIL.
func TestASessionThatHandsOverNothingIsLogged(t *testing.T) {
	var buf strings.Builder
	log := slog.New(slog.NewTextHandler(&buf, nil))

	b := &Backend{Spool: testSpool(t), Log: log, MaxMessageSize: 1 << 20}
	s := &session{backend: b, remote: "10.0.0.7:12345"}

	// The 501 case: the library refused MAIL, our hooks saw nothing.
	if err := s.Logout(); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "session ended without a message") || !strings.Contains(got, "mail_seen=false") {
		t.Errorf("the empty session is not in the log:\n%s", got)
	}

	// And the control: a session that spooled something says nothing
	// extra - one accepted line per message is already there.
	buf.Reset()
	s = &session{backend: b, accepted: 1}
	_ = s.Logout()
	if buf.Len() != 0 {
		t.Errorf("a productive session must not log an empty-session line:\n%s", buf.String())
	}
}

// The protocol trace is what shows the refused command itself. Lines
// are split and capped, so a message body at debug level is legible
// rather than one megabyte-long entry.
func TestTheProtocolTraceSplitsAndCapsLines(t *testing.T) {
	var buf strings.Builder
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	w := ProtocolTrace(log, "worker")
	// Two writes, one line split across them - the writer must join.
	_, _ = w.Write([]byte("MAIL FROM:<Mail"))
	_, _ = w.Write([]byte("yard>\r\n501 5.5.2 Was expecting MAIL arg syntax of FROM:<address>\r\n"))
	_, _ = w.Write([]byte(strings.Repeat("x", 2000) + "\n"))

	got := buf.String()
	if !strings.Contains(got, "MAIL FROM:<Mailyard>") {
		t.Errorf("the refused command is not in the trace:\n%s", got)
	}

	if !strings.Contains(got, "501 5.5.2") {
		t.Errorf("the refusal is not in the trace:\n%s", got)
	}

	if strings.Contains(got, strings.Repeat("x", 600)) {
		t.Errorf("a long line was not capped")
	}
}
