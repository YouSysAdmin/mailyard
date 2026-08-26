// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package database

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// uuidParseRoutine is the Postgres C function that refuses a string
// which is not a uuid.
//
// The ROUTINE, not the message. Both arrive in the same error, and the
// message - `invalid input syntax for type uuid: "banana"` - is
// translated by lc_messages, so matching it would work here and stop
// working on an installation whose database speaks anything else. The
// routine name is a protocol field and is not translated.
//
// It is also what tells a malformed uuid from a malformed integer or
// JSON: all three answer 22P02, and only this one means "no row can have that id".
// A bad integer is a different mistake and has to keep failing loudly.
const uuidParseRoutine = "string_to_uuid"

// MalformedID reports whether err is Postgres refusing a parameter that
// cannot be a uuid.
//
// Ids are uuid columns, so passing `banana` where one belongs is not a
// request for a row that happens to be missing: the comparison cannot be
// made at all and the statement fails, which would otherwise surface as
// a 500. A 404 is the honest answer - no resource has that id, and none
// ever can.
//
// We match on the routine rather than the message, because the message
// is translated by lc_messages and 22P02 also covers a malformed integer
// or JSON, which are bugs that must keep failing loudly:
//
//	uuid   code=22P02 routine="string_to_uuid"     file=uuid.c
//	int    code=22P02 routine="pg_strtoint32_safe" file=numutils.c
//	jsonb  code=22P02 routine="json_errsave_error" file=jsonfuncs.c
func MalformedID(err error) bool {
	pg, ok := errors.AsType[*pgconn.PgError](err)
	if !ok {
		return false
	}

	return pg.Code == "22P02" && pg.Routine == uuidParseRoutine
}
