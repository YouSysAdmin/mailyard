// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package postgres

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/yousysadmin/mailyard/internal/database/dbtest"
)

// The version this binary needs is the newest migration on disk.
//
// Computed here from the DIRECTORY and there from the EMBEDDED FS, so the
// two disagree if a migration is ever added without being embedded - and
// a version read too low is the dangerous direction: it admits an
// installation whose schema is missing exactly that file.
func TestTheBinaryKnowsItsNewestMigration(t *testing.T) {
	got, err := BinarySchemaVersion()
	if err != nil {
		t.Fatalf("BinarySchemaVersion: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}

	var want int64
	files := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}

		files++
		digits, _, _ := strings.Cut(e.Name(), "_")
		v, err := strconv.ParseInt(digits, 10, 64)
		if err != nil {
			t.Fatalf("migration %q does not start with a version number", e.Name())
		}

		if v > want {
			want = v
		}
	}

	if files < 40 {
		t.Fatalf("only found %d migrations on disk - this test would prove nothing", files)
	}

	if got != want {
		t.Errorf("the embedded migrations top out at %d, the directory at %d", got, want)
	}
}

// Behind is fatal, level is fine, ahead is a warning.
//
// Rehearsed live before it was written - the previous binary built the
// schema, the new one refused it, `--init` applied the pending migration
// and the same database then served with its rows intact. This is what
// keeps that true without a second container: a mistake in the
// comparison stops every node from booting, which is the one failure
// worse than the one it prevents.
func TestASchemaBehindTheBinaryRefusesToServe(t *testing.T) {
	db := dbtest.Open(t)

	// goose's own bookkeeping table, since dbtest applies the SQL
	// directly rather than through goose.
	if _, err := db.ExecContext(context.Background(), `CREATE TABLE goose_db_version (
		id SERIAL PRIMARY KEY,
		version_id BIGINT NOT NULL,
		is_applied BOOLEAN NOT NULL,
		tstamp TIMESTAMP DEFAULT now())`); err != nil {
		t.Fatalf("create goose table: %v", err)
	}

	newest, err := BinarySchemaVersion()
	if err != nil {
		t.Fatal(err)
	}

	record := func(v int64) {
		if _, err := db.ExecContext(context.Background(),
			`INSERT INTO goose_db_version (version_id, is_applied) VALUES ($1, true)`, v); err != nil {
			t.Fatalf("record version %d: %v", v, err)
		}
	}

	record(newest - 1)
	err = RequireCurrentSchema(db)
	if err == nil {
		t.Fatal("a schema one migration behind was accepted - an upgraded binary would serve from it")
	}

	// The message has to name both numbers and the fix: an operator
	// reading it at 3am is the whole audience.
	for _, want := range []string{strconv.FormatInt(newest-1, 10), strconv.FormatInt(newest, 10), "--init"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %v", want, err)
		}
	}

	record(newest)
	if err := RequireCurrentSchema(db); err != nil {
		t.Errorf("a current schema was refused: %v", err)
	}

	// A rollback. The operator did this deliberately, and columns this
	// binary does not name cost it nothing.
	record(newest + 10)
	if err := RequireCurrentSchema(db); err != nil {
		t.Errorf("a schema newer than the binary was refused, which blocks a rollback: %v", err)
	}
}
