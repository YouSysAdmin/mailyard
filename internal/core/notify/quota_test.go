// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package notify

import "testing"

// The send path asks this before filing anything, so it answers for
// every accepted message. What it must never do is say yes when the
// observer would then say no - that puts the goroutine back.
func TestQuotaWorthRaisingAgreesWithTheThreshold(t *testing.T) {
	cases := []struct {
		name  string
		used  int
		limit int
		raise bool
	}{
		{name: "unlimited plan raises nothing", used: 1_000_000, limit: 0},
		{name: "a negative limit is not a division", used: 5, limit: -1},
		{name: "well under the threshold", used: 10, limit: 1000},
		{name: "one short of it", used: 799, limit: 1000},
		{name: "exactly at it", used: 800, limit: 1000, raise: true},
		{name: "over it", used: 950, limit: 1000, raise: true},
		{name: "at the limit", used: 1000, limit: 1000, raise: true},
		{name: "past the limit", used: 1200, limit: 1000, raise: true},
		{name: "a limit of one is reached by one", used: 1, limit: 1, raise: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := QuotaWorthRaising(c.used, c.limit); got != c.raise {
				t.Fatalf("QuotaWorthRaising(%d, %d) = %v, want %v", c.used, c.limit, got, c.raise)
			}
		})
	}
}

// warnAt is stated in the documentation and in the console, so a
// change to it is a change to what an operator was told.
func TestTheWarningThresholdIsEightyPercent(t *testing.T) {
	if warnAt != 80 {
		t.Fatalf("warnAt = %d, want 80 - the docs and the dashboard both say 80", warnAt)
	}
}
