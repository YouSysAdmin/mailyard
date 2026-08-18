// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package ids mints the primary keys.
//
// One function, so the uuid version is decided in one place instead of
// at a hundred call sites. Named ids and not id, which is a local
// variable in nearly every handler here.
package ids

import (
	"time"

	"github.com/google/uuid"
)

// New returns a UUIDv7 as a string.
//
// v7 and not v4: the leading 48 bits are a timestamp, so inserts land
// at the right-hand edge of the index instead of dirtying a random
// page. It only shows once the index outgrows memory - at 2M rows,
// 16.2s for v4 against 9.1s for v7. It also gives the (created_at, id)
// keyset cursor a tiebreaker in insertion order.
func New() string {
	return uuid.Must(uuid.NewV7()).String()
}

// MintedAt returns the millisecond a v7 id was minted, and whether it
// could be read at all.
//
// The leading 48 bits of a v7 are a Unix millisecond timestamp - the
// same property that makes these ids index-friendly - so an id carries
// roughly WHEN its row was created. That is worth something specific
// here: `emails` is RANGE partitioned by created_at, so a lookup by id
// alone visits every live partition, and this is what lets a caller name
// the week instead.
//
// ROUGHLY, and the distance matters. The id is minted before the row is
// written, so the timestamp is a little earlier than created_at - and a
// caller that sets created_at itself (an import, a test fixture) can put
// them arbitrarily far apart. So this answers "about when", and every
// caller pairs it with a window and a fallback. It is never a
// substitute for reading the column.
//
// False for anything that is not a v7 uuid, including a malformed
// string: there is no timestamp to read, and guessing one would produce
// a window that silently excludes the row.
func MintedAt(id string) (time.Time, bool) {
	u, err := uuid.Parse(id)
	if err != nil || u.Version() != 7 {
		return time.Time{}, false
	}

	ms := int64(u[0])<<40 | int64(u[1])<<32 | int64(u[2])<<24 |
		int64(u[3])<<16 | int64(u[4])<<8 | int64(u[5])

	return time.UnixMilli(ms).UTC(), true
}
