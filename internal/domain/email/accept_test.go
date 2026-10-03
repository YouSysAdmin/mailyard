// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/quota"
	"github.com/yousysadmin/mailyard/internal/core/settings"
	coretracking "github.com/yousysadmin/mailyard/internal/core/tracking"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	dmodel "github.com/yousysadmin/mailyard/internal/models/domain"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
	planmodel "github.com/yousysadmin/mailyard/internal/models/plan"
	projmodel "github.com/yousysadmin/mailyard/internal/models/project"
	smodel "github.com/yousysadmin/mailyard/internal/models/setting"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
	ulmodel "github.com/yousysadmin/mailyard/internal/models/unsubscribelist"
)

// ----------------------------------------------------------------------------
// Fakes for a project that may send: a verified domain, the shared pool
// to carry it, a plan, and whatever the test puts in the lists.
// ----------------------------------------------------------------------------

type acceptProjects struct{ store.ProjectStore }

func (acceptProjects) Get(_ context.Context, id string) (*projmodel.Project, error) {
	return &projmodel.Project{ID: id}, nil
}

type acceptPlans struct {
	store.PlanStore
	plan *planmodel.Plan
}

func (f acceptPlans) GetDefault(context.Context) (*planmodel.Plan, error) { return f.plan, nil }

type acceptEmails struct {
	store.EmailStore
	accepted int
	put      *emailmodel.Email
	row      *emailmodel.Email
	reset    []string
}

func (f *acceptEmails) AcceptedSince(context.Context, string, time.Time) (int, error) {
	return f.accepted, nil
}

func (f *acceptEmails) Put(_ context.Context, e *emailmodel.Email) error {
	f.put = e

	return nil
}

func (f *acceptEmails) Get(context.Context, string, string) (*emailmodel.Email, error) {
	return f.row, nil
}

func (f *acceptEmails) Reset(_ context.Context, _, _ string, recipients []string) (bool, error) {
	f.reset = recipients

	return true, nil
}

type acceptSuppressions struct {
	store.SuppressionStore
	blocked []string
}

func (f acceptSuppressions) FilterSuppressedForList(_ context.Context, _, _ string, emails []string) ([]string, []string, error) {
	var allowed, blocked []string
	for _, e := range emails {
		if slices.Contains(f.blocked, e) {
			blocked = append(blocked, e)
		} else {
			allowed = append(allowed, e)
		}
	}

	return allowed, blocked, nil
}

type acceptLists struct {
	store.UnsubscribeListStore
	list *ulmodel.List
}

func (f acceptLists) Get(_ context.Context, _, id string) (*ulmodel.List, error) {
	if f.list != nil && f.list.ID == id {
		return f.list, nil
	}

	return nil, nil
}

func acceptingService(emails *acceptEmails, plan *planmodel.Plan, list *ulmodel.List, blocked ...string) *Service {
	return &Service{
		Store: &store.Store{
			Email:           emails,
			Project:         acceptProjects{},
			Plan:            acceptPlans{plan: plan},
			Suppression:     acceptSuppressions{blocked: blocked},
			UnsubscribeList: acceptLists{list: list},
			Sender:          noSenders{},
			SMTPServer:      &fakeProjectServers{count: 0},
			SMTPGroup:       &fakeGroups{},
			SharedSMTP:      &fakeSharedPool{servers: []*ssmodel.Shared{sharedServer("pool-1", nil)}},
			Domain:          &fakeDomains{verified: &dmodel.Domain{Domain: "example.com", ProjectID: "proj-a", Verified: true}},
		},
		Log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		MaxAttempts: 3,
		Wake:        func() {},
		Emit:        func(context.Context, string, *emailmodel.Email) {},
		Tracking:    coretracking.NewSigner("https://mail.example.test", "test-secret-test-secret-test-secret"),
	}
}

func scopedSend(listID string) *SendRequest {
	return &SendRequest{
		From: "no-reply@example.com", To: []string{"ada@x.test"}, Subject: "Shipped",
		Text: "on its way", UnsubscribeListID: listID,
	}
}

