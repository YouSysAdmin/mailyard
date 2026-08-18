// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package postgres

import (
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"strconv"
	"strings"

	"github.com/pressly/goose/v3"

	"github.com/yousysadmin/mailyard/migrations"
)

// BinarySchemaVersion is the newest migration this binary carries.
//
// Read from the embedded FS rather than kept as a constant beside it:
// a number somebody has to remember to bump is a number that is wrong the
// first time a migration is added in a hurry, and being wrong here means
// either refusing a healthy install or admitting a stale one.
func BinarySchemaVersion() (int64, error) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return 0, fmt.Errorf("reading the embedded migrations: %w", err)
	}

	var newest int64
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}

		digits, _, ok := strings.Cut(name, "_")
		if !ok {
			continue
		}

		v, err := strconv.ParseInt(digits, 10, 64)
		if err != nil {
			// goose refuses to run a file it cannot version, so this is
			// not a state to tolerate quietly: it would silently lower
			// the version this binary believes it needs.
			return 0, fmt.Errorf("migration %q does not start with a version number", name)
		}

		if v > newest {
			newest = v
		}
	}

	if newest == 0 {
		return 0, fmt.Errorf("the embedded migrations are empty")
	}

	return newest, nil
}

// RequireCurrentSchema refuses a database whose schema is older than
// the migrations this binary carries.
//
// Without it, a node booting without --init only checked that a schema
// existed at all: an upgraded binary started happily on last month's
// schema, answered health checks and served pages whose columns did
// not exist. Fatal for the same reason a refused port bind is - in a
// rolling upgrade the node with --init applies the schema and the rest
// boot once it has.
//
// Only for a serving node. The one-shot commands open the same
// database with migrate=false and have to work against an installation
// whose server will not come up.
//
// A database AHEAD only warns. That is a rollback, and a column this
// binary never names costs it nothing.
func RequireCurrentSchema(db *sql.DB) error {
	want, err := BinarySchemaVersion()
	if err != nil {
		return err
	}

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("goose set dialect: %w", err)
	}

	have, err := goose.GetDBVersion(db)
	if err != nil {
		return fmt.Errorf("reading the applied schema version: %w", err)
	}

	switch {
	case have < want:
		return fmt.Errorf(
			"the database is at schema version %d and this binary needs %d - "+
				"start exactly one node with `--init` to apply the pending migrations",
			have, want)
	case have > want:
		slog.Warn("the database schema is newer than this binary - "+
			"a rollback, so any column added since is simply unused here",
			"database", have, "binary", want)
	}

	return nil
}
