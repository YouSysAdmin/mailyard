// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package partition keeps the daily partitions of the emails table
// ahead of the writes, and removes the ones retention has finished
// with.
//
// DAILY, not weekly, for retention rather than performance: a partition
// can only be dropped once its whole range is past the cutoff, so a
// weekly one held up to six extra days and then removed a week in a
// lump. The cost is seven times the partitions - see maxPartitions.
//
// Mixed widths coexist. Names are dates, Postgres allows range
// partitions of differing width as long as they do not overlap, and
// DropSpent reads each bound from the catalog, so weekly partitions an
// installation already has age out in place.
//
// Partitioning buys DROP TABLE instead of a DELETE over millions of
// rows, and costs a new way for an INSERT to fail: a row whose
// created_at falls outside every partition has nowhere to go. Two
// guards against that. The job creates two weeks ahead, so one that
// stops running is noticed long before it matters, and the DEFAULT
// partition catches the rest - a backstop only, since Postgres will not
// attach a partition whose range claims rows the default already holds.
// EnsureAhead reports a non-empty default as an error.
package partition

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"regexp"
	"time"

	"github.com/yousysadmin/mailyard/internal/database"
)

// Table is the only partitioned table. Named as a constant rather
// than taken as a parameter because every query below has to be a
// constant string for TestNoDynamicSQL, and because a second
// partitioned table would want its own decisions about width and
// retention anyway.
const Table = "emails"

// defaultPartition catches rows no dated partition claims.
const defaultPartition = "emails_default"

// daysAhead is how far in front of now partitions are kept.
//
// Fourteen. The job runs hourly and almost every run is a no-op, but the
// headroom is what a node being down eats into, and the cost of a
// partition that turns out to be empty is one entry in the catalog. Two
// weeks is the same proportion of margin the weekly version kept with
// four weeks, at the same catalog cost.
const daysAhead = 14

// maxPartitions is the ceiling that makes daily partitioning safe when
// nothing is dropping them.
//
// retention_days defaults to 30, which settles near 45 partitions. But 0
// means keep forever: 365 new partitions a year with nothing removing
// any, and the cost lands on the claim query, which carries no date
// predicate and so can never be pruned.
//
// At 730 partitions against 105, on 2M rows: planning 94-420ms rather
// than 2ms, and 2194 relation locks per claim rather than 319. The locks
// are what breaks rather than slows - the lock table is shared across
// the database, and 730 partitions fail at 16 concurrent claims where
// 380 survive 24. The failure is "out of shared memory" on the claim,
// so the delivery queue stops.
//
// 400 sits just above the largest legitimate setting: a year of
// retention settles near 380. Only retention_days = 0 climbs past it.
//
// It cannot DROP anything to stay under - see the alarm in EnsureAhead.
// The remedies are the operator's: a retention window, or a higher
// max_locks_per_transaction.
const maxPartitions = 400

// nearCeiling is where the warning starts, at eighty percent of the
// ceiling. Not a second constant, because the ceiling is overridable
// per Maintainer and two independent numbers would drift apart.
func nearCeiling(ceiling int) int { return ceiling * 8 / 10 }

// ceilingRemedy is what an operator can actually do about it, carried
// on both the warning and the error so the log line is self-contained.
const ceilingRemedy = "set retention_days to a non-zero window, or raise max_locks_per_transaction - " +
	"at 0 this grows by 365 a year, and measured at 730 partitions sixteen concurrent " +
	"queue claims failed with out of shared memory"

// Health is what the partition count looks like right now: how many
// there are and how many this installation tolerates.
//
// A struct rather than two returns because every caller wants both -
// the number means nothing without the ceiling it is measured against,
// and the ceiling is overridable per Maintainer.
type Health struct {
	Partitions int
	Ceiling    int
}

// NearCeiling reports whether the count has reached the point where an
// operator still has months to act. Over reports the point where they
// no longer do.
func (h Health) NearCeiling() bool { return h.Partitions >= nearCeiling(h.Ceiling) }

// Over reports that the ceiling itself has been reached.
func (h Health) Over() bool { return h.Partitions >= h.Ceiling }

// Maintainer creates upcoming partitions and drops spent ones.
type Maintainer struct {
	DB  *sql.DB
	Log *slog.Logger

	// Ceiling overrides maxPartitions. Zero means the constant.
	//
	// A field only so the alarm can be exercised: a test that had to
	// create four hundred partitions to reach it would take longer than
	// the rest of the package put together, and safety code nothing
	// presses the button on is a claim rather than a guard.
	Ceiling int
}

func (m *Maintainer) ceiling() int {
	if m.Ceiling > 0 {
		return m.Ceiling
	}

	return maxPartitions
}

