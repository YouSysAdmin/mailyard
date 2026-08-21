// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package dbtest gives store tests a real PostgreSQL to run against.
//
// A real engine and not in-memory SQLite, however convenient that would
// be. These tests pin SQL semantics - an atomic claim, a conditional
// UPDATE, a LIKE with an ESCAPE clause - and semantics are exactly what
// differs between engines.
//
// The connection comes from MAILYARD_TEST_DSN. Without it they skip
// rather than fail, so `go test ./...` stays green with no database, at
// the cost that a skipped test did not run:
//
//	docker run -d --rm --name mailyard-test-pg \
//	  -e POSTGRES_PASSWORD=test -e POSTGRES_DB=mailyard_test \
//	  -p 55432:5432 postgres:16-alpine
//	MAILYARD_TEST_DSN='postgres://postgres:test@localhost:55432/mailyard_test?sslmode=disable' \
//	  go test ./...
package dbtest

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// DSNEnv names the environment variable holding the test connection.
const DSNEnv = "MAILYARD_TEST_DSN"

// Open returns a handle whose search_path points at a schema private
// to this test, created empty and dropped on cleanup.
//
// Per-schema isolation rather than per-database: it is one statement
// instead of a CREATE DATABASE round trip, and it lets tests run in
// parallel against one container without seeing each other's tables.
// The caller creates whatever tables it needs inside it.
func Open(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv(DSNEnv)
	if dsn == "" {
		t.Skipf("%s is not set, skipping the store tests that need a real database", DSNEnv)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open %s: %v", DSNEnv, err)
	}

	if err := db.PingContext(t.Context()); err != nil {
		_ = db.Close()
		t.Fatalf("connect to the test database: %v", err)
	}

	schema := schemaName(t)

	// Drop first: a previous run killed mid-test leaves its schema
	// behind, and inheriting those tables would make this run's results depend on the last one's.
	// A schema name is an identifier, and identifiers cannot be bound
	// as parameters in any engine - interpolation is the only option.
	// schemaName restricts the value to [a-z0-9_] built from the test
	// name, so nothing here comes from outside the test binary.
	//sqlconst:allow schema is an identifier from schemaName, restricted to [a-z0-9_]
	if _, err := db.ExecContext(t.Context(), `DROP SCHEMA IF EXISTS `+schema+` CASCADE`); err != nil {
		_ = db.Close()
		t.Fatalf("drop stale schema %s: %v", schema, err)
	}

	//sqlconst:allow schema is an identifier from schemaName, restricted to [a-z0-9_]
	if _, err := db.ExecContext(t.Context(), `CREATE SCHEMA `+schema); err != nil {
		_ = db.Close()
		t.Fatalf("create schema %s: %v", schema, err)
	}

	//sqlconst:allow schema is an identifier from schemaName, restricted to [a-z0-9_]
	if _, err := db.ExecContext(t.Context(), `SET search_path TO `+schema); err != nil {
		_ = db.Close()
		t.Fatalf("set search_path: %v", err)
	}

	// One connection for the whole handle. search_path is per session,
	// so a pooled second connection would land back on public and see
	// none of the tables this test creates.
	db.SetMaxOpenConns(1)

	t.Cleanup(func() {
		// Same interpolation and same reason as the three above, and it
		// went unmarked because the guard could not see it: this is
		// *sql.DB.Exec, which takes the query first, and sqlSinks maps
		// Exec to the second argument for database.Base. The lookup found
		// nothing at that index and skipped the call. Both ends are fixed
		// - the marker is here, and sqlSinkArg now shifts to the first
		// argument when it is a string expression.
		//
		//sqlconst:allow schema is an identifier from schemaName, restricted to [a-z0-9_]
		if _, err := db.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE`); err != nil {
			t.Logf("cleanup: drop schema %s: %v", schema, err)
		}

		_ = db.Close()
	})

	return db
}

// Peer returns a second handle onto the same private schema, so a
// test can act as two nodes at once.
//
// Open caps itself at one connection because search_path is per
// session, and that cap makes concurrency invisible: two goroutines
// on one handle queue behind the same connection, so a test of
// simultaneous claims would silently be a test of sequential ones -
// and would pass against an implementation with no locking at all.
// Peer gives the second node a connection of its own.
//
// Call it only after Open, which is what creates the schema. The
// schema is dropped by Open's cleanup, so this one only closes.
func Peer(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv(DSNEnv)
	if dsn == "" {
		t.Skipf("%s is not set, skipping the store tests that need a real database", DSNEnv)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open peer handle: %v", err)
	}

	//sqlconst:allow schema is an identifier from schemaName, restricted to [a-z0-9_]
	if _, err := db.ExecContext(t.Context(), `SET search_path TO `+schemaName(t)); err != nil {
		_ = db.Close()
		t.Fatalf("peer set search_path: %v", err)
	}

	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	return db
}

// Schema runs setup DDL, failing the test on error.
//
// Named Schema rather than Exec on purpose: TestNoDynamicSQL treats
// any call to Exec as a SQL sink whose second argument is the query,
// and an Exec(t, db, ddl) here would have every caller reported for
// passing a *sql.DB where a query was expected. Renaming beats
// scattering allow markers over correct code.
func Schema(t *testing.T, db *sql.DB, stmts ...string) {
	t.Helper()
	for _, s := range stmts {
		//sqlconst:allow DDL written in the test itself, never from a request
		if _, err := db.ExecContext(t.Context(), s); err != nil {
			if extensionRace(err, s) {
				// The extension exists NOW, which is all the statement
				// was for - a concurrent package won the insert.
				continue
			}

			t.Fatalf("setup statement failed: %v\n%s", err, s)
		}
	}
}

// extensionRace reports whether err is the loser's half of two
// packages creating the same extension at once.
//
// Every test gets a schema of its own, but an EXTENSION is
// database-global - one row in pg_extension - and `go test ./...`
// runs package binaries in parallel against one database. CREATE
// EXTENSION IF NOT EXISTS is check-then-insert with nothing
// serializing two checkers, so the loser gets 23505 on
// pg_extension_name_index (or 42710 when it lost after the winner
// committed). Both mean the same thing: by the time the error is
// raised the winner's row is committed, so the state the statement
// wanted is the state the database is in.
//
// Scoped to CREATE EXTENSION IF NOT EXISTS statements, because a
// 23505 from anything else - a data INSERT in a migration - is a bug
// that must keep failing loudly.
func extensionRace(err error, stmt string) bool {
	if !strings.Contains(stmt, "CREATE EXTENSION IF NOT EXISTS") {
		return false
	}

	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return false
	}

	return pg.Code == "23505" || pg.Code == "42710"
}

// schemaName turns a test name into a legal, unique identifier.
// t.Name() carries slashes and capitals from subtests, neither of
// which survives an unquoted identifier.
func schemaName(t *testing.T) string {
	var b strings.Builder
	b.WriteString("t_")
	for _, r := range strings.ToLower(t.Name()) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}

	name := b.String()

	// Postgres truncates identifiers at 63 bytes, and a silent
	// truncation could collide two long test names onto one schema.
	if len(name) > 60 {
		name = name[:60]
	}

	return name
}

// Migrate applies the real migrations to the test schema.
//
// Prefer this to a hand-written CREATE TABLE. A subset schema tests
// the columns somebody remembered to copy, and drifts silently the
// moment a migration adds one: two suites here pinned their own
// version of the emails table and both broke, at a distance, when a
// column was added elsewhere. The error then points at the test
// rather than at anything real.
//
// The Down half of each file is skipped - it would undo the Up half we just applied.
func Migrate(t *testing.T, db *sql.DB) {
	t.Helper()
	Schema(t, db, MigrationsUp(t)...)
}

// MigrationsUp returns the Up half of every migration, in order, for
// callers that need the SQL rather than an applied schema.
func MigrationsUp(t *testing.T) []string {
	t.Helper()
	dir := MigrationsDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}

	// Lexical order is migration order - goose zero-pads them so this holds.
	sort.Strings(names)

	out := make([]string, 0, len(names))
	for _, name := range names {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}

		sqlText := string(body)
		if i := strings.Index(sqlText, "-- +goose Down"); i >= 0 {
			sqlText = sqlText[:i]
		}

		out = append(out, sqlText)
	}

	return out
}

// MigrationsDir walks up from the test's working directory until it
// finds the migrations, so callers at any package depth can use this
// without hard-coding how many ".." they are from the root.
//
// Exported because a test elsewhere may read the SQL for its own
// reasons. One walk, so moving the directory is one edit rather than
// a hunt for relative paths that still resolve to nothing and fail as "no such file".
func MigrationsDir(t *testing.T) string {
	t.Helper()
	rel := "migrations"
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	for range 10 {
		candidate := filepath.Join(dir, rel)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}

		dir = parent
	}

	t.Fatalf("could not find %s above the working directory", rel)

	return ""
}
