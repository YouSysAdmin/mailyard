// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package campaign

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/domain/store"
	cmodel "github.com/yousysadmin/mailyard/internal/models/campaign"
)

// failingCampaigns fails both poll queries the way the database does:
// with the caller's own context error, wrapped, once that is cancelled,
// and with err otherwise. Every other method is unimplemented.
type failingCampaigns struct {
	store.CampaignStore
	err error
}

func (s failingCampaigns) fail(ctx context.Context) error {
	if ctx.Err() != nil {
		return fmt.Errorf("timeout: context already done: %w", ctx.Err())
	}

	return s.err
}

func (s failingCampaigns) PromoteScheduled(ctx context.Context, _ time.Time) (int, error) {
	return 0, s.fail(ctx)
}

func (s failingCampaigns) ClaimDue(ctx context.Context, _ time.Time, _ time.Duration) (*cmodel.Campaign, error) {
	return nil, s.fail(ctx)
}

// errorLog keeps the messages logged at ERROR.
type errorLog struct{ msgs []string }

func (l *errorLog) Enabled(context.Context, slog.Level) bool { return true }

func (l *errorLog) Handle(_ context.Context, r slog.Record) error {
	if r.Level >= slog.LevelError {
		l.msgs = append(l.msgs, r.Message)
	}

	return nil
}

func (l *errorLog) WithAttrs([]slog.Attr) slog.Handler { return l }
func (l *errorLog) WithGroup(string) slog.Handler      { return l }

// A poll cut short by the runner's own shutdown logs nothing, and the
// same failure with the runner running is still an error.
func TestAShutdownIsNotACampaignClaimFailure(t *testing.T) {
	cases := []struct {
		name     string
		cancel   bool
		expected []string
	}{
		{"running", false, []string{"campaign: promote scheduled", "campaign: claim"}},
		{"shutting down", true, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			if tc.cancel {
				cancel()
			}

			got := &errorLog{}
			r := &Runner{
				Store: &store.Store{Campaign: failingCampaigns{err: errors.New("connection refused")}},
				Log:   slog.New(got),
				stop:  make(chan struct{}),
			}
			r.pollOnce(ctx)

			if !slices.Equal(got.msgs, tc.expected) {
				t.Fatalf("logged %q, expected %q", got.msgs, tc.expected)
			}
		})
	}
}
