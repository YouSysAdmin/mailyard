// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package user

import (
	"sync"
	"testing"

	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	usermodel "github.com/yousysadmin/mailyard/internal/models/user"
)

// newTestStore builds the slice of the users table this test needs.
// The real migrations pull in the whole schema, which is more setup
// than a single-column guard warrants.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	db := dbtest.Open(t)
	// The real migrations, not a hand-written subset. This test had its
	// own CREATE TABLE and it went stale the moment the users table
	// gained a column - which is the failure the house rule about
	// subset schemas exists to prevent.
	dbtest.Migrate(t, db)
	s := NewStore(db)
	if err := s.Put(t.Context(), &usermodel.User{ID: "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1", Email: "a@example.com"}); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	return s
}

func TestClaimTOTPStepRefusesReplay(t *testing.T) {
	s := newTestStore(t)

	ok, err := s.ClaimTOTPStep(t.Context(), "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1", 100)
	if err != nil || !ok {
		t.Fatalf("first claim: ok=%v err=%v, want true/nil", ok, err)
	}

	// The same code presented twice inside its 90-second window.
	ok, err = s.ClaimTOTPStep(t.Context(), "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1", 100)
	if err != nil {
		t.Fatalf("replay claim: %v", err)
	}

	if ok {
		t.Error("replaying step 100 was accepted, want refused")
	}

	// An earlier step inside the skew window is a replay too.
	if ok, _ := s.ClaimTOTPStep(t.Context(), "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1", 99); ok {
		t.Error("stepping backwards to 99 was accepted, want refused")
	}

	// The next code still works.
	if ok, _ := s.ClaimTOTPStep(t.Context(), "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1", 101); !ok {
		t.Error("step 101 was refused, want accepted")
	}
}

// Two requests presenting the same code at the same moment must not
// both win. A read-then-write in Go would let them.
func TestClaimTOTPStepIsAtomic(t *testing.T) {
	s := newTestStore(t)

	const racers = 8
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		wins  int
		fails []error
	)
	for range racers {
		wg.Go(func() {
			ok, err := s.ClaimTOTPStep(t.Context(), "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1", 500)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				fails = append(fails, err)

				return
			}

			if ok {
				wins++
			}
		})
	}

	wg.Wait()

	for _, err := range fails {
		t.Errorf("claim errored: %v", err)
	}

	if wins != 1 {
		t.Errorf("%d of %d concurrent claims succeeded, want exactly 1", wins, racers)
	}
}
