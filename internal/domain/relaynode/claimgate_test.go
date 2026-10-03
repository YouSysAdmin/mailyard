// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package relaynode

import (
	"testing"
	"time"
)

// A new claim from the same node releases the one parked before it, so
// a node that restarts mid-claim parks again instead of being answered
// empty at once while its dead claim holds the slot.
func TestANewClaimReleasesTheParkedOne(t *testing.T) {
	g := &claimGate{nodes: map[string]*parkedClaim{}}

	first, releaseFirst := g.enter(t.Context(), "n1")
	defer releaseFirst()

	second, releaseSecond := g.enter(t.Context(), "n1")

	select {
	case <-first.Done():
	case <-time.After(time.Second):
		t.Fatal("the first claim was not released by the second")
	}

	if second.Err() != nil {
		t.Fatal("the new claim was released, want it parked")
	}

	// Releasing the superseded claim late must not drop the new one.
	releaseFirst()
	if g.nodes["n1"] == nil {
		t.Fatal("a late release of the old claim removed the new one")
	}

	releaseSecond()
	if g.nodes["n1"] != nil {
		t.Error("the slot stayed taken after the parked claim answered")
	}

	other, releaseOther := g.enter(t.Context(), "n2")
	defer releaseOther()

	if other.Err() != nil {
		t.Error("another node's claim was released")
	}
}
