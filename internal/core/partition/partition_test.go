// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package partition

import (
	"database/sql"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/database/dbtest"
)

func testMaintainer(t *testing.T) (*Maintainer, *sql.DB) {
	t.Helper()
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)

	return &Maintainer{DB: db, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, db
}

func newProject(t *testing.T, db *sql.DB) string {
	t.Helper()
	id := ids.New()
	_, err := db.ExecContext(t.Context(), `
		INSERT INTO projects (id, name, slug, owner_id, created_at, updated_at)
		VALUES ($1, 'test', $2, NULL, now(), now())`, id, id)
	if err != nil {
		t.Fatalf("insert project: %v", err)
	}

	return id
}

func insertEmail(t *testing.T, db *sql.DB, projID, status string, createdAt time.Time) string {
	t.Helper()
	id := ids.New()
	_, err := db.ExecContext(t.Context(), `
		INSERT INTO emails (id, project_id, sender, recipients, subject, status, created_at)
		VALUES ($1, $2, 'a@b.invalid', '["c@d.invalid"]', 's', $3, $4)`,
		id, projID, status, createdAt)
	if err != nil {
		t.Fatalf("insert email at %s: %v", createdAt, err)
	}

	return id
}

func partitionCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	// Scoped to this test's schema. pg_class holds every schema's
	// tables, so matching on relname alone counted the partitions of
	// every other store test running at the same time - and the count
	// then moved between two calls in this test for reasons that had
	// nothing to do with the maintainer. It failed once in a full
	// parallel run and passed every time the package was run alone,
	// which is the worst shape a test failure can have.
	err := db.QueryRowContext(t.Context(), `
		SELECT count(*) FROM pg_inherits i
		JOIN pg_class p ON p.oid = i.inhparent
		WHERE p.relname = 'emails'
		  AND p.relnamespace = current_schema()::regnamespace`).Scan(&n)
	if err != nil {
		t.Fatalf("count partitions: %v", err)
	}

	return n
}

// dayStart has to agree with Postgres's date_trunc('day'), because the
// partition bounds are DATEs and the job compares against them. Disagree
// and it creates ranges that overlap the existing ones, and every CREATE
// fails.
//
// This replaced a weekStart that had to match date_trunc('week') - a
// harder thing to get right, since Go numbers Sunday 0 and Postgres
// starts its week on Monday. Cutting on days removes that trap entirely.
func TestDayStartMatchesPostgres(t *testing.T) {
	_, db := testMaintainer(t)
	for _, day := range []string{"2026-08-02", "2026-08-03", "2026-08-06", "2026-01-01"} {
		at, err := time.Parse(time.DateOnly, day)
		if err != nil {
			t.Fatal(err)
		}

		var pg time.Time
		if err := db.QueryRowContext(t.Context(),
			`SELECT date_trunc('day', $1::timestamptz)`, at).Scan(&pg); err != nil {
			t.Fatalf("date_trunc: %v", err)
		}

		got := dayStart(at)
		if !got.Equal(pg.UTC()) {
			t.Errorf("%s: dayStart gave %s, Postgres gave %s",
				day, got.Format(time.DateOnly), pg.UTC().Format(time.DateOnly))
		}
	}
}

// A second run creates nothing. That is the property worth having: the
// job fires hourly and almost every run has to be a no-op.
//
// The first run is no longer a no-op, and that is the changeover: the
// migration lays down weekly partitions, so the first daily pass fills in
// whatever days those weeks do not already cover.
func TestEnsureAheadIsIdempotent(t *testing.T) {
	m, db := testMaintainer(t)

	if _, err := m.EnsureAhead(t.Context()); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	settled := partitionCount(t, db)

	created, err := m.EnsureAhead(t.Context())
	if err != nil {
		t.Fatalf("second ensure: %v", err)
	}

	if created != 0 {
		t.Errorf("a second run created %d partitions, want none", created)
	}

	if got := partitionCount(t, db); got != settled {
		t.Errorf("a second run moved the partition count from %d to %d", settled, got)
	}
}

// The changeover itself: weekly partitions from before the switch and
// daily ones after it live in the same table, and nothing overlaps.
//
// Without coveredBy this is where the job would break - Postgres refuses
// a range that overlaps an existing partition, so every run would fail
// for as long as the last weekly partition still covered today, which is
// the whole first week after an upgrade.
func TestDailyPartitionsLandBesideTheWeeklyOnes(t *testing.T) {
	m, db := testMaintainer(t)

	weekly := partitionCount(t, db)
	if weekly == 0 {
		t.Fatal("the migration laid down no partitions, so this proves nothing")
	}

	if _, err := m.EnsureAhead(t.Context()); err != nil {
		t.Fatalf("ensure over weekly partitions: %v", err)
	}

	// Every day from today forward has somewhere to go, whichever kind of
	// partition claims it.
	proj := newProject(t, db)
	for i := 0; i <= daysAhead; i++ {
		at := dayStart(time.Now().UTC()).AddDate(0, 0, i).Add(9 * time.Hour)
		insertEmail(t, db, proj, "sent", at)
	}

	var stranded int
	if err := db.QueryRowContext(t.Context(),
		`SELECT count(*) FROM emails_default`).Scan(&stranded); err != nil {
		t.Fatalf("count default: %v", err)
	}

	if stranded != 0 {
		t.Errorf("%d row(s) fell into the default partition", stranded)
	}
}