// EnsureAhead creates any missing daily partition from today through
// daysAhead, and reports rows found in the default.
//
// Idempotent, and safe to run concurrently on several nodes: CREATE
// TABLE IF NOT EXISTS races cleanly, and two nodes creating the same
// partition is not a conflict worth coordinating over.
func (m *Maintainer) EnsureAhead(ctx context.Context) (created int, err error) {
	// What already exists, so a day covered by a WEEKLY partition from
	// before the cutover is skipped rather than attempted. Postgres
	// refuses an overlapping range, and that refusal would stop the job
	// dead for as long as the last weekly partition still covers today.
	covered, err := m.rangePartitions(ctx)
	if err != nil {
		return 0, err
	}

	start := dayStart(time.Now().UTC())
	for i := 0; i <= daysAhead; i++ {
		from := start.AddDate(0, 0, i)
		to := from.AddDate(0, 0, 1)

		if coveredBy(covered, from) {
			continue
		}

		made, cerr := m.ensureDay(ctx, from, to)
		if cerr != nil {
			return created, cerr
		}

		if made {
			created++
			m.Log.Info("partition created", "table", partitionName(from),
				"from", from.Format(time.DateOnly), "to", to.Format(time.DateOnly))
		}
	}

	// The ceiling, checked on the count this run leaves behind.
	//
	// It cannot DROP anything, and that is deliberate rather than
	// unfinished: the only way to reach this number is retention_days = 0,
	// which is the operator saying keep everything forever. Removing their
	// history to protect a lock table would be answering a question nobody
	// asked. So it is an alarm with the remedy in it.
	//
	// At error, because the failure it predicts is the delivery queue
	// stopping - see maxPartitions for the measurements - and because the
	// gap between noticing and breaking is months, which is plenty of
	// warning if anybody reads it.
	//
	// TWO levels, because one is not useful. At the ceiling the runway
	// is already spent, and the remedy - choosing a retention window,
	// or raising max_locks_per_transaction, which needs a restart - is
	// not something anybody does the same afternoon. The warning at
	// NearCeiling is the one that can still be acted on calmly: at
	// 365 partitions a year it arrives roughly three months ahead.
	if n := len(covered) + created; n >= m.ceiling() {
		m.Log.Error("the emails table has too many partitions", "partitions", n,
			"ceiling", m.ceiling(), "remedy", ceilingRemedy)
	} else if n >= nearCeiling(m.ceiling()) {
		m.Log.Warn("the emails table is approaching its partition ceiling", "partitions", n,
			"ceiling", m.ceiling(), "remedy", ceilingRemedy)
	}

	var stranded int64
	// Constant query, no interpolation - the default partition has a
	// fixed name.
	if err := m.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM emails_default`).Scan(&stranded); err != nil {
		return created, fmt.Errorf("count default partition: %w", err)
	}

	if stranded > 0 {
		// An error, not a warning. Every one of these rows blocks the
		// creation of the partition its week belongs to, and nothing
		// here can fix that safely on its own - moving rows between
		// partitions rewrites them under a lock, which is not a thing
		// a maintenance job should decide to do unattended.
		return created, fmt.Errorf(
			"%d row(s) are in %s - the partitions covering them cannot be created "+
				"until they are moved, so partition maintenance is stuck", stranded, defaultPartition)
	}

	return created, nil
}

func (m *Maintainer) ensureDay(ctx context.Context, from, to time.Time) (bool, error) {
	name := partitionName(from)
	var exists bool
	// to_regclass, again through the search_path, for the same reason
	// as rangePartitions: relname alone is not unique across schemas.
	if err := m.DB.QueryRowContext(ctx,
		`SELECT to_regclass($1) IS NOT NULL`, name).Scan(&exists); err != nil {
		return false, fmt.Errorf("check partition %s: %w", name, err)
	}

	if exists {
		return false, nil
	}

	// The one place a table name is interpolated. It is not runtime
	// data: partitionName builds it from a time.Time with a fixed
	// layout, and mustBeSafeName below refuses anything that is not
	// exactly that shape - so there is no input, from any caller, that
	// could reach this string.
	if err := mustBeSafeName(name); err != nil {
		return false, err
	}

	stmt := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES FROM ('%s') TO ('%s')`,
		name, Table, from.Format(time.DateOnly), to.Format(time.DateOnly))
	//sqlconst:allow the table name is derived from a time, and validated by mustBeSafeName
	if _, err := m.DB.ExecContext(ctx, stmt); err != nil {
		return false, fmt.Errorf("create partition %s: %w", name, err)
	}

	return true, nil
}

// Count reports the current partition health.
//
// Read from the catalog on every call rather than cached from the last
// EnsureAhead: a metric scrape and an alert check both want the number
// NOW, and the maintainer only runs hourly. It is one catalog query.
func (m *Maintainer) Count(ctx context.Context) (Health, error) {
	parts, err := m.rangePartitions(ctx)
	if err != nil {
		return Health{}, err
	}

	return Health{Partitions: len(parts), Ceiling: m.ceiling()}, nil
}

