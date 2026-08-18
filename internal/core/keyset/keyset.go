// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package keyset carries the cursor type for lists that grow with
// sending volume rather than with what a person typed in.
//
// OFFSET is fine for templates and api keys, where a project has
// dozens of rows. On suppressions, bounces and webhook deliveries it
// is wrong: those grow per message, and OFFSET 900000 reads and throws
// away nine hundred thousand rows for every page, so page N costs more
// the deeper you go.
//
// A cursor names the last row seen instead, so every page is an index
// seek. It gives up jumping to page 40 - forward only - which is what
// reading a log looks like anyway.
//
// No total count either, same reason: COUNT(*) on a multi-million row
// table is a full index scan per page load for a number nobody acts
// on. "Is there more" is answered by fetching one extra row.
package keyset

import (
	"encoding/base64"
	"strings"
	"time"

	"github.com/google/uuid"
)

// cursorSep separates the timestamp from the id. Not a character
// either half can contain: RFC 3339 has no pipe, and ids are uuids.
const cursorSep = "|"

// Cursor is the position of the last row a client saw. The zero value
// means the first page.
//
// Both halves are needed. Ordering on created_at alone is not a total
// order - two rows written in the same microsecond tie, and a page
// boundary landing between them would either repeat a row or skip
// one. The id breaks the tie, and it is stable because it never
// changes after insert.
type Cursor struct {
	CreatedAt time.Time
	ID        string
}

// IsZero reports whether this is the first page.
func (c Cursor) IsZero() bool { return c.ID == "" || c.CreatedAt.IsZero() }

// Encode renders a cursor for the client. Opaque on purpose: it is
// base64 so nobody builds one by hand and then depends on the shape,
// which is what makes the encoding safe to change later.
func (c Cursor) Encode() string {
	if c.IsZero() {
		return ""
	}

	raw := c.CreatedAt.UTC().Format(time.RFC3339Nano) + cursorSep + c.ID

	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeCursor parses one. A malformed cursor yields the zero value
// rather than an error: the worst it can do is start the caller at
// the first page, and failing a list request over a stale bookmark
// helps nobody.
func DecodeCursor(s string) Cursor {
	if s == "" {
		return Cursor{}
	}

	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}
	}

	at, id, ok := strings.Cut(string(raw), cursorSep)
	if !ok || id == "" {
		return Cursor{}
	}

	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return Cursor{}
	}

	// The id half lands in a `(created_at, id) < (?, ?)` comparison
	// against a uuid column, and Postgres refuses a non-uuid there with
	// 22P02 - which MalformedID then turns into a 404 for the WHOLE
	// list. A crafted cursor carrying "banana" should get the first
	// page like every other malformed one, not make the log look empty.
	if _, err := uuid.Parse(id); err != nil {
		return Cursor{}
	}

	return Cursor{CreatedAt: t, ID: id}
}

// "One keyset page request" is paging.Window, and it is declared there
// only. Every handler goes through paging.WindowFrom, which reads the
// query string, so a second definition here would be one concept in two
// places waiting to drift.

// Cut trims an over-fetched slice back to the page size and reports
// whether more rows exist.
//
// Generic so each store does not write the same four lines. The
// caller supplies the cursor for the last row it keeps, because only
// it knows which fields those are.
func Cut[T any](rows []T, limit int) (page []T, more bool) {
	if limit > 0 && len(rows) > limit {
		return rows[:limit], true
	}

	return rows, false
}