// The gap the job exists to close: drop the far partition and check it
// puts it back.
func TestEnsureAheadRecreatesMissingDays(t *testing.T) {
	m, db := testMaintainer(t)
	if _, err := m.EnsureAhead(t.Context()); err != nil {
		t.Fatalf("settle: %v", err)
	}

	ahead := dayStart(time.Now().UTC()).AddDate(0, 0, daysAhead)
	name := partitionName(ahead)

	//sqlconst:allow the name comes from partitionName, not from any input
	if _, err := db.ExecContext(t.Context(), `DROP TABLE `+name); err != nil {
		t.Fatalf("drop %s: %v", name, err)
	}

	created, err := m.EnsureAhead(t.Context())
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}

	if created != 1 {
		t.Errorf("created %d partitions, want 1", created)
	}

	// And a row for that day now has somewhere to go.
	proj := newProject(t, db)
	insertEmail(t, db, proj, "sent", ahead.Add(9*time.Hour))
}

// A row in the default partition blocks the creation of the partition
// covering it, forever, so the job has to report it rather than carry on
// looking successful.
func TestRowsInTheDefaultPartitionAreAnError(t *testing.T) {
	m, db := testMaintainer(t)
	proj := newProject(t, db)
	// Well past the four weeks the migration laid down.
	insertEmail(t, db, proj, "sent", time.Now().UTC().AddDate(0, 0, 90))

	_, err := m.EnsureAhead(t.Context())
	if err == nil {
		t.Fatal("a stranded row in the default partition was reported as success")
	}

	if got := err.Error(); !strings.Contains(got, "emails_default") {
		t.Errorf("the error does not name the default partition: %s", got)
	}
}

// The point of the whole exercise.
func TestDropSpentRemovesWholeSpentPartitions(t *testing.T) {
	m, db := testMaintainer(t)
	proj := newProject(t, db)

	old := dayStart(time.Now().UTC()).AddDate(0, 0, -21)
	if _, err := m.ensureDay(t.Context(), old, old.AddDate(0, 0, 7)); err != nil {
		t.Fatalf("create old partition: %v", err)
	}

	insertEmail(t, db, proj, "sent", old.Add(time.Hour))

	// A cutoff after that whole week, but before this one.
	dropped, err := m.DropSpent(t.Context(), old.AddDate(0, 0, 7))
	if err != nil {
		t.Fatalf("drop: %v", err)
	}

	if len(dropped) != 1 || dropped[0] != partitionName(old) {
		t.Fatalf("dropped %v, want just %s", dropped, partitionName(old))
	}

	var n int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM emails`).Scan(&n); err != nil {
		t.Fatal(err)
	}

	if n != 0 {
		t.Errorf("%d rows survived the partition drop", n)
	}
}

// The rule the row-by-row DELETE always had and a partition drop
// would otherwise lose: a message scheduled for next month can have
// been CREATED months ago, so its row sits in an old partition.
// Dropping that partition would silently cancel the send.
func TestDropSpentKeepsAPartitionHoldingScheduledMail(t *testing.T) {
	m, db := testMaintainer(t)
	proj := newProject(t, db)

	old := dayStart(time.Now().UTC()).AddDate(0, 0, -21)
	if _, err := m.ensureDay(t.Context(), old, old.AddDate(0, 0, 7)); err != nil {
		t.Fatalf("create old partition: %v", err)
	}

	insertEmail(t, db, proj, "sent", old.Add(time.Hour))
	kept := insertEmail(t, db, proj, "scheduled", old.Add(2*time.Hour))

	dropped, err := m.DropSpent(t.Context(), old.AddDate(0, 0, 7))
	if err != nil {
		t.Fatalf("drop: %v", err)
	}

	if len(dropped) != 0 {
		t.Fatalf("dropped %v, which would have taken a scheduled message with it", dropped)
	}

	var n int
	if err := db.QueryRowContext(t.Context(),
		`SELECT count(*) FROM emails WHERE id = $1`, kept).Scan(&n); err != nil {
		t.Fatal(err)
	}

	if n != 1 {
		t.Error("the scheduled message is gone")
	}
}

// The current week straddles the cutoff, so it is never a whole-week
// drop however old the cutoff is. Retention's row-by-row DELETE
// handles it, and this only has to leave it alone.
func TestDropSpentLeavesThePartitionHoldingTheCutoff(t *testing.T) {
	m, db := testMaintainer(t)
	proj := newProject(t, db)
	now := time.Now().UTC()
	insertEmail(t, db, proj, "sent", now.Add(-time.Hour))

	dropped, err := m.DropSpent(t.Context(), now)
	if err != nil {
		t.Fatalf("drop: %v", err)
	}

	for _, d := range dropped {
		if d == partitionName(dayStart(now)) {
			t.Fatal("dropped the partition the cutoff falls inside")
		}
	}

	var n int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM emails`).Scan(&n); err != nil {
		t.Fatal(err)
	}

	if n != 1 {
		t.Errorf("%d rows left, want the one just inserted", n)
	}
}

