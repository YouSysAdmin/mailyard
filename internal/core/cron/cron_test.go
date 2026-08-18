// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package cron

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
)

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestDailyScheduleRollsToTomorrow(t *testing.T) {
	s := DailyAt(3, 0)

	// Before the hour: today.
	from := time.Date(2026, 8, 6, 1, 30, 0, 0, time.UTC)
	if got := s.Next(from); !got.Equal(time.Date(2026, 8, 6, 3, 0, 0, 0, time.UTC)) {
		t.Errorf("Next(01:30) = %v", got)
	}

	// After the hour: tomorrow.
	from = time.Date(2026, 8, 6, 4, 0, 0, 0, time.UTC)
	if got := s.Next(from); !got.Equal(time.Date(2026, 8, 7, 3, 0, 0, 0, time.UTC)) {
		t.Errorf("Next(04:00) = %v", got)
	}

	// Exactly on the hour must move on, not return the same instant
	// and re-fire in a loop.
	from = time.Date(2026, 8, 6, 3, 0, 0, 0, time.UTC)
	if got := s.Next(from); !got.After(from) {
		t.Errorf("Next(03:00) = %v, must be strictly later", got)
	}

	if s.Display != "0 3 * * *" {
		t.Errorf("Display = %q", s.Display)
	}
}

func TestIntervalSchedule(t *testing.T) {
	s := EveryInterval(5 * time.Minute)
	from := time.Date(2026, 8, 6, 1, 0, 0, 0, time.UTC)
	if got := s.Next(from); !got.Equal(from.Add(5 * time.Minute)) {
		t.Errorf("Next = %v", got)
	}
}

func TestRunNowExecutesAndRecordsStatus(t *testing.T) {
	m := New(discard())
	calls := 0
	m.Register(Job{
		Name:     "job-a",
		Schedule: DailyAt(3, 0),
		Run: func(context.Context) error {
			calls++

			return nil
		},
	})

	if err := m.RunNow(t.Context(), "job-a"); err != nil {
		t.Fatalf("RunNow: %v", err)
	}

	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}

	st := m.Statuses()
	if len(st) != 1 || st[0].Name != "job-a" {
		t.Fatalf("statuses = %+v", st)
	}

	if st[0].LastRunAt == nil {
		t.Error("LastRunAt must be recorded")
	}

	if st[0].LastError != "" {
		t.Errorf("LastError = %q", st[0].LastError)
	}

	if st[0].Running {
		t.Error("job must not still be marked running")
	}
}

func TestRunNowSurfacesAndClearsErrors(t *testing.T) {
	m := New(discard())
	fail := true
	m.Register(Job{
		Name:     "flaky",
		Schedule: DailyAt(3, 0),
		Run: func(context.Context) error {
			if fail {
				return errors.New("boom")
			}

			return nil
		},
	})

	if err := m.RunNow(t.Context(), "flaky"); err == nil {
		t.Error("a failing job must return its error")
	}

	if got := m.Statuses()[0].LastError; got != "boom" {
		t.Errorf("LastError = %q", got)
	}

	// A later success must clear the stale error, otherwise the admin
	// view shows a permanent red mark on a healthy job.
	fail = false
	if err := m.RunNow(t.Context(), "flaky"); err != nil {
		t.Fatalf("second run: %v", err)
	}

	if got := m.Statuses()[0].LastError; got != "" {
		t.Errorf("LastError after success = %q, want empty", got)
	}
}

func TestPanicInJobIsContained(t *testing.T) {
	m := New(discard())
	m.Register(Job{
		Name:     "panicky",
		Schedule: DailyAt(3, 0),
		Run:      func(context.Context) error { panic("kaboom") },
	})

	err := m.RunNow(t.Context(), "panicky")
	if err == nil {
		t.Fatal("a panicking job must surface as an error, not take the process down")
	}

	if got := m.Statuses()[0].LastError; got == "" {
		t.Error("panic must be recorded as the last error")
	}

	if m.Statuses()[0].Running {
		t.Error("running flag must be cleared after a panic")
	}
}

func TestRunNowRejectsUnknownJob(t *testing.T) {
	m := New(discard())
	if err := m.RunNow(t.Context(), "nope"); err == nil {
		t.Error("unknown job must error")
	}
}

func TestRegisterIgnoresDuplicates(t *testing.T) {
	m := New(discard())
	j := Job{Name: "dup", Schedule: DailyAt(1, 0), Run: func(context.Context) error { return nil }}
	m.Register(j)
	m.Register(j)
	if got := len(m.Statuses()); got != 1 {
		t.Errorf("statuses = %d, want 1", got)
	}
}
