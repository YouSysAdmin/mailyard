// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package keyset

import (
	"testing"
	"time"
)

func TestACursorSurvivesARoundTrip(t *testing.T) {
	// Nanosecond precision on purpose: Postgres stores microseconds,
	// and a cursor that loses sub-second detail would re-read or skip
	// rows at every page boundary.
	want := Cursor{
		CreatedAt: time.Date(2026, 8, 8, 3, 15, 25, 123456000, time.UTC),
		ID:        "3f7c1a2b-0000-4000-8000-000000000001",
	}
	got := DecodeCursor(want.Encode())
	if !got.CreatedAt.Equal(want.CreatedAt) || got.ID != want.ID {
		t.Errorf("round trip produced %+v, want %+v", got, want)
	}
}

// A cursor is a bookmark, not a credential. Anything unparseable
// means "start at the beginning" - failing a list request because
// somebody has a stale tab open helps nobody, and the worst a bad
// cursor can do is show the first page.
func TestAMalformedCursorIsTheFirstPage(t *testing.T) {
	for _, in := range []string{
		"",
		"not base64 at all !!!",
		"YWJj",                         // valid base64, no separator
		"fA",                           // just the separator
		"MjAyNi0wOC0wOHxpZA",           // unparseable timestamp
		"MjAyNi0wOC0wOFQwMzoxNTowMFp8", // timestamp, empty id
	} {
		if got := DecodeCursor(in); !got.IsZero() {
			t.Errorf("DecodeCursor(%q) produced %+v, want the zero cursor", in, got)
		}
	}
}

// The zero cursor must encode to the empty string rather than to a
// cursor pointing at the zero time, which would page past every row.
func TestTheZeroCursorEncodesToNothing(t *testing.T) {
	if got := (Cursor{}).Encode(); got != "" {
		t.Errorf("the zero cursor encoded to %q", got)
	}

	// Half a cursor is not a cursor. An id with no timestamp cannot
	// resume anything, and a timestamp with no id loses the tie-break
	// the pair exists for.
	if got := (Cursor{ID: "abc"}).Encode(); got != "" {
		t.Errorf("a cursor with no timestamp encoded to %q", got)
	}

	if got := (Cursor{CreatedAt: time.Now()}).Encode(); got != "" {
		t.Errorf("a cursor with no id encoded to %q", got)
	}
}

// Cut is how a handler tells "last page" from "there is more"
// without a COUNT. The store over-fetches by one, and that extra row
// must never reach the client.
func TestCutHidesTheOverfetchedRow(t *testing.T) {
	cases := []struct {
		name     string
		rows     int
		limit    int
		wantPage int
		wantMore bool
	}{
		{"a partial page is the last page", 3, 10, 3, false},
		{"an exactly full page is the last page", 10, 10, 10, false},
		{"one over means there is more", 11, 10, 10, true},
		{"nothing at all", 0, 10, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows := make([]int, tc.rows)
			page, more := Cut(rows, tc.limit)
			if len(page) != tc.wantPage || more != tc.wantMore {
				t.Errorf("Cut(%d rows, limit %d) = %d rows, more=%v; want %d, %v",
					tc.rows, tc.limit, len(page), more, tc.wantPage, tc.wantMore)
			}
		})
	}
}