// Nothing but a name of the exact shape partitionName produces gets
// concatenated into a statement.
func TestOnlyGeneratedNamesReachAStatement(t *testing.T) {
	if err := mustBeSafeName(partitionName(time.Now())); err != nil {
		t.Errorf("a generated name was refused: %v", err)
	}

	// The WEEKLY prefix is still accepted, and has to be: DropSpent names
	// partitions it read out of pg_class, and the catalog holds the weekly
	// ones laid down before the changeover until they age out. Refusing
	// them would leave exactly the partitions that need removing as the
	// ones this cannot remove.
	if err := mustBeSafeName("emails_w2026_08_03"); err != nil {
		t.Errorf("a pre-changeover weekly name was refused: %v", err)
	}

	for _, bad := range []string{
		"emails", "emails_default", "emails_d2026_08", "emails_d2026_08_03; DROP TABLE users",
		"", "public.emails_d2026_08_03", "EMAILS_D2026_08_03",
		// A prefix nobody generates. The character class is [dw] and not
		// a wildcard, so a third kind of partition has to be a deliberate
		// change here rather than something that quietly starts working.
		"emails_m2026_08_03",
	} {
		if err := mustBeSafeName(bad); err == nil {
			t.Errorf("%q was accepted as a partition name", bad)
		}
	}
}

// The ceiling is an ALARM, not a scythe.
//
// It can only be reached by setting retention_days to 0, which is the
// operator saying keep everything forever - so removing their history to
// protect a lock table would be answering a question nobody asked. What
// it must do is say so loudly enough to be acted on, and keep working.
//
// Why it matters at all: daily partitions with nothing dropping them grow
// by 365 a year, and at 730 the queue claim takes 2194 relation locks. The
// lock table is shared and holds about 6400 on a default install, so
// sixteen concurrent claims failed with "out of shared memory" - measured,
// against 105 weekly partitions where the same sixteen failed none.
func TestTheCeilingWarnsAndDoesNotDropAnything(t *testing.T) {
	m, db := testMaintainer(t)
	var logged strings.Builder
	m.Log = slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelError}))
	// Low enough that the partitions already present cross it.
	m.Ceiling = 2

	before := partitionCount(t, db)
	if _, err := m.EnsureAhead(t.Context()); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	out := logged.String()
	if !strings.Contains(out, "too many partitions") {
		t.Errorf("the ceiling was crossed and nothing was logged at error level: %q", out)
	}

	// The remedy has to be IN the message. An alarm that names a number
	// and not what to do about it is a line somebody scrolls past.
	if !strings.Contains(out, "retention_days") {
		t.Errorf("the alarm does not name the remedy: %q", out)
	}

	// And nothing was removed. Crossing the ceiling must not cost a row
	// or a partition.
	if after := partitionCount(t, db); after < before {
		t.Errorf("partitions went from %d to %d - the ceiling dropped something", before, after)
	}

	// Still functional: the job keeps topping up the horizon.
	proj := newProject(t, db)
	insertEmail(t, db, proj, "sent", dayStart(time.Now().UTC()).AddDate(0, 0, daysAhead).Add(9*time.Hour))
}

// Under the ceiling it says nothing, or the alarm is noise.
func TestNoAlarmBelowTheCeiling(t *testing.T) {
	m, _ := testMaintainer(t)
	var logged strings.Builder
	m.Log = slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelError}))

	if _, err := m.EnsureAhead(t.Context()); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	if strings.Contains(logged.String(), "too many partitions") {
		t.Errorf("a fresh install tripped the ceiling: %q", logged.String())
	}
}

// No two partitions may claim the same instant. This is the property the
// changeover rests on - weekly and daily ranges coexisting - and Postgres
// enforces it, so a failure here is the maintainer asking for something
// impossible rather than a silent overlap.
func TestNoTwoPartitionsOverlap(t *testing.T) {
	m, _ := testMaintainer(t)
	if _, err := m.EnsureAhead(t.Context()); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	parts, err := m.rangePartitions(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if len(parts) < 2 {
		t.Fatalf("only %d partitions, so this proves nothing", len(parts))
	}

	for i, a := range parts {
		for j, b := range parts {
			if i >= j {
				continue
			}

			if a.lower.Before(b.upper) && b.lower.Before(a.upper) {
				t.Errorf("%s [%s,%s) overlaps %s [%s,%s)",
					a.name, a.lower, a.upper, b.name, b.lower, b.upper)
			}
		}
	}
}
