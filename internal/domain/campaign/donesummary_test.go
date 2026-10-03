// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package campaign

import "testing"

func TestTheDoneSummaryNamesEveryOutcome(t *testing.T) {
	if got := doneSummary(map[string]int{"sent": 3, "skipped": 1}); got != "3 sent, 0 failed, 1 skipped." {
		t.Errorf("summary = %q", got)
	}
}
