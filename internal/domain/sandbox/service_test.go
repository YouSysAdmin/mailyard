// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package sandbox

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/settings"
	sbmodel "github.com/yousysadmin/mailyard/internal/models/sandbox"
	smodel "github.com/yousysadmin/mailyard/internal/models/setting"
)

// fakeStore records what Capture wrote, so the parsing and retention
// rules can be exercised without a database.
type fakeStore struct {
	rows      []*sbmodel.Email
	trimmedTo int
}

func (f *fakeStore) Put(_ context.Context, e *sbmodel.Email) error {
	f.rows = append(f.rows, e)

	return nil
}

func (f *fakeStore) Trim(_ context.Context, _ string, keep int) (int64, error) {
	f.trimmedTo = keep

	return 0, nil
}

func (f *fakeStore) Get(context.Context, string, string) (*sbmodel.Email, error) { return nil, nil }
func (f *fakeStore) Raw(context.Context, string, string) ([]byte, error)         { return nil, nil }
func (f *fakeStore) List(context.Context, string, int, int) ([]*sbmodel.Email, error) {
	return nil, nil
}
func (f *fakeStore) Count(context.Context, string) (int, error)             { return 0, nil }
func (f *fakeStore) Delete(context.Context, string, string) error           { return nil }
func (f *fakeStore) Clear(context.Context, string) (int64, error)           { return 0, nil }
func (f *fakeStore) PurgeExpired(context.Context, time.Time) (int64, error) { return 0, nil }

// fakeLoader feeds the settings cache without a database.
type fakeLoader struct{ rows []*smodel.Setting }

func (f fakeLoader) All(context.Context) ([]*smodel.Setting, error) { return f.rows, nil }

func testService(t *testing.T, overrides map[string]string) (*Service, *fakeStore) {
	t.Helper()
	st := &fakeStore{}
	var rows []*smodel.Setting
	for k, v := range overrides {
		rows = append(rows, &smodel.Setting{Key: k, Value: v})
	}

	set := settings.New(fakeLoader{rows: rows})
	if err := set.Reload(t.Context()); err != nil {
		t.Fatalf("reload settings: %v", err)
	}

	return &Service{
		Store:    st,
		Settings: set,
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, st
}

const rawMessage = "From: dev@localhost\r\n" +
	"To: someone@example.com\r\n" +
	"Subject: it works\r\n" +
	"X-App-Trace: abc123\r\n" +
	"Content-Type: text/plain; charset=utf-8\r\n" +
	"\r\n" +
	"hello from the test suite\r\n"

func TestCaptureStoresTheParsedMessageAndTheBytes(t *testing.T) {
	svc, st := testService(t, nil)

	e, err := svc.Capture(t.Context(), &Request{
		ProjectID:    "proj-1",
		Source:       sbmodel.SourceSubmission,
		EnvelopeFrom: "bounce@localhost",
		Recipients:   []string{"envelope@example.com"},
		Raw:          []byte(rawMessage),
	})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}

	if len(st.rows) != 1 {
		t.Fatalf("wrote %d rows, want 1", len(st.rows))
	}

	if e.Subject != "it works" {
		t.Errorf("subject parsed as %q", e.Subject)
	}

	if string(e.Raw) != rawMessage {
		t.Error("the wire bytes were not stored verbatim")
	}

	// The ENVELOPE wins over the header addresses. They differ exactly
	// where a developer is most likely to be confused - a Bcc, a VERP
	// return path - and the envelope is what a receiver would route on.
	if e.Sender != "bounce@localhost" {
		t.Errorf("sender is %q, want the envelope address", e.Sender)
	}

	if len(e.Recipients) != 1 || e.Recipients[0] != "envelope@example.com" {
		t.Errorf("recipients are %v, want the envelope recipients", e.Recipients)
	}
}

// Unparseable MIME is a thing somebody comes to the sandbox to look
// at. Dropping it would hide the one case where the raw view is the
// entire point.
func TestAMessageThatWillNotParseIsStillStored(t *testing.T) {
	svc, st := testService(t, nil)

	e, err := svc.Capture(t.Context(), &Request{
		ProjectID:  "proj-1",
		Recipients: []string{"someone@example.com"},
		Raw:        []byte("\x00\x01 this is not a message at all"),
	})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}

	if len(st.rows) != 1 {
		t.Fatalf("wrote %d rows, want 1", len(st.rows))
	}

	if len(e.Raw) == 0 {
		t.Error("the bytes were dropped, which is the only thing worth keeping here")
	}
}

// The rule the per-message window exists under: a caller may ask for
// less, never for more. Otherwise one application pins a project's
// sandbox open against the operator's setting.
func TestAMessageCanShortenItsRetentionButNotExtendIt(t *testing.T) {
	svc, _ := testService(t, map[string]string{smodel.KeySandboxRetentionDays: "7"})
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name      string
		requested int
		wantDays  int
	}{
		{"no request takes the platform window", 0, 7},
		{"a shorter window is honored", 3, 3},
		{"a longer window is clamped down", 90, 7},
		{"the same window is a no-op", 7, 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := svc.expiryFor(t.Context(), "", now, tc.requested)
			if got == nil {
				t.Fatal("no expiry was set")
			}

			want := now.AddDate(0, 0, tc.wantDays)
			if !got.Equal(want) {
				t.Errorf("expiry is %v, want %v", got, want)
			}
		})
	}
}

// Zero means keep, consistently with every other retention window in
// the product. A caller may still shorten it, which is the only way
// a per-message window makes sense against an unlimited default.
func TestAnUnlimitedPlatformWindowStillHonorsAShorterRequest(t *testing.T) {
	svc, _ := testService(t, map[string]string{smodel.KeySandboxRetentionDays: "0"})
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)

	if got := svc.expiryFor(t.Context(), "", now, 0); got != nil {
		t.Errorf("an unlimited window produced an expiry of %v", got)
	}

	got := svc.expiryFor(t.Context(), "", now, 2)
	if got == nil || !got.Equal(now.AddDate(0, 0, 2)) {
		t.Errorf("a shortened window came out as %v", got)
	}
}

func TestTheProjectCapIsAppliedOnEveryCapture(t *testing.T) {
	svc, st := testService(t, map[string]string{smodel.KeySandboxMaxMessages: "25"})

	if _, err := svc.Capture(t.Context(), &Request{ProjectID: "proj-1", Raw: []byte(rawMessage)}); err != nil {
		t.Fatalf("capture: %v", err)
	}

	if st.trimmedTo != 25 {
		t.Errorf("trimmed to %d, want the configured cap of 25", st.trimmedTo)
	}
}
