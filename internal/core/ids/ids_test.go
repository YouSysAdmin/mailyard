// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package ids_test

import (
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/yousysadmin/mailyard/internal/core/ids"
)

func TestNewIsVersion7AndOrdered(t *testing.T) {
	const n = 5000
	out := make([]string, 0, n)
	for range n {
		s := ids.New()
		u, err := uuid.Parse(s)
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}

		if v := u[6] >> 4; v != 7 {
			t.Fatalf("%s is version %d, want 7", s, v)
		}

		out = append(out, s)
	}

	// The point of the change: consecutive ids sort in the order they
	// were minted, so an insert lands at the right-hand edge of the
	// index instead of somewhere random. Lexical order is byte order
	// here because the string form is fixed width hex.
	if !slices.IsSorted(out) {
		for i := 1; i < len(out); i++ {
			if out[i-1] > out[i] {
				t.Fatalf("ids came back out of order at %d: %s then %s", i, out[i-1], out[i])
			}
		}
	}

	if len(slices.Compact(slices.Clone(out))) != n {
		t.Error("two ids collided")
	}
}

// The timestamp a v7 carries has to be the time it was minted, or the
// partition window built from it excludes the row it names.
func TestMintedAtReadsTheTimestampBackOut(t *testing.T) {
	before := time.Now().UTC().Add(-time.Millisecond)
	id := ids.New()
	after := time.Now().UTC().Add(time.Millisecond)

	got, ok := ids.MintedAt(id)
	if !ok {
		t.Fatalf("ids.MintedAt(%q) could not read a freshly minted id", id)
	}

	if got.Before(before) || got.After(after) {
		t.Errorf("ids.MintedAt = %s, want between %s and %s", got, before, after)
	}
}

// Anything that is not a v7 has no timestamp to read, and inventing one
// would build a window that quietly misses the row.
func TestMintedAtRefusesWhatIsNotAV7(t *testing.T) {
	for _, tc := range []struct{ what, id string }{
		{"a v4", uuid.NewV4().String()},
		{"the nil uuid", "00000000-0000-0000-0000-000000000000"},
		{"not a uuid at all", "banana"},
		{"empty", ""},
	} {
		if _, ok := ids.MintedAt(tc.id); ok {
			t.Errorf("%s: ids.MintedAt reported a timestamp", tc.what)
		}
	}
}
