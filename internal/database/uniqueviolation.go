// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package database

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// UniqueViolation reports whether err is Postgres refusing a write
// because it would duplicate a key, and if index is not empty, that it
// was that index.
//
// The constraint name matters, because "a duplicate" is only meaningful
// with the answer to "of what". A caller that reads any 23505 as "this
// already exists" turns an unrelated key collision into a success, and
// the row it then reports as existing is a different row entirely.
// pg.ConstraintName carries the index name for a unique-index violation,
// which is a protocol field rather than translated prose - the same
// reason MalformedID matches Routine and not the message.
//
// Matched as a prefix, since a partitioned or renamed index can carry a
// suffix Postgres appended to keep names unique.
func UniqueViolation(err error, index string) bool {
	pg, ok := errors.AsType[*pgconn.PgError](err)
	if !ok {
		return false
	}

	if pg.Code != "23505" {
		return false
	}

	return index == "" || strings.HasPrefix(pg.ConstraintName, index)
}
