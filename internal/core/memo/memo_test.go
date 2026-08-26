// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package memo

import (
	"errors"
	"testing"
	"time"
)

// Within the TTL one computation answers every caller, errors included,
// and past it the next caller recomputes.
func TestAValueIsComputedOncePerWindow(t *testing.T) {
	clock := time.Unix(1000, 0)
	v := New[int](time.Second)
	v.now = func() time.Time { return clock }

	calls := 0
	compute := func() (int, error) {
		calls++
		if calls == 1 {
			return 0, errors.New("down")
		}

		return calls, nil
	}

	if _, err := v.Get(compute); err == nil {
		t.Fatal("first call: want the computed error")
	}

	if _, err := v.Get(compute); err == nil || calls != 1 {
		t.Fatalf("second call inside the window: err %v calls %d, want the memoized error and 1 call", err, calls)
	}

	clock = clock.Add(time.Second)
	got, err := v.Get(compute)
	if err != nil || got != 2 || calls != 2 {
		t.Fatalf("after the window: got %d err %v calls %d, want a fresh answer", got, err, calls)
	}
}