// An inactive list still scopes the send and its opt-outs, and mints no
// new link - what the documentation says active means.
func TestAnInactiveListMintsNoLinkAndStillSends(t *testing.T) {
	list := &ulmodel.List{ID: "0198f6a1-3c7e-7b21-9f4d-2a5c8e0b1d33", Name: "shipping", Active: false}

	emails := &acceptEmails{}
	e, _, err := acceptingService(emails, nil, list).Send(t.Context(), "proj-a", "", "", scopedSend(list.ID))
	if err != nil {
		t.Fatalf("Send on an inactive list = %v, want it accepted", err)
	}

	if e.ListUnsubscribeURL != "" {
		t.Errorf("an inactive list minted %q", e.ListUnsubscribeURL)
	}

	_, blocked, err := acceptingService(&acceptEmails{}, nil, list, "ada@x.test").Send(
		t.Context(), "proj-a", "", "", scopedSend(list.ID))
	if err != nil || len(blocked) != 1 {
		t.Errorf("an opted-out address on an inactive list: blocked=%v err=%v, want it dropped", blocked, err)
	}

	list.Active = true
	e, _, err = acceptingService(&acceptEmails{}, nil, list).Send(t.Context(), "proj-a", "", "", scopedSend(list.ID))
	if err != nil || e.ListUnsubscribeURL == "" {
		t.Errorf("an active list: url=%q err=%v, want a link", e.ListUnsubscribeURL, err)
	}
}

// A dry run is refused where the real send would be: over the plan's
// volume it is the same quota error, not valid:true.
func TestADryRunAgreesWithTheQuota(t *testing.T) {
	plan := &planmodel.Plan{Name: "free", HourlyEmailLimit: 10}
	req := scopedSend("")

	_, err := acceptingService(&acceptEmails{accepted: 10}, plan, nil).DryRun(t.Context(), "proj-a", req)
	if _, ok := errors.AsType[*quota.Error](err); !ok {
		t.Errorf("DryRun over the hourly limit = %v, want the quota error Send answers", err)
	}

	blocked, err := acceptingService(&acceptEmails{accepted: 3}, plan, nil, "ada@x.test").DryRun(t.Context(), "proj-a", req)
	if err != nil || len(blocked) != 1 {
		t.Errorf("DryRun within the limit = %v, %v, want the suppressed recipient reported", blocked, err)
	}
}

// A subject or body of nothing but whitespace is no subject or body.
func TestWhitespaceIsNotASubjectOrABody(t *testing.T) {
	svc := acceptingService(&acceptEmails{}, nil, nil)
	for name, req := range map[string]*SendRequest{
		"subject": {From: "a@example.com", To: []string{"b@x.test"}, Subject: "  \t ", Text: "hi"},
		"body":    {From: "a@example.com", To: []string{"b@x.test"}, Subject: "hi", Text: "  ", HTML: "\n"},
	} {
		var re *RequestError
		if err := svc.ValidateShape(req); !errors.As(err, &re) {
			t.Errorf("whitespace %s: %v, want a request error", name, err)
		}
	}
}

// A retry is refused when the content may have been cleared, and goes
// only to the recipients not suppressed since.
func TestRetryRefusesClearedContentAndDropsSuppressed(t *testing.T) {
	failed := func(created time.Time, body string) *emailmodel.Email {
		return &emailmodel.Email{
			ID: "e1", ProjectID: "proj-a", Status: emailmodel.StatusFailed, CreatedAt: created,
			Recipients: []string{"ada@x.test", "bob@x.test"}, TextBody: body,
		}
	}

	var ce *ConflictError

	emails := &acceptEmails{row: failed(time.Now(), "")}
	if _, err := acceptingService(emails, nil, nil).Retry(t.Context(), "proj-a", "e1"); !errors.As(err, &ce) {
		t.Errorf("retry of a cleared body = %v, want a conflict", err)
	}

	svc := acceptingService(&acceptEmails{row: failed(time.Now().AddDate(0, 0, -10), "hello")}, nil, nil)
	svc.Settings = settings.New(staticSettings{{Key: smodel.KeyEmailAttachmentRetentionDays, Value: "7", Type: smodel.TypeInt}})
	if err := svc.Settings.Reload(t.Context()); err != nil {
		t.Fatalf("reload settings: %v", err)
	}

	if _, err := svc.Retry(t.Context(), "proj-a", "e1"); !errors.As(err, &ce) {
		t.Errorf("retry past the attachment window = %v, want a conflict", err)
	}

	emails = &acceptEmails{row: failed(time.Now(), "hello")}
	if _, err := acceptingService(emails, nil, nil, "ada@x.test").Retry(t.Context(), "proj-a", "e1"); err != nil {
		t.Fatalf("retry = %v", err)
	}

	if !slices.Equal(emails.reset, []string{"bob@x.test"}) {
		t.Errorf("retry recipients = %v, want the suppressed one dropped", emails.reset)
	}

	emails = &acceptEmails{row: failed(time.Now(), "hello")}
	if _, err := acceptingService(emails, nil, nil, "ada@x.test", "bob@x.test").Retry(t.Context(), "proj-a", "e1"); !errors.As(err, &ce) {
		t.Errorf("retry with everybody suppressed = %v, want a conflict", err)
	}
}

type staticSettings []*smodel.Setting

func (s staticSettings) All(context.Context) ([]*smodel.Setting, error) { return s, nil }