// DropSpent removes every partition whose whole range is older than
// before AND which holds no in-flight rows, returning the names it
// dropped.
//
// The in-flight check is not a nicety. A message scheduled for next
// month can have been CREATED months ago, so its row lives in an old
// partition - and retention has always exempted queued, scheduled and
// processing rows however old they are. Dropping the partition would
// take that message with it, which the row-by-row DELETE it replaces
// would never have done.
//
// Whatever this leaves behind is still handled: the caller falls back
// to the ordinary DELETE for those partitions, which removes the
// finished rows and leaves the in-flight ones exactly as before.
func (m *Maintainer) DropSpent(ctx context.Context, before time.Time) ([]string, error) {
	parts, err := m.rangePartitions(ctx)
	if err != nil {
		return nil, err
	}

	var dropped []string
	for _, p := range parts {
		if !p.upper.Before(before) && !p.upper.Equal(before) {
			continue
		}

		if err := mustBeSafeName(p.name); err != nil {
			return dropped, err
		}

		ok, err := m.dropOne(ctx, p.name)
		if err != nil {
			return dropped, err
		}

		if !ok {
			continue
		}

		dropped = append(dropped, p.name)
		m.Log.Info("partition dropped", "table", p.name, "upper_bound", p.upper.Format(time.DateOnly))
	}

	return dropped, nil
}

// dropOne checks and drops one partition inside a single transaction
// holding an ACCESS EXCLUSIVE lock, reporting whether the drop
// happened.
//
// One transaction, because as two independent statements a row could
// return to the queue between the check and the DROP - a console Reset
// of an old failed message, an insert carrying a caller-set old
// created_at - and the DROP took it along, silently cancelling a send.
// The row-by-row DELETE this path replaced evaluated its status
// predicate atomically, and the lock buys that property back.
func (m *Maintainer) dropOne(ctx context.Context, name string) (bool, error) {
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	// Bounded, so the sweep never parks behind somebody's long
	// transaction - a partition that cannot be locked promptly is
	// simply kept until the next run.
	if _, err := tx.ExecContext(ctx, `SET LOCAL lock_timeout = '5s'`); err != nil {
		return false, err
	}

	//sqlconst:allow the name comes from pg_class, and is validated by mustBeSafeName
	if _, err := tx.ExecContext(ctx, `LOCK TABLE `+name+` IN ACCESS EXCLUSIVE MODE`); err != nil {
		switch database.SQLState(err) {
		case "42P01":
			// Undefined table: another node's sweep won the race, which
			// is the outcome this one wanted. Every worker runs this job
			// on the same schedule, so converging silently here is what
			// DROP IF EXISTS did for the statement this replaces.
			return false, nil
		case "55P03", "57014":
			// The lock timed out (lock_not_available, or the timeout
			// cancelling the statement). Kept for this sweep, the next
			// one retries.
			m.Log.Info("partition kept, could not lock it promptly", "table", name)

			return false, nil
		}

		return false, fmt.Errorf("lock partition %s: %w", name, err)
	}

	busy, err := m.hasInFlight(ctx, tx, name)
	if err != nil {
		return false, err
	}

	if busy {
		m.Log.Info("partition kept, it still holds in-flight mail", "table", name)

		return false, nil
	}

	//sqlconst:allow the name comes from pg_class, and is validated by mustBeSafeName
	if _, err := tx.ExecContext(ctx, `DROP TABLE `+name); err != nil {
		return false, fmt.Errorf("drop partition %s: %w", name, err)
	}

	return true, tx.Commit()
}

type rangePartition struct {
	name  string
	lower time.Time
	upper time.Time
}

// boundsRE pulls both ends of a RANGE partition out of its bound
// expression.
//
// There is no catalog column holding them - pg_class.relpartbound is an
// internal node tree, and pg_get_expr renders it as
// `FOR VALUES FROM ('...') TO ('...')`. Reading them out of that text is
// the standard way, and it is stable because the renderer is.
//
// Both ends, since the changeover to daily partitions: DropSpent needs
// only the upper one, but EnsureAhead has to know whether a day is
// already inside a WEEKLY partition left over from before, and that is a
// question about where the range starts.
var boundsRE = regexp.MustCompile(`FROM \('([^']+)'\) TO \('([^']+)'\)`)

