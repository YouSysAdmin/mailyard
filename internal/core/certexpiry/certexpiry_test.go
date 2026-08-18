// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package certexpiry

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	certmodel "github.com/yousysadmin/mailyard/internal/models/certificate"
)

type fakeStore struct{ rows []*certmodel.Certificate }

func (f *fakeStore) ExpiringBefore(context.Context, time.Time) ([]*certmodel.Certificate, error) {
	return f.rows, nil
}

type fakeMail struct {
	on   bool
	sent int
	last string
}

func (m *fakeMail) Enabled() bool { return m.on }
func (m *fakeMail) Send(_ context.Context, _ []string, subject, _, text string) error {
	m.sent++
	m.last = subject + "\n" + text

	return nil
}

func at(d time.Duration) *certmodel.Certificate {
	t := time.Now().Add(d)

	return &certmodel.Certificate{Scope: "managed", Name: "web", NotAfter: &t}
}

func checker(rows []*certmodel.Certificate, mail *fakeMail) *Checker {
	return &Checker{
		Store:  &fakeStore{rows: rows},
		Mail:   mail,
		Admins: func(context.Context) ([]string, error) { return []string{"ops@example.com"}, nil },
		Log:    slog.New(slog.DiscardHandler),
	}
}

// Nothing expiring means nothing said. A sweep that mails every six
// hours to report that everything is fine is a sweep people filter.
func TestQuietWhenNothingIsExpiring(t *testing.T) {
	mail := &fakeMail{on: true}
	if err := checker(nil, mail).Run(t.Context()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if mail.sent != 0 {
		t.Errorf("mailed %d times with nothing expiring", mail.sent)
	}
}

// One mail a day, however often the job runs. The schedule is six
// hourly so a certificate entering the window at noon is not first
// reported the following morning - that cadence belongs in the log,
// not in somebody's inbox.
func TestOneMailADay(t *testing.T) {
	mail := &fakeMail{on: true}
	c := checker([]*certmodel.Certificate{at(3 * 24 * time.Hour)}, mail)

	for range 4 {
		if err := c.Run(t.Context()); err != nil {
			t.Fatalf("Run: %v", err)
		}
	}

	if mail.sent != 1 {
		t.Errorf("four runs sent %d mails, want 1", mail.sent)
	}

	// A day later it speaks again, because the condition is still true
	// and silence would read as resolved.
	c.lastMailed = time.Now().Add(-25 * time.Hour)
	if err := c.Run(t.Context()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if mail.sent != 2 {
		t.Errorf("after a day it sent %d mails, want 2", mail.sent)
	}
}

// The subject has to carry the urgency, because that is all anybody
// reads before deciding whether to open it now.
func TestSubjectMatchesHowBadItIs(t *testing.T) {
	for name, tc := range map[string]struct {
		left time.Duration
		want string
	}{
		"already gone": {-time.Hour, "EXPIRED"},
		"days left":    {3 * 24 * time.Hour, "within a week"},
		"weeks left":   {20 * 24 * time.Hour, "expiring soon"},
	} {
		mail := &fakeMail{on: true}
		if err := checker([]*certmodel.Certificate{at(tc.left)}, mail).Run(t.Context()); err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		if mail.sent != 1 {
			t.Errorf("%s: sent %d", name, mail.sent)
			continue
		}

		if !strings.Contains(mail.last, tc.want) {
			t.Errorf("%s: subject line missing %q:\n%s", name, tc.want, mail.last)
		}
	}
}

// The worst one decides the subject, so a certificate that has already
// expired is not buried behind one with three weeks left.
func TestTheWorstOneSetsTheTone(t *testing.T) {
	mail := &fakeMail{on: true}
	rows := []*certmodel.Certificate{at(25 * 24 * time.Hour), at(-time.Hour)}
	if err := checker(rows, mail).Run(t.Context()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(mail.last, "EXPIRED") {
		t.Errorf("an expired certificate did not set the subject:\n%s", mail.last)
	}
}

// With platform mail off the sweep still runs and still logs - it is
// the log that must never go quiet.
func TestRunsWithNoMailer(t *testing.T) {
	c := checker([]*certmodel.Certificate{at(time.Hour)}, &fakeMail{on: false})
	if err := c.Run(t.Context()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	c.Mail = nil
	if err := c.Run(t.Context()); err != nil {
		t.Fatalf("Run with no mailer at all: %v", err)
	}
}
