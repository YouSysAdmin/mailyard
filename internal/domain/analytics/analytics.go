// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package analytics answers reporting questions that span tables:
// the project dashboard summary and the delivery trend.
//
// It owns its SQL rather than adding a Count method to eight domain
// stores for numbers only this page wants. Everything here is
// read-only aggregation, every query is scoped by project_id, and
// nothing in this package writes.
package analytics

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/yousysadmin/mailyard/internal/database"
	amodel "github.com/yousysadmin/mailyard/internal/models/analytics"
)

// Store persists the reporting reads. Project scoped: a method taking
// projID answers nothing for a row another project owns.
type Store struct {
	database.Base
}

// NewStore takes optional read replicas. Every query in this store
// reads them, and it is the only store where that is true: nothing
// here writes, and no request writes a row and then asks this package
// to aggregate it back. Whether the replicas arrive at all is decided
// by database.replica_reads.analytics.
func NewStore(db *sql.DB, replicas ...*sql.DB) *Store {
	return &Store{Base: database.NewBase(db, replicas...)}
}

// countBy runs a grouped count scoped to one project.
func (s *Store) countBy(ctx context.Context, query, projID string, args ...any) (map[string]int, error) {
	//sqlconst:allow query is a parameter, checked at each countBy call site
	//replicaread:allow every countBy call site in this file passes a constant grouped SELECT
	rows, err := s.ReadQuery(ctx, query, append([]any{projID}, args...)...)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	out := map[string]int{}
	for rows.Next() {
		var key string
		var n int
		if err := rows.Scan(&key, &n); err != nil {
			return nil, err
		}

		out[key] = n
	}

	return out, rows.Err()
}

