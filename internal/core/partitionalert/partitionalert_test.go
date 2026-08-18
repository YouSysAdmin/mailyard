// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package partitionalert

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/partition"
)

type countFunc func(ctx context.Context) (partition.Health, error)

func (f countFunc) Count(ctx context.Context) (partition.Health, error) { return f(ctx) }

type recorder struct {
	enabled bool
	sent    int
	subject string
	body    string
	err     error
}

func (r *recorder) Enabled() bool { return r.enabled }

func (r *recorder) Send(_ context.Context, _ []string, subject, _, text string) error {
	if r.err != nil {
		return r.err
	}

	r.sent++
	r.subject, r.body = subject, text

	return nil
}

func checker(h partition.Health, m *recorder) *Checker {
	return &Checker{
		Counter: countFunc(func(context.Context) (partition.Health, error) { return h, nil }),
		Mail:    m,
		Admins:  func(context.Context) ([]string, error) { return []string{"admin@example.com"}, nil },
	}
}

// A healthy installation sits around 45 partitions forever. Mailing
// about that every day is how an alert becomes a filter rule.
func TestAHealthyCountSaysNothing(t *testing.T) {
	m := &recorder{enabled: true}
	if err := checker(partition.Health{Partitions: 45, Ceiling: 400}, m).Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	if m.sent != 0 {
		t.Fatalf("mailed %d times about a healthy count", m.sent)
	}
}

// The warning has to arrive while the remedy is still calm. At 365
// partitions a year, eighty percent of the ceiling is about three
// months of runway.
func TestTheWarningArrivesBeforeTheCeiling(t *testing.T) {
	m := &recorder{enabled: true}
	if err := checker(partition.Health{Partitions: 320, Ceiling: 400}, m).Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	if m.sent != 1 {
		t.Fatalf("sent %d mails at the threshold, want 1", m.sent)
	}

	if !strings.Contains(m.subject, "approaching") {
		t.Errorf("subject %q does not say the ceiling is approaching", m.subject)
	}

	// The remedy is the point of the mail. An operator who has to go
	// and research what a partition ceiling is will act next week.
	for _, want := range []string{"retention", "max_locks_per_transaction", "320", "400"} {
		if !strings.Contains(m.body, want) {
			t.Errorf("the mail never mentions %q:\n%s", want, m.body)
		}
	}
}

func TestAtTheCeilingItSaysSo(t *testing.T) {
	m := &recorder{enabled: true}
	if err := checker(partition.Health{Partitions: 400, Ceiling: 400}, m).Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	if !strings.Contains(m.subject, "has reached") {
		t.Errorf("subject %q does not distinguish reaching the ceiling from approaching it", m.subject)
	}
}

// The job runs daily and the condition lasts for months, so without
// the collapse this is a mail a day until somebody acts.
func TestItMailsOnceADay(t *testing.T) {
	m := &recorder{enabled: true}
	c := checker(partition.Health{Partitions: 320, Ceiling: 400}, m)

	for range 5 {
		if err := c.Run(context.Background()); err != nil {
			t.Fatalf("run: %v", err)
		}
	}

	if m.sent != 1 {
		t.Fatalf("sent %d mails across five runs in one day, want 1", m.sent)
	}

	// A day later it is worth saying again - the condition has not
	// been fixed and the count has grown.
	c.lastMailed = time.Now().Add(-25 * time.Hour)
	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	if m.sent != 2 {
		t.Fatalf("sent %d mails, want a second one the next day", m.sent)
	}
}

// Platform mail is optional, and every caller of it has to degrade.
func TestItSurvivesMailBeingOff(t *testing.T) {
	m := &recorder{enabled: false}
	if err := checker(partition.Health{Partitions: 400, Ceiling: 400}, m).Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	if m.sent != 0 {
		t.Fatalf("sent %d mails with the mailer disabled", m.sent)
	}
}

// A failed send must not burn the daily allowance: the next run has to
// try again, or one flaky SMTP moment costs the whole warning.
func TestAFailedSendIsRetriedTomorrow(t *testing.T) {
	m := &recorder{enabled: true, err: errors.New("smtp down")}
	c := checker(partition.Health{Partitions: 320, Ceiling: 400}, m)
	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	m.err = nil
	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	if m.sent != 1 {
		t.Fatalf("sent %d after a failure then a success, want the retry to land", m.sent)
	}
}

// A count that cannot be read is a job failure, not silence - cron
// logs it, and a silent sweep looks identical to a healthy one.
func TestAnUnreadableCountIsAnError(t *testing.T) {
	c := &Checker{Counter: countFunc(func(context.Context) (partition.Health, error) {
		return partition.Health{}, errors.New("catalog unavailable")
	})}
	if err := c.Run(context.Background()); err == nil {
		t.Fatal("a failed count was reported as a healthy sweep")
	}
}
