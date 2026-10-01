// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package keyexpiry

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	smodel "github.com/yousysadmin/mailyard/internal/models/sender"
)

type fakeStore struct{ rows []*smodel.SigningKey }

func (f *fakeStore) SigningExpiringBefore(context.Context, time.Time) ([]*smodel.SigningKey, error) {
	return f.rows, nil
}

type fakeMail struct {
	sent map[string]int
	last string
}

func (m *fakeMail) Enabled() bool { return true }
func (m *fakeMail) Send(_ context.Context, to []string, subject, _, text string) error {
	if m.sent == nil {
		m.sent = map[string]int{}
	}

	m.sent[to[0]]++
	m.last = subject + "\n" + text

	return nil
}

func key(project, email string, d time.Duration) *smodel.SigningKey {
	return &smodel.SigningKey{
		ProjectID: project, SenderEmail: email, Kind: "smime", NotAfter: new(time.Now().Add(d)),
	}
}

func checker(rows []*smodel.SigningKey, mail *fakeMail) *Checker {
	return &Checker{
		Store: &fakeStore{rows: rows},
		Mail:  mail,
		Recipients: func(_ context.Context, project string) ([]string, error) {
			return []string{"owner-of-" + project + "@example.com"}, nil
		},
		Log: slog.New(slog.DiscardHandler),
	}
}

// Each project hears about its own keys, once a day however often the
// sweep runs, and a quiet installation hears nothing.
func TestEachProjectIsMailedOnceADayAboutItsOwnKeys(t *testing.T) {
	mail := &fakeMail{}
	if err := checker(nil, mail).Run(t.Context()); err != nil || len(mail.sent) != 0 {
		t.Fatalf("quiet sweep: %v, mailed %v", err, mail.sent)
	}

	c := checker([]*smodel.SigningKey{
		key("p1", "billing@one.example", 3*24*time.Hour),
		key("p1", "sales@one.example", 20*24*time.Hour),
		key("p2", "hello@two.example", -24*time.Hour),
	}, mail)
	for range 3 {
		if err := c.Run(t.Context()); err != nil {
			t.Fatal(err)
		}
	}

	if mail.sent["owner-of-p1@example.com"] != 1 || mail.sent["owner-of-p2@example.com"] != 1 {
		t.Errorf("mailed %v, want one each", mail.sent)
	}

	if !strings.Contains(mail.last, "EXPIRED") && !strings.Contains(mail.last, "within a week") {
		t.Errorf("the digest does not carry the urgency:\n%s", mail.last)
	}
}
