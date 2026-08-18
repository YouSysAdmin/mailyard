// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package cron runs the platform's scheduled maintenance jobs.
//
// The schedule vocabulary is deliberately two shapes - "daily at
// HH:MM UTC" and "every N" - rather than full cron expressions. That
// covers every job the platform actually has and keeps the dependency
// list unchanged. Schedules carry a cron-syntax Display string so the
// admin API can show operators something familiar.
//
// Jobs are expected to be idempotent and safe to skip: a node that is
// down at 03:00 does not backfill, it simply runs at 03:00 the next
// day. On a multi-node deployment every node runs every job, so jobs
// must tolerate concurrent execution - the built-in ones are all
// delete-by-age statements, which are naturally convergent.
package cron

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Schedule decides when a job runs next.
type Schedule struct {
	// Daily, when set, runs the job once a day at this UTC time.
	Daily *TimeOfDay

	// Every, when set, runs the job on a fixed interval.
	Every time.Duration

	// Display is the human-facing form, cron syntax where it maps.
	Display string
}

// TimeOfDay is a UTC wall-clock time.
type TimeOfDay struct {
	Hour   int
	Minute int
}

// DailyAt builds a schedule that fires once a day at hour:minute UTC.
func DailyAt(hour, minute int) Schedule {
	return Schedule{
		Daily:   &TimeOfDay{Hour: hour, Minute: minute},
		Display: fmt.Sprintf("%d %d * * *", minute, hour),
	}
}

// EveryInterval builds a fixed-interval schedule.
func EveryInterval(d time.Duration) Schedule {
	return Schedule{Every: d, Display: "every " + d.String()}
}

// Next returns the first firing strictly after from.
func (s Schedule) Next(from time.Time) time.Time {
	if s.Every > 0 {
		return from.Add(s.Every)
	}

	if s.Daily == nil {
		// A schedule with neither shape never fires. Park it far out
		// rather than spinning.
		return from.Add(24 * time.Hour)
	}

	from = from.UTC()
	next := time.Date(from.Year(), from.Month(), from.Day(),
		s.Daily.Hour, s.Daily.Minute, 0, 0, time.UTC)
	if !next.After(from) {
		next = next.AddDate(0, 0, 1)
	}

	return next
}

// Job is one scheduled unit of work.
type Job struct {
	Name     string
	Schedule Schedule

	// Run does the work. It should be idempotent.
	Run func(ctx context.Context) error
}

// Status is the admin-facing view of a job.
type Status struct {
	Name      string     `json:"name"`
	Schedule  string     `json:"schedule"`
	Running   bool       `json:"running"`
	LastRunAt *time.Time `json:"last_run_at"`
	LastError string     `json:"last_error"`
	NextRunAt *time.Time `json:"next_run_at"`

	// LastDuration is how long the previous run took, in
	// milliseconds.
	LastDurationMS int64 `json:"last_duration_ms"`
}

type jobState struct {
	job       Job
	running   bool
	lastRun   *time.Time
	lastErr   string
	nextRun   time.Time
	lastTaken time.Duration
}

// Manager owns the job set and the ticker driving it.
type Manager struct {
	log *slog.Logger

	mu   sync.RWMutex
	jobs map[string]*jobState

	// order preserves registration order for a stable listing.
	order []string

	stopped chan struct{}
	once    sync.Once
}

// New builds an empty manager.
func New(log *slog.Logger) *Manager {
	return &Manager{
		log:     log,
		jobs:    map[string]*jobState{},
		stopped: make(chan struct{}),
	}
}

// Register adds a job. Call before Start.
func (m *Manager) Register(j Job) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.jobs[j.Name]; exists {
		return
	}

	m.jobs[j.Name] = &jobState{job: j, nextRun: j.Schedule.Next(time.Now().UTC())}
	m.order = append(m.order, j.Name)
}

// Start runs the scheduler until ctx is cancelled. The tick is coarse
// (30s) because nothing registered here is finer than hourly - firing a
// few seconds late is irrelevant, and a slow tick keeps an idle process
// genuinely idle.
func (m *Manager) Start(ctx context.Context) {
	defer m.once.Do(func() { close(m.stopped) })

	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	m.log.Info("cron: started", "jobs", len(m.jobs))

	for {
		select {
		case <-ctx.Done():
			m.log.Info("cron: stopped")

			return
		case <-t.C:
			m.runDue(ctx)
		}
	}
}

// runDue fires every job whose time has come. Each runs in its own
// goroutine so a slow job does not hold up the others.
func (m *Manager) runDue(ctx context.Context) {
	now := time.Now().UTC()

	m.mu.Lock()
	var due []*jobState
	for _, name := range m.order {
		st := m.jobs[name]
		if st.running || now.Before(st.nextRun) {
			continue
		}

		st.running = true
		// Advance the clock immediately so a job that outlives its
		// own interval is not queued up again the moment it returns.
		st.nextRun = st.job.Schedule.Next(now)
		due = append(due, st)
	}

	m.mu.Unlock()

	for _, st := range due {
		// The return value is for RunNow. Here the outcome is already
		// logged and recorded on the job state by execute itself.
		go func() { _ = m.execute(ctx, st) }()
	}
}

// RunNow executes one job out of band, for the admin trigger. It
// reports an error when the job is unknown or already in flight.
func (m *Manager) RunNow(ctx context.Context, name string) error {
	m.mu.Lock()
	st, ok := m.jobs[name]
	if !ok {
		m.mu.Unlock()

		return fmt.Errorf("unknown job %q", name)
	}

	if st.running {
		m.mu.Unlock()

		return fmt.Errorf("job %q is already running", name)
	}

	st.running = true
	m.mu.Unlock()

	// execute's own return, not a re-read of st.lastErr: the moment
	// execute unlocks, the scheduler may start the same job again and
	// overwrite lastErr - so the re-read could report the OTHER run's
	// outcome to the admin who pressed the button.
	return m.execute(ctx, st)
}

// execute runs one job and returns what the job returned, with a panic
// folded in as an error.
func (m *Manager) execute(ctx context.Context, st *jobState) error {
	name := st.job.Name
	started := time.Now().UTC()
	m.log.Info("cron: job start", "job", name)

	err := func() (err error) {
		// A panicking job must not take the process with it.
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic: %v", r)
			}
		}()

		return st.job.Run(ctx)
	}()

	finished := time.Now().UTC()
	m.mu.Lock()
	st.running = false
	st.lastRun = &started
	st.lastTaken = finished.Sub(started)
	if err != nil {
		st.lastErr = err.Error()
	} else {
		st.lastErr = ""
	}

	m.mu.Unlock()

	if err != nil {
		m.log.Error("cron: job failed", "job", name, "err", err, "took", finished.Sub(started))

		return err
	}

	m.log.Info("cron: job done", "job", name, "took", finished.Sub(started))

	return nil
}

// Statuses returns the admin view, in registration order.
func (m *Manager) Statuses() []Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Status, 0, len(m.order))
	for _, name := range m.order {
		st := m.jobs[name]
		next := st.nextRun
		out = append(out, Status{
			Name:           name,
			Schedule:       st.job.Schedule.Display,
			Running:        st.running,
			LastRunAt:      st.lastRun,
			LastError:      st.lastErr,
			NextRunAt:      &next,
			LastDurationMS: st.lastTaken.Milliseconds(),
		})
	}

	return out
}

// Wait blocks until the scheduler loop has exited.
func (m *Manager) Wait(timeout time.Duration) {
	select {
	case <-m.stopped:
	case <-time.After(timeout):
	}
}