func (m *Maintainer) rangePartitions(ctx context.Context) ([]rangePartition, error) {
	// to_regclass resolves the parent through the search_path to one
	// oid. Joining on relname instead matched an emails table in every
	// schema that had one - which is every store test, since dbtest
	// gives each its own - and a partition from a schema this process
	// is not looking at is not this process's to drop.
	rows, err := m.DB.QueryContext(ctx, `
        SELECT c.relname, pg_get_expr(c.relpartbound, c.oid)
        FROM pg_class c
        JOIN pg_inherits i ON i.inhrelid = c.oid
        WHERE i.inhparent = to_regclass($1)::oid
          AND c.relname <> $2
          AND c.relpartbound IS NOT NULL`, Table, defaultPartition)
	if err != nil {
		return nil, fmt.Errorf("list partitions: %w", err)
	}

	defer func() { _ = rows.Close() }()

	var out []rangePartition
	for rows.Next() {
		var name, bound string
		if err := rows.Scan(&name, &bound); err != nil {
			return nil, err
		}

		match := boundsRE.FindStringSubmatch(bound)
		if match == nil {
			// Not a range partition we understand. Skipping is the
			// safe direction: the worst outcome is a partition that
			// retention deletes row by row, which is what it did
			// before any of this existed.
			m.Log.Warn("partition bound not understood, leaving it alone",
				"table", name, "bound", bound)
			continue
		}

		lower, lerr := parseBound(match[1])
		upper, perr := parseBound(match[2])
		if lerr != nil || perr != nil {
			m.Log.Warn("partition bound not parseable, leaving it alone",
				"table", name, "from", match[1], "to", match[2],
				"from_err", lerr, "to_err", perr)
			continue
		}

		out = append(out, rangePartition{name: name, lower: lower, upper: upper})
	}

	return out, rows.Err()
}

func (m *Maintainer) hasInFlight(ctx context.Context, q queryRower, name string) (bool, error) {
	if err := mustBeSafeName(name); err != nil {
		return false, err
	}

	var n int
	//sqlconst:allow the name comes from pg_class, and is validated by mustBeSafeName
	err := q.QueryRowContext(ctx,
		`SELECT count(*) FROM `+name+
			` WHERE status IN ('queued', 'scheduled', 'processing')`).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("count in-flight in %s: %w", name, err)
	}

	return n > 0, nil
}

// queryRower is the slice of *sql.DB and *sql.Tx hasInFlight needs, so
// the sweep can ask inside the transaction that holds the lock.
type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// dayStart is midnight UTC of the day containing t, which is what a
// DATE bound means to Postgres and therefore what the partition ranges
// are cut on.
func dayStart(t time.Time) time.Time {
	return t.UTC().Truncate(24 * time.Hour)
}

// coveredBy reports whether an existing partition already claims the
// given day: lower <= day < upper.
//
// This is what lets weekly and daily partitions live in one table
// through the changeover. Without it EnsureAhead would ask Postgres to
// create a day that a leftover WEEKLY partition still covers, Postgres
// would refuse the overlap, and the job would stay broken for as long as
// that week ran - which is the whole first week after an upgrade.
func coveredBy(existing []rangePartition, day time.Time) bool {
	for _, p := range existing {
		if !p.lower.After(day) && p.upper.After(day) {
			return true
		}
	}

	return false
}

// partitionName names a DAILY partition, and the prefix says which kind
// it is.
//
// `d` for day, where the weekly partitions this replaced used `w`. That
// is not decoration: the two live in one table through the changeover,
// both are named after the date their range STARTS, and with one prefix
// they were indistinguishable in a table listing. The first real run of
// this produced emails_w2026_08_10 - a week - sitting directly above
// emails_w2026_08_17 - a day - and nothing on the screen said so. Whoever
// next reasons about what a DROP takes away needs to know which it is.
func partitionName(from time.Time) string {
	return "emails_d" + from.UTC().Format("2006_01_02")
}

// safeName is the shape of a partition name this package will
// concatenate into a statement. Anything else does not get there.
//
// Both prefixes, because the names it validates come from two places:
// partitionName above, and pg_class - and the catalog still holds the
// weekly partitions laid down before the changeover, which DropSpent has
// to be able to name in order to remove them. It also holds any
// emails_wYYYY_MM_DD that a DAY was created under before the prefix
// changed; those work unchanged, since every decision about a partition
// is made from its bounds and never from its name.
var safeName = regexp.MustCompile(`^emails_[dw][0-9]{4}_[0-9]{2}_[0-9]{2}$`)

func mustBeSafeName(name string) error {
	if !safeName.MatchString(name) {
		return fmt.Errorf("refusing to use %q as a partition name: it is not of the form emails_dYYYY_MM_DD", name)
	}

	return nil
}

// parseBound reads the timestamp Postgres renders in a partition
// bound. The layout carries an offset that may or may not include
// minutes, so both are tried.
func parseBound(s string) (time.Time, error) {
	for _, layout := range []string{
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05-07",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}

	return time.Time{}, fmt.Errorf("unrecognised bound %q", s)
}
