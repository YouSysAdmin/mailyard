// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package bell

import (
	"context"
	"sync"
	"testing"
	"time"
)

// One ring releases every waiter, a wait with no ring times out, and a
// ring before anybody waits is not remembered.
func TestRingReleasesEveryWaiter(t *testing.T) {
	var b Bell
	b.Ring()

	var wg sync.WaitGroup
	rang := make([]bool, 3)
	for i := range rang {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rang[i] = b.Wait(context.Background(), 5*time.Second)
		}()
	}

	time.Sleep(50 * time.Millisecond)
	b.Ring()
	wg.Wait()
	for i, r := range rang {
		if !r {
			t.Errorf("waiter %d timed out, want released by the ring", i)
		}
	}

	if b.Wait(context.Background(), 20*time.Millisecond) {
		t.Error("a wait after the ring was released, want a timeout")
	}
}
