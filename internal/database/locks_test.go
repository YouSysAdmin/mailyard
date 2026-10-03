// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package database_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
)

// Holders of one name never overlap, holders of different names do not
// wait for each other, and a released lock is free again.
func TestALockHasOneHolderAtATime(t *testing.T) {
	_ = dbtest.Open(t)

	var inside, overlaps atomic.Int32
	var wg sync.WaitGroup
	for range 10 {
		locks := database.NewLocks(dbtest.Peer(t))
		wg.Go(func() {
			release, err := locks.Hold(t.Context(), "test", "project-a")
			if err != nil {
				t.Errorf("Hold: %v", err)

				return
			}

			if inside.Add(1) > 1 {
				overlaps.Add(1)
			}

			time.Sleep(5 * time.Millisecond)
			inside.Add(-1)
			release()
		})
	}

	wg.Wait()

	if n := overlaps.Load(); n != 0 {
		t.Errorf("%d holders overlapped", n)
	}

	a := database.NewLocks(dbtest.Peer(t))
	b := database.NewLocks(dbtest.Peer(t))
	releaseA, err := a.Hold(t.Context(), "test", "project-a")
	if err != nil {
		t.Fatal(err)
	}

	releaseB, err := b.Hold(t.Context(), "test", "project-b")
	if err != nil {
		t.Fatalf("another key waited on a held one: %v", err)
	}

	releaseB()
	releaseA()
}
