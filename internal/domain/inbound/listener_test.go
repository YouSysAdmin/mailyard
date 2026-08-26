// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package inbound

import (
	"context"
	"log/slog"
	"net"
	"net/smtp"
	"strings"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/domain/store"
	dmodel "github.com/yousysadmin/mailyard/internal/models/domain"
	imodel "github.com/yousysadmin/mailyard/internal/models/inbound"
	suppressionmodel "github.com/yousysadmin/mailyard/internal/models/suppression"
)

// fakeDomains serves one verified domain.
type fakeDomains struct{ verified *dmodel.Domain }

func (f *fakeDomains) Get(context.Context, string, string) (*dmodel.Domain, error) { return nil, nil }
func (f *fakeDomains) GetVerifiedByName(_ context.Context, name string) (*dmodel.Domain, error) {
	if f.verified != nil && f.verified.Domain == name {
		return f.verified, nil
	}

	return nil, nil
}

// GetVerifiedCovering accepts the name and anything under it, which
// is what lets an MX on a subdomain deliver to a project that
// verified the apex.
func (f *fakeDomains) GetVerifiedCovering(_ context.Context, name string) (*dmodel.Domain, error) {
	if f.verified == nil {
		return nil, nil
	}

	if name == f.verified.Domain || strings.HasSuffix(name, "."+f.verified.Domain) {
		return f.verified, nil
	}

	return nil, nil
}
func (f *fakeDomains) GetByName(context.Context, string) (*dmodel.Domain, error) { return nil, nil }
func (f *fakeDomains) List(context.Context, string) ([]*dmodel.Domain, error)    { return nil, nil }
func (f *fakeDomains) VerifiedNames(context.Context) ([]string, error) {
	if f.verified == nil {
		return nil, nil
	}

	return []string{f.verified.Domain}, nil
}
func (f *fakeDomains) VerifiedNamesIn(_ context.Context, projID string) ([]string, error) {
	if f.verified == nil || f.verified.ProjectID != projID {
		return nil, nil
	}

	return []string{f.verified.Domain}, nil
}
func (f *fakeDomains) Put(context.Context, *dmodel.Domain) error { return nil }
func (f *fakeDomains) SetVerified(context.Context, string, string, bool, time.Time) error {
	return nil
}
func (f *fakeDomains) Delete(context.Context, string, string) error { return nil }
func (f *fakeDomains) Count(context.Context, string) (int, error)   { return 0, nil }

// fakeInbound stores rows in memory.
type fakeInbound struct{ rows []*imodel.Email }

func (f *fakeInbound) Get(context.Context, string, string) (*imodel.Email, error) { return nil, nil }
func (f *fakeInbound) List(context.Context, string, store.InboundFilter) ([]*imodel.Email, error) {
	return f.rows, nil
}
func (f *fakeInbound) Put(_ context.Context, e *imodel.Email) error {
	f.rows = append(f.rows, e)

	return nil
}
func (f *fakeInbound) Delete(context.Context, string, string) error { return nil }
func (f *fakeInbound) FindByDedupHash(_ context.Context, _, h string) (*imodel.Email, error) {
	for _, r := range f.rows {
		if r.DedupHash == h {
			return r, nil
		}
	}

	return nil, nil
}
func (f *fakeInbound) CountByStatus(context.Context, string) (map[string]int, error) {
	return nil, nil
}
func (f *fakeInbound) StorageKeysOlderThan(context.Context, time.Time) ([]string, error) {
	return nil, nil
}
func (f *fakeInbound) StorageKeysForProject(context.Context, string) ([]string, error) {
	return nil, nil
}
func (f *fakeInbound) PurgeOlderThan(context.Context, time.Time) (int64, error) { return 0, nil }
func (f *fakeInbound) ClearContentOlderThan(context.Context, time.Time) (int64, error) {
	return 0, nil
}

// fakeSuppressions suppresses one address and records what the DSN
// pipeline writes back.
type fakeSuppressions struct {
	blocked  string
	recorded []*suppressionmodel.Suppression
}

func (f *fakeSuppressions) List(context.Context, string, store.SuppressionFilter) ([]*suppressionmodel.Suppression, error) {
	return nil, nil
}
func (f *fakeSuppressions) Upsert(_ context.Context, s *suppressionmodel.Suppression) error {
	f.recorded = append(f.recorded, s)

	return nil
}
func (f *fakeSuppressions) Delete(context.Context, string, string) (bool, error) { return false, nil }
func (f *fakeSuppressions) PurgeForAddress(context.Context, string, string) (int64, error) {
	return 0, nil
}
func (f *fakeSuppressions) IsSuppressed(_ context.Context, _, email string) (bool, error) {
	return email == f.blocked, nil
}
func (f *fakeSuppressions) FilterSuppressed(_ context.Context, _ string, emails []string) ([]string, []string, error) {
	return emails, nil, nil
}
func (f *fakeSuppressions) IsSuppressedForList(_ context.Context, _, email, _ string) (bool, error) {
	return email == f.blocked, nil
}
func (f *fakeSuppressions) FilterSuppressedForList(_ context.Context, _, _ string, emails []string) ([]string, []string, error) {
	return emails, nil, nil
}
func (f *fakeSuppressions) CountForList(context.Context, string, string) (int, error) {
	return 0, nil
}
func (f *fakeSuppressions) DeleteForList(context.Context, string, string, string) (bool, error) {
	return false, nil
}

