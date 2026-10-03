// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package queue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
)

// failingSource fails every poll query the way the database does: with
// the caller's own context error, wrapped, once that is cancelled, and
// with err otherwise.
type failingSource struct {
	*memSource
	err error
}

func (s failingSource) fail(ctx context.Context) error {
	if ctx.Err() != nil {
		return fmt.Errorf("timeout: context already done: %w", ctx.Err())
	}

	return s.err
}

func (s failingSource) RecoverStuck(ctx context.Context, _ time.Time) (int, error) {
	return 0, s.fail(ctx)
}

func (s failingSource) ClaimDue(ctx context.Context, _ time.Time, _ int) ([]*emailmodel.Email, error) {
	return nil, s.fail(ctx)
}

// errorLog keeps the messages logged at ERROR.
type errorLog struct {
	mu   sync.Mutex
	msgs []string
}

func (l *errorLog) Enabled(context.Context, slog.Level) bool { return true }

func (l *errorLog) Handle(_ context.Context, r slog.Record) error {
	if r.Level >= slog.LevelError {
		l.mu.Lock()
		l.msgs = append(l.msgs, r.Message)
		l.mu.Unlock()
	}

	return nil
}

func (l *errorLog) WithAttrs([]slog.Attr) slog.Handler { return l }
func (l *errorLog) WithGroup(string) slog.Handler      { return l }

// A poll cut short by the worker's own shutdown logs nothing, and the
// same failure with the worker running is still an error.
func TestAShutdownIsNotAClaimFailure(t *testing.T) {
	cases := []struct {
		name     string
		cancel   bool
		expected []string
	}{
		{"running", false, []string{"queue: recover stuck", "queue: claim due"}},
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
			src := failingSource{memSource: newMemSource(), err: errors.New("connection refused")}
			w := NewWorker(src, funcProcessor(func(*emailmodel.Email) Outcome { return Done() }),
				testConfig(), slog.New(got))
			w.idle.Store(1)
			w.pollOnce(ctx)

			if !slices.Equal(got.msgs, tc.expected) {
				t.Fatalf("logged %q, expected %q", got.msgs, tc.expected)
			}
		})
	}
}
