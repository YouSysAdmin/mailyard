// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relayagent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/mx"
	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
)

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testSpool(t *testing.T) *Spool {
	t.Helper()
	s, err := OpenSpool(t.TempDir())
	if err != nil {
		t.Fatalf("OpenSpool: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	return s
}

func queued(id string) *Message {
	now := time.Now().UTC()

	return &Message{
		ID: id, EmailID: "e-1", EnvelopeFrom: "bounces@mail.example.com",
		Domain: "example.org", Recipients: []string{"user@example.org"},
		AcceptedAt: now, NextAttempt: now,
	}
}

const rawBody = "X-Mailyard-Email-Id: e-1\r\n" +
	"DKIM-Signature: v=1; a=rsa-sha256; d=user.com; b=abc\r\n" +
	"From: News <news@user.com>\r\n" +
	"Subject: hi\r\n\r\nbody\r\n"

// The node accepts a message and then owns it. The worker that handed
// it over has already recorded the send, so a queue that did not
// survive a restart would lose mail Mailyard believes was sent.
func TestTheQueueSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenSpool(dir)
	if err != nil {
		t.Fatalf("OpenSpool: %v", err)
	}
	m := queued(ids.New())
	m.Attempts = 3
	m.NextAttempt = time.Now().Add(-time.Minute)
	if err := s.Put(m, []byte(rawBody)); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	again, err := OpenSpool(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = again.Close() }()

	due, err := again.Due(time.Now(), 0)
	if err != nil {
		t.Fatalf("Due: %v", err)
	}

	if len(due) != 1 {
		t.Fatalf("after a restart the queue holds %d messages, want 1", len(due))
	}

	if due[0].Attempts != 3 || due[0].EmailID != "e-1" {
		t.Errorf("the retry state did not survive: %+v", due[0])
	}
	body, err := again.Body(due[0].ID)
	if err != nil || string(body) != rawBody {
		t.Errorf("the message bytes did not survive: %v", err)
	}
}

func TestAMessageNotYetDueIsNotReturned(t *testing.T) {
	s := testSpool(t)
	m := queued(ids.New())
	m.NextAttempt = time.Now().Add(time.Hour)
	if err := s.Put(m, []byte(rawBody)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	due, err := s.Due(time.Now(), 0)
	if err != nil {
		t.Fatalf("Due: %v", err)
	}

	if len(due) != 0 {
		t.Errorf("a message scheduled for later was handed out now")
	}
}

// A backlog must drain oldest first. Bolt walks in key order, which
// is uuid order and therefore arbitrary.
func TestTheBacklogDrainsOldestFirst(t *testing.T) {
	s := testSpool(t)
	base := time.Now().Add(-time.Hour).UTC()
	for i, offset := range []time.Duration{30 * time.Minute, 0, 15 * time.Minute} {
		m := queued(ids.New())
		m.AcceptedAt = base.Add(offset)
		m.NextAttempt = base
		m.LastError = fmt.Sprint(i)
		if err := s.Put(m, []byte(rawBody)); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	due, err := s.Due(time.Now(), 0)
	if err != nil {
		t.Fatalf("Due: %v", err)
	}
	for i := 1; i < len(due); i++ {
		if due[i].AcceptedAt.Before(due[i-1].AcceptedAt) {
			t.Fatalf("queue came back out of order: %v", due)
		}
	}
}

// Put writes bytes first and metadata second, so a crash between them
// leaves a file nothing will ever deliver. The sweep is what stops
// those accumulating until the disk fills.
func TestOrphanedBodiesAreSweptUp(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenSpool(dir)
	if err != nil {
		t.Fatalf("OpenSpool: %v", err)
	}
	defer func() { _ = s.Close() }()

	live := queued(ids.New())
	if err := s.Put(live, []byte(rawBody)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	orphan := filepath.Join(dir, "data", ids.New()+".eml")
	if err := os.WriteFile(orphan, []byte("nobody will ever send this"), 0o600); err != nil {
		t.Fatalf("write orphan: %v", err)
	}

	n, err := s.SweepOrphans()
	if err != nil {
		t.Fatalf("SweepOrphans: %v", err)
	}

	if n != 1 {
		t.Errorf("swept %d files, want 1", n)
	}

	if _, err := os.Stat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Error("the orphan survived")
	}

	if _, err := s.Body(live.ID); err != nil {
		t.Errorf("the sweep removed a live message: %v", err)
	}
}

// bbolt takes an exclusive lock. Two agents on one spool must fail
// loudly rather than one of them hanging while looking healthy.
func TestASecondAgentCannotOpenTheSameSpool(t *testing.T) {
	dir := t.TempDir()
	first, err := OpenSpool(dir)
	if err != nil {
		t.Fatalf("OpenSpool: %v", err)
	}
	defer func() { _ = first.Close() }()

	done := make(chan error, 1)
	go func() {
		s, err := OpenSpool(dir)
		if err == nil {
			_ = s.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a second agent opened the same spool")
		}
	case <-time.After(5 * time.Second):
		t.Error("the second open hung instead of failing")
	}
}

type fakeSender struct {
	mu    sync.Mutex
	calls int
	res   *smtpclient.DirectResult
	err   error
	sent  [][]byte
	hosts []string
}

func (f *fakeSender) send(_ context.Context, _ smtpclient.DirectConfig, hosts []string, msg *smtpclient.Raw) (*smtpclient.DirectResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.hosts = hosts
	f.sent = append(f.sent, msg.Data)
	if f.err != nil {
		return nil, f.err
	}

	return f.res, nil
}

type staticResolver struct{ hosts []string }

func (s staticResolver) LookupMX(_ context.Context, _ string) ([]*net.MX, error) {
	var out []*net.MX
	for i, h := range s.hosts {
		// A null MX is the root exchange, written exactly ".", so it
		// must not have another dot appended.
		if h != "." {
			h += "."
		}
		out = append(out, &net.MX{Host: h, Pref: uint16(10 + i)})
	}

	return out, nil
}

func (s staticResolver) LookupHost(_ context.Context, h string) ([]string, error) {
	return []string{"192.0.2.1"}, nil
}

func testDeliverer(t *testing.T, s *Spool, send *fakeSender) (*Deliverer, *[]Outcome) {
	t.Helper()
	var mu sync.Mutex
	outcomes := &[]Outcome{}
	d := &Deliverer{
		Spool:       s,
		Lookup:      mx.New(mx.Config{Resolver: staticResolver{hosts: []string{"mx.example.org"}}}),
		Log:         quietLog(),
		HELO:        "node1.example.com",
		SMTPPort:    2525,
		MaxLifetime: 72 * time.Hour,
		Concurrency: 2,
		Send:        send.send,
		Report: func(_ context.Context, o Outcome) {
			mu.Lock()
			defer mu.Unlock()
			*outcomes = append(*outcomes, o)
		},
	}

	return d, outcomes
}

func TestADeliveredMessageLeavesTheQueue(t *testing.T) {
	s := testSpool(t)
	m := queued(ids.New())
	if err := s.Put(m, []byte(rawBody)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	send := &fakeSender{res: &smtpclient.DirectResult{
		Host: "mx.example.org", TLS: smtpclient.TLSVerified,
		Accepted: []string{"user@example.org"}, Rejected: map[string]error{},
	}}
	d, outcomes := testDeliverer(t, s, send)
	d.pass(t.Context())

	left, err := s.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}

	if len(left) != 0 {
		t.Errorf("a delivered message is still queued: %+v", left)
	}

	if len(*outcomes) != 1 || !(*outcomes)[0].Delivered {
		t.Errorf("outcomes: %+v", *outcomes)
	}

	// The bytes the worker signed must reach the wire unchanged.
	if len(send.sent) != 1 || string(send.sent[0]) != rawBody {
		t.Error("the message was altered before delivery")
	}
}

// The property a queue exists for: a transient failure must not lose
// the message, and must not report anything final about it.
func TestATransientFailureKeepsTheMessage(t *testing.T) {
	s := testSpool(t)
	m := queued(ids.New())
	if err := s.Put(m, []byte(rawBody)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	send := &fakeSender{err: errors.New("connection refused")}
	d, outcomes := testDeliverer(t, s, send)
	d.pass(t.Context())

	left, err := s.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}

	if len(left) != 1 {
		t.Fatalf("a deferred message was dropped")
	}

	if left[0].Attempts != 1 {
		t.Errorf("attempts = %d, want 1", left[0].Attempts)
	}

	if !left[0].NextAttempt.After(time.Now()) {
		t.Error("the message was rescheduled for the past, so it will be retried immediately")
	}

	if len(*outcomes) != 0 {
		t.Errorf("a transient failure reported a terminal outcome: %+v", *outcomes)
	}
}

// Some accepted, some permanently refused. Reporting the refused ones
// and retrying only the rest is what stops a retry delivering twice.
func TestOnlyDeferredRecipientsAreRetried(t *testing.T) {
	s := testSpool(t)
	m := queued(ids.New())
	m.Recipients = []string{"good@example.org", "gone@example.org", "busy@example.org"}
	if err := s.Put(m, []byte(rawBody)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	send := &fakeSender{res: &smtpclient.DirectResult{
		Host:     "mx.example.org",
		Accepted: []string{"good@example.org"},
		Rejected: map[string]error{
			"gone@example.org": &smtpclient.SendError{Code: 550, Msg: "no such user", Err: errors.New("550")},
			"busy@example.org": &smtpclient.SendError{Code: 451, Msg: "try later", Err: errors.New("451")},
		},
	}}
	d, outcomes := testDeliverer(t, s, send)
	d.pass(t.Context())

	left, err := s.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}

	if len(left) != 1 {
		t.Fatalf("the message should still be queued for the deferred recipient")
	}

	if len(left[0].Recipients) != 1 || left[0].Recipients[0] != "busy@example.org" {
		t.Fatalf("retry set is %v - a delivered or refused recipient would be sent to twice", left[0].Recipients)
	}
	var delivered, refused int
	for _, o := range *outcomes {
		if o.Delivered {
			delivered++
		} else if o.Permanent {
			refused++
		}
	}
	if delivered != 1 || refused != 1 {
		t.Errorf("outcomes: %+v", *outcomes)
	}
}

// A 5xx from the exchanger is the domain's answer. Retrying for three
// days asks the same question again.
func TestAPermanentRefusalIsFinal(t *testing.T) {
	s := testSpool(t)
	m := queued(ids.New())
	if err := s.Put(m, []byte(rawBody)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	send := &fakeSender{err: &smtpclient.SendError{
		Code: 550, Msg: "we do not accept your mail", Err: errors.New("550"),
	}}
	d, outcomes := testDeliverer(t, s, send)
	d.pass(t.Context())

	left, _ := s.All()
	if len(left) != 0 {
		t.Error("a permanently refused message is still being retried")
	}

	if len(*outcomes) != 1 || !(*outcomes)[0].Permanent {
		t.Errorf("outcomes: %+v", *outcomes)
	}
}

// Running out of time is final too, but it is NOT the recipient's
// fault - reporting it as permanent would suppress an address that
// may be perfectly good.
func TestExpiryIsFinalButNotPermanent(t *testing.T) {
	s := testSpool(t)
	m := queued(ids.New())
	m.AcceptedAt = time.Now().Add(-96 * time.Hour)
	if err := s.Put(m, []byte(rawBody)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	send := &fakeSender{err: errors.New("connection refused")}
	d, outcomes := testDeliverer(t, s, send)
	d.pass(t.Context())

	if left, _ := s.All(); len(left) != 0 {
		t.Error("an expired message is still queued")
	}

	if send.calls != 0 {
		t.Error("an expired message made another connection")
	}

	if len(*outcomes) != 1 {
		t.Fatalf("outcomes: %+v", *outcomes)
	}

	if (*outcomes)[0].Permanent {
		t.Error("expiry was reported as permanent, which would suppress a good address")
	}

	if (*outcomes)[0].Delivered {
		t.Error("expiry was reported as delivered")
	}
}

// A domain that publishes a null MX accepts no mail at all. Retrying
// for three days is pure delay before an identical answer.
func TestANullMXDomainIsGivenUpImmediately(t *testing.T) {
	s := testSpool(t)
	m := queued(ids.New())
	if err := s.Put(m, []byte(rawBody)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	send := &fakeSender{}
	d, outcomes := testDeliverer(t, s, send)
	d.Lookup = mx.New(mx.Config{Resolver: staticResolver{hosts: []string{"."}}})
	d.pass(t.Context())

	if left, _ := s.All(); len(left) != 0 {
		t.Error("a domain that accepts no mail is still queued")
	}

	if send.calls != 0 {
		t.Error("a connection was attempted to a null MX domain")
	}

	if len(*outcomes) != 1 || !(*outcomes)[0].Permanent {
		t.Errorf("outcomes: %+v", *outcomes)
	}
}

// The loop can start a new pass while a slow delivery is still
// running. Without the in-flight guard the same message is attempted
// twice at once, which means delivered twice.
func TestOnePassCannotOverlapAnother(t *testing.T) {
	s := testSpool(t)
	m := queued(ids.New())
	if err := s.Put(m, []byte(rawBody)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	send := &fakeSender{res: &smtpclient.DirectResult{
		Host: "mx.example.org", Accepted: []string{"user@example.org"}, Rejected: map[string]error{},
	}}
	d, _ := testDeliverer(t, s, send)

	if !d.claim(m.ID) {
		t.Fatal("could not claim a free message")
	}
	d.pass(t.Context())
	if send.calls != 0 {
		t.Error("a message already in flight was attempted again")
	}
	d.release(m.ID)

	d.pass(t.Context())
	if send.calls != 1 {
		t.Errorf("after release the message was attempted %d times, want 1", send.calls)
	}
}

// A receiver that is throttling wants fewer connections. A fixed
// short retry is indistinguishable from hammering.
func TestBackoffGrowsAndIsCapped(t *testing.T) {
	prev := time.Duration(0)
	for attempt := 1; attempt <= 12; attempt++ {
		d := backoff(attempt)
		if d <= 0 {
			t.Fatalf("attempt %d gave %v", attempt, d)
		}

		if d < prev {
			t.Fatalf("attempt %d backed off less than attempt %d (%v < %v)", attempt, attempt-1, d, prev)
		}

		if d > 4*time.Hour {
			t.Fatalf("attempt %d exceeded the ceiling: %v", attempt, d)
		}
		prev = d
	}
	if backoff(12) != 4*time.Hour {
		t.Errorf("a long-failing message is not at the ceiling: %v", backoff(12))
	}
}

// Reading a header is safe. Reparsing and re-rendering the message is
// not: those bytes are DKIM-signed by a process that is not this one.
func TestTheSendingIDIsReadFromTheHeadersOnly(t *testing.T) {
	body := []byte("From: a@b.com\r\n" +
		"X-Mailyard-Email-Id: real-id\r\n" +
		"\r\n" +
		"X-Mailyard-Email-Id: forged-id\r\n")
	if got := headerValue(body, smtpclient.HeaderEmailID); got != "real-id" {
		t.Errorf("id is %q - the body was searched as well as the headers", got)
	}
}

func TestAMissingSendingIDIsEmptyNotAGuess(t *testing.T) {
	if got := headerValue([]byte("From: a@b.com\r\n\r\nbody"), smtpclient.HeaderEmailID); got != "" {
		t.Errorf("id is %q, want empty", got)
	}
}

// An outcome with no id cannot be attributed. Reporting it anyway
// would mean the platform inventing which message it was about.
func TestAnOutcomeWithNoIDIsNotReported(t *testing.T) {
	s := testSpool(t)
	m := queued(ids.New())
	m.EmailID = ""
	if err := s.Put(m, []byte(rawBody)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	send := &fakeSender{res: &smtpclient.DirectResult{
		Host: "mx.example.org", Accepted: []string{"user@example.org"}, Rejected: map[string]error{},
	}}
	d, outcomes := testDeliverer(t, s, send)
	d.pass(t.Context())

	if len(*outcomes) != 0 {
		t.Errorf("an unattributable outcome was reported: %+v", *outcomes)
	}

	if left, _ := s.All(); len(left) != 0 {
		t.Error("the message was not delivered")
	}
}
