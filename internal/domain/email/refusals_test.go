// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/queue"
	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
	bmodel "github.com/yousysadmin/mailyard/internal/models/bounce"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
	supmodel "github.com/yousysadmin/mailyard/internal/models/suppression"
)

type recordingBounces struct {
	fakeBounces
	recipients []string
}

func (f *recordingBounces) Put(_ context.Context, b *bmodel.Bounce) error {
	f.recipients = append(f.recipients, b.Recipient)

	return nil
}

type recordingSuppressions struct {
	acceptSuppressions
	emails []string
}

func (f *recordingSuppressions) Upsert(_ context.Context, s *supmodel.Suppression) error {
	f.emails = append(f.emails, s.Email)

	return nil
}

func refusal(rcpt string) *smtpclient.SendError {
	return &smtpclient.SendError{Stage: "RCPT TO", Recipient: rcpt, Code: 550, Err: errors.New("550 user unknown")}
}

// A message one recipient refused is sent to the others: the outcome is
// done, naming the refused address, and only that address bounces.
func TestAPartialDeliveryIsSentAndBouncesOnlyTheRefused(t *testing.T) {
	script := &scriptedSend{byHost: map[string]error{"first": &smtpclient.RecipientRefusals{
		Refusals: []*smtpclient.SendError{refusal("gone@x.com")}, Delivered: true,
	}}}
	p := failoverProcessor(t, []*ssmodel.Server{srv("first", func(s *ssmodel.Server) { s.Host = "first" })}, script)
	bounces, sups := &recordingBounces{}, &recordingSuppressions{}
	p.Store.Bounce, p.Store.Suppression, p.AutoSuppress = bounces, sups, true

	e := delivery()
	e.Recipients = []string{"ann@x.com", "gone@x.com", "bob@x.com"}
	out := p.Process(t.Context(), e)
	if out.Kind != queue.KindDone {
		t.Fatalf("outcome %v (%v), want done", out.Kind, out.Err)
	}

	if !slices.Equal(out.Refused, []string{"gone@x.com"}) || out.Err == nil {
		t.Errorf("refused = %v, err = %v, want gone@x.com named", out.Refused, out.Err)
	}

	if !slices.Equal(bounces.recipients, []string{"gone@x.com"}) || !slices.Equal(sups.emails, []string{"gone@x.com"}) {
		t.Errorf("bounces %v suppressions %v, want gone@x.com alone", bounces.recipients, sups.emails)
	}
}

// Every recipient refused fails the message, and each one bounces.
func TestEveryRecipientRefusedFailsAndBouncesEach(t *testing.T) {
	script := &scriptedSend{byHost: map[string]error{"first": &smtpclient.RecipientRefusals{
		Refusals: []*smtpclient.SendError{refusal("a@x.com"), refusal("b@x.com")},
	}}}
	p := failoverProcessor(t, []*ssmodel.Server{
		srv("first", func(s *ssmodel.Server) { s.Host = "first" }),
		srv("second", func(s *ssmodel.Server) { s.Host = "second" }),
	}, script)
	bounces := &recordingBounces{}
	p.Store.Bounce = bounces

	e := delivery()
	e.Recipients = []string{"a@x.com", "b@x.com"}
	out := p.Process(t.Context(), e)
	if out.Kind != queue.KindFail {
		t.Fatalf("outcome %v, want failed", out.Kind)
	}

	if len(script.tried) != 1 {
		t.Errorf("tried %v, want the walk stopped at the refusal", script.tried)
	}

	if !slices.Equal(bounces.recipients, []string{"a@x.com", "b@x.com"}) {
		t.Errorf("bounces %v, want both", bounces.recipients)
	}
}