func (s *Store) count(ctx context.Context, table, projID string, extra string, args ...any) (int, error) {
	// table is a package constant at every call site, never caller
	// input, so interpolating it cannot carry injection.
	query := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE project_id = ?`, table)
	if extra != "" {
		query += " AND " + extra
	}

	var n int

	//sqlconst:allow query is built from table and extra, both checked at each count call site
	//replicaread:allow the statement is a SELECT COUNT(*) built here, table and extra are constants at every call site
	err := s.ReadQueryRow(ctx, query, append([]any{projID}, args...)...).Scan(&n)

	return n, err
}

// Summary builds the dashboard readout in one call.
func (s *Store) Summary(ctx context.Context, projID string) (*amodel.Summary, error) {
	out := &amodel.Summary{}
	var err error

	out.Emails, err = s.countBy(ctx,
		`SELECT status, COUNT(*) FROM emails WHERE project_id = ? GROUP BY status`, projID)
	if err != nil {
		return nil, err
	}

	for _, n := range out.Emails {
		out.TotalEmails += n
	}

	finalized := out.Emails["sent"] + out.Emails["failed"]
	if finalized > 0 {
		out.FailureRate = float64(out.Emails["failed"]) / float64(finalized) * 100
	}

	out.Inbound, err = s.countBy(ctx, `SELECT status, COUNT(*) FROM inbound_emails WHERE project_id = ? GROUP BY status`, projID)
	if err != nil {
		return nil, err
	}

	if out.Engagement, err = s.engagement(ctx, projID); err != nil {
		return nil, err
	}

	res := map[string]int{}
	for key, spec := range map[string]struct {
		table string
		extra string
	}{
		"domains":           {"domains", ""},
		"verified_domains":  {"domains", "verified = TRUE"},
		"smtp_servers":      {"smtp_servers", ""},
		"api_keys":          {"api_keys", ""},
		"active_api_keys":   {"api_keys", "revoked = FALSE"},
		"smtp_credentials":  {"smtp_credentials", ""},
		"senders":           {"senders", ""},
		"templates":         {"templates", ""},
		"contacts":          {"contacts", ""},
		"subscribers":       {"subscribers", ""},
		"suppressions":      {"suppressions", ""},
		"bounces":           {"bounces", ""},
		"webhooks":          {"webhooks", ""},
		"campaigns":         {"campaigns", ""},
		"unsubscribe_lists": {"unsubscribe_lists", ""},
	} {
		//sqlconst:allow spec comes from the literal map directly above, never from a request
		n, err := s.count(ctx, spec.table, projID, spec.extra)
		if err != nil {
			return nil, err
		}

		res[key] = n
	}

	out.Resources = res

	return out, nil
}

// engagement counts opens and clicks against the mail that could
// register them.
//
// One pass with FILTER rather than three counts. `emails` is range
// partitioned by week and none of these predicates prunes a partition,
// so each count would be a separate walk of every live partition for a
// number that belongs beside the other two anyway.
//
// opened_at is set by the pixel AND by a click - some clients fetch no
// images at all, so a message somebody demonstrably read would otherwise
// count as unopened. That is decided in MarkClicked, and this query
// inherits it rather than restating it.
func (s *Store) engagement(ctx context.Context, projID string) (amodel.Engagement, error) {
	var e amodel.Engagement
	// Only SENT mail is the denominator. A queued or scheduled message
	// carries the pixel already and has had no chance to be opened, so
	// counting it would make the rate sag every time a batch is
	// accepted and recover as it drains.
	err := s.ReadQueryRow(ctx, `
        SELECT COUNT(*) FILTER (WHERE status = 'sent' AND tracked),
               COUNT(*) FILTER (WHERE opened_at IS NOT NULL),
               COUNT(*) FILTER (WHERE clicked_at IS NOT NULL)
        FROM emails WHERE project_id = ?`, projID).
		Scan(&e.TrackedSent, &e.Opened, &e.Clicked)
	if err != nil {
		return e, err
	}

	if e.TrackedSent > 0 {
		e.OpenRate = float64(e.Opened) / float64(e.TrackedSent) * 100
		e.ClickRate = float64(e.Clicked) / float64(e.TrackedSent) * 100
	}

	return e, nil
}

// DailyCounts buckets emails by calendar day over a range. status
// narrows to one delivery state when non-empty.
//
// Bucketing is done in SQL with to_char because pulling every row
// into Go to group it would scale with the send volume rather than
// with the number of days.
func (s *Store) DailyCounts(ctx context.Context, projID string, from, to time.Time, status string) ([]amodel.DayCount, error) {
	// From the rollup, not from the emails table. The same GROUP BY over
	// a fourteen-day window cost 434-698ms per status on 1.2M rows, twice
	// per dashboard load - see migration 00069 for why this is
	// recomputed rather than incremented, and what that costs in
	// freshness.
	query := `
        SELECT to_char(day, 'YYYY-MM-DD') AS day, SUM(n) FROM email_daily
        WHERE project_id = ? AND day >= ?::date AND day <= ?::date`
	args := []any{projID, from, to}
	if status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}

	query += ` GROUP BY day ORDER BY day ASC`

	rows, err := s.ReadQuery(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	seen := map[string]int{}
	for rows.Next() {
		// NullString rather than string: a row whose timestamp the
		// engine cannot format yields NULL, and one such row must not
		// fail the whole report.
		var day sql.NullString
		var n int
		if err := rows.Scan(&day, &n); err != nil {
			return nil, err
		}

		if day.Valid {
			seen[day.String] = n
		}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Fill the gaps. A chart with missing days silently rescales its
	// x-axis and makes a quiet week look like a busy one.
	//
	// Iterated over dates in UTC, the same unit the rollup is bucketed
	// in. Walking timestamps and stopping before `to` is right for the
	// handler, which passes tomorrow midnight, but drops the last partial
	// day for anyone passing an instant - the data sits in the map and is
	// never emitted.
	day := func(t time.Time) time.Time {
		t = t.UTC()

		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	}

	last := day(to)
	// An exclusive upper bound at midnight names the day before it: the
	// handler asks for [from, tomorrow-midnight) and means through today.
	if to.Equal(last) {
		last = last.AddDate(0, 0, -1)
	}

	var out []amodel.DayCount
	for d := day(from); !d.After(last); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		out = append(out, amodel.DayCount{Date: key, Count: seen[key]})
	}

	return out, nil
}

// StatusBreakdown counts emails by status over the same range.
func (s *Store) StatusBreakdown(ctx context.Context, projID string, from, to time.Time) (map[string]int, error) {
	return s.countBy(ctx, `
        SELECT status, COUNT(*) FROM emails
        WHERE project_id = ? AND created_at >= ? AND created_at < ?
        GROUP BY status`, projID, from, to)
}
