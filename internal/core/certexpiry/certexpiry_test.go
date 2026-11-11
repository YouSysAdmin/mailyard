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
	return &certmodel.Certificate{Scope: "managed", Name: "web", NotAfter: new(time.Now().Add(d))}
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

// Mail at two weeks, at one week, then every day from three days out,
// however often the job runs. Between thirty days and two weeks the
// sweep only logs, so a certificate on a short lifetime that renews on
// time never reaches anybody's inbox.
func TestMailedAtTwoWeeksOneWeekThenDaily(t *testing.T) {
	mail := &fakeMail{on: true}
	cert := at(20 * 24 * time.Hour)
	c := checker([]*certmodel.Certificate{cert}, mail)

	run := func(left time.Duration, times int, want int) {
		t.Helper()

		cert.NotAfter = new(time.Now().Add(left))
		for range times {
			if err := c.Run(t.Context()); err != nil {
				t.Fatalf("Run: %v", err)
			}
		}

		if mail.sent != want {
			t.Fatalf("at %v left: sent %d mails, want %d", left, mail.sent, want)
		}
	}

	run(20*24*time.Hour, 3, 0)
	run(13*24*time.Hour, 3, 1)
	run(10*24*time.Hour, 3, 1)
	run(6*24*time.Hour, 3, 2)
	run(4*24*time.Hour, 3, 2)
	run(2*24*time.Hour, 3, 3)

	// A day later it speaks again, because the condition is still true
	// and silence would read as resolved.
	c.lastDaily = time.Now().Add(-25 * time.Hour)
	run(time.Hour, 3, 4)
}

// A renewed certificate starts over: it is mailed about again only when
// the new one reaches two weeks.
func TestARenewedCertificateStartsOver(t *testing.T) {
	mail := &fakeMail{on: true}
	cert := at(6 * 24 * time.Hour)
	store := &fakeStore{rows: []*certmodel.Certificate{cert}}
	c := checker(nil, mail)
	c.Store = store

	if err := c.Run(t.Context()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	store.rows = nil
	if err := c.Run(t.Context()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	cert.NotAfter = new(time.Now().Add(13 * 24 * time.Hour))
	store.rows = []*certmodel.Certificate{cert}
	if err := c.Run(t.Context()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if mail.sent != 2 {
		t.Errorf("sent %d mails, want one before and one after the renewal", mail.sent)
	}
}

// Only what is inside two weeks is listed, so a digest about one
// certificate is not padded with others that are merely in the log.
func TestTheDigestListsOnlyWhatIsMailedAbout(t *testing.T) {
	mail := &fakeMail{on: true}
	rows := []*certmodel.Certificate{at(2 * 24 * time.Hour), at(25 * 24 * time.Hour)}
	rows[1].Name = "later"
	if err := checker(rows, mail).Run(t.Context()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if strings.Contains(mail.last, "later") {
		t.Errorf("a certificate outside two weeks was mailed about:\n%s", mail.last)
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
		"weeks left":   {10 * 24 * time.Hour, "expiring soon"},
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
