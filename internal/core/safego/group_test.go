// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package safego

import (
	"log/slog"
	"testing"
	"time"
)

// Wait returns once the tracked work has, and gives up at its timeout
// on work that has not.
func TestAGroupWaitsForItsWork(t *testing.T) {
	var g Group
	release := make(chan struct{})
	finished := false
	g.Go(slog.Default(), "test", func() {
		<-release
		finished = true
	})

	if g.Wait(20 * time.Millisecond) {
		t.Fatal("Wait reported done while the work was still running")
	}

	close(release)
	if !g.Wait(5*time.Second) || !finished {
		t.Fatal("Wait returned before the work finished")
	}
}

// A panicking unit is recovered and still counted as finished.
func TestAPanicInAGroupIsRecovered(t *testing.T) {
	var g Group
	g.Go(slog.New(slog.DiscardHandler), "test", func() { panic("boom") })

	if !g.Wait(5 * time.Second) {
		t.Fatal("a panicking unit was never counted as done")
	}
}
