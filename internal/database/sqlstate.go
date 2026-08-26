// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package database

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// SQLState returns the five-character PostgreSQL error code carried by
// err, or "" when err is not a Postgres error. For the codes with a
// meaning already attached, prefer the named helpers (MalformedID,
// UniqueViolation) - this exists for callers that branch on a code
// those do not cover, like the partition sweep telling "the table is
// already gone" apart from "the lock timed out".
func SQLState(err error) string {
	pg, ok := errors.AsType[*pgconn.PgError](err)
	if !ok {
		return ""
	}

	return pg.Code
}