func startListener(t *testing.T, blockedSender string) (string, *fakeInbound, *[]string) {
	t.Helper()
	rows := &fakeInbound{}
	var events []string
	svc := &Service{
		Domains:      &fakeDomains{verified: &dmodel.Domain{ID: "dom-1", ProjectID: "proj-1", Domain: "in.example.com", Verified: true}},
		Inbound:      rows,
		Suppressions: &fakeSuppressions{blocked: blockedSender},
		Emit: func(_ context.Context, _, event, _ string, _ any) {
			events = append(events, event)
		},
		Log: slog.New(slog.DiscardHandler),
	}
	b := &Backend{Service: svc, MaxMessageSize: 1 << 20}
	srv := NewServer(b, "127.0.0.1:0", "mx-test", nil)
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		t.Fatal(err)
	}

	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	return ln.Addr().String(), rows, &events
}

func deliver(addr, from string, to []string, msg string) error {
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

const inboundMsg = "From: Sender <someone@remote.example>\r\n" +
	"To: support@in.example.com\r\n" +
	"Subject: help please\r\n" +
	"Message-Id: <m1@remote.example>\r\n" +
	"Content-Type: text/plain; charset=UTF-8\r\n" +
	"\r\n" +
	"inbound body\r\n"

func TestInboundAcceptsVerifiedDomain(t *testing.T) {
	addr, rows, events := startListener(t, "")
	if err := deliver(addr, "someone@remote.example", []string{"support@in.example.com"}, inboundMsg); err != nil {
		t.Fatalf("deliver: %v", err)
	}

	if len(rows.rows) != 1 {
		t.Fatalf("stored rows = %d, want 1", len(rows.rows))
	}

	rec := rows.rows[0]
	if rec.ProjectID != "proj-1" || rec.DomainID != "dom-1" {
		t.Errorf("routing = proj %q dom %q", rec.ProjectID, rec.DomainID)
	}

	if rec.Subject != "help please" || !strings.Contains(rec.TextBody, "inbound body") {
		t.Errorf("parsed subject %q body %q", rec.Subject, rec.TextBody)
	}

	if rec.MessageID != "m1@remote.example" {
		t.Errorf("message id = %q", rec.MessageID)
	}

	if len(*events) != 1 || (*events)[0] != "inbound.received" {
		t.Errorf("events = %v", *events)
	}
}

func TestInboundRejectsUnknownDomain(t *testing.T) {
	addr, rows, _ := startListener(t, "")
	err := deliver(addr, "a@remote.example", []string{"x@other.example.com"}, inboundMsg)
	if err == nil || !strings.Contains(err.Error(), "550") {
		t.Errorf("unknown domain must get 550, got %v", err)
	}

	if len(rows.rows) != 0 {
		t.Errorf("nothing must be stored, got %d rows", len(rows.rows))
	}
}

func TestInboundRejectsSuppressedSender(t *testing.T) {
	addr, rows, events := startListener(t, "spammer@remote.example")
	err := deliver(addr, "spammer@remote.example", []string{"support@in.example.com"}, inboundMsg)
	if err == nil || !strings.Contains(err.Error(), "550") {
		t.Errorf("suppressed sender must get 550, got %v", err)
	}

	if len(rows.rows) != 1 || rows.rows[0].Status != imodel.StatusRejected {
		t.Errorf("rejection must be persisted for audit, rows = %+v", rows.rows)
	}

	if len(*events) != 0 {
		t.Errorf("no webhook for rejected mail, events = %v", *events)
	}
}

func TestInboundDuplicateIsIdempotent(t *testing.T) {
	addr, rows, events := startListener(t, "")
	for range 2 {
		if err := deliver(addr, "someone@remote.example", []string{"support@in.example.com"}, inboundMsg); err != nil {
			t.Fatalf("deliver: %v", err)
		}
	}

	if len(rows.rows) != 1 {
		t.Errorf("duplicate message-id must store once, got %d", len(rows.rows))
	}

	if len(*events) != 1 {
		t.Errorf("duplicate must not re-emit, events = %v", *events)
	}
}

func TestInboundMalformedStoredAsFailed(t *testing.T) {
	addr, rows, events := startListener(t, "")
	if err := deliver(addr, "someone@remote.example", []string{"support@in.example.com"},
		"Content-Type: multipart/mixed\r\n\r\nnot a boundary\r\n"); err != nil {
		t.Fatalf("deliver: %v", err)
	}

	if len(rows.rows) != 1 || rows.rows[0].Status != imodel.StatusFailed {
		t.Fatalf("malformed message must be stored failed, rows = %+v", rows.rows)
	}

	if len(rows.rows[0].Raw) == 0 {
		t.Error("raw bytes must be retained for failed parses")
	}

	if len(*events) != 0 {
		t.Errorf("no webhook for failed parse, events = %v", *events)
	}
}
