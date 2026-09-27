// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package user

import (
	"errors"
	"sync"
	"testing"
	"time"

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

// The password lockout is one statement on the row: the limit-th wrong
// password locks, the count resets with it, and a right password clears
// both.
func TestRecordLoginFailureLocksAtTheLimit(t *testing.T) {
	s := newTestStore(t)
	const id = "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1"

	for i := 1; i < 3; i++ {
		locked, err := s.RecordLoginFailure(t.Context(), id, 3, time.Minute)
		if err != nil || locked {
			t.Fatalf("failure %d: locked=%v err=%v, want unlocked", i, locked, err)
		}
	}

	locked, err := s.RecordLoginFailure(t.Context(), id, 3, time.Minute)
	if err != nil || !locked {
		t.Fatalf("third failure: locked=%v err=%v, want locked", locked, err)
	}

	until, err := s.LoginLockedUntil(t.Context(), id)
	if err != nil || until == nil || !until.After(time.Now()) {
		t.Fatalf("LoginLockedUntil = %v, %v, want a future time", until, err)
	}

	if err := s.ClearLoginFailures(t.Context(), id); err != nil {
		t.Fatal(err)
	}

	if until, _ := s.LoginLockedUntil(t.Context(), id); until != nil {
		t.Fatalf("still locked until %v after a right password", until)
	}
}

// Demoting or disabling the last enabled administrator is refused
// inside the write.
func TestTheLastAdministratorCannotBeDemoted(t *testing.T) {
	s := newTestStore(t)
	admin := &usermodel.User{ID: "6b1b2a2e-2b5e-4d9e-8c1a-0a4a3f9d1a01", Email: "admin@example.com", Admin: true}
	if err := s.Put(t.Context(), admin); err != nil {
		t.Fatal(err)
	}

	admin.Admin = false
	if err := s.PutKeepingAnAdmin(t.Context(), admin); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("demoting the only admin: err = %v, want ErrLastAdmin", err)
	}

	admin.Admin, admin.Disabled = true, true
	if err := s.PutKeepingAnAdmin(t.Context(), admin); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("disabling the only admin: err = %v, want ErrLastAdmin", err)
	}

	second := &usermodel.User{ID: "9c7d5f11-4e2a-4b6f-9a3d-2f1e0c8b7a02", Email: "second@example.com", Admin: true}
	if err := s.Put(t.Context(), second); err != nil {
		t.Fatal(err)
	}

	admin.Disabled = false
	admin.Admin = false
	if err := s.PutKeepingAnAdmin(t.Context(), admin); err != nil {
		t.Fatalf("demoting one of two admins: %v", err)
	}

	got, _ := s.GetByID(t.Context(), admin.ID)
	if got.Admin {
		t.Error("the demotion was not written")
	}
}

// PutFirst makes exactly one user the first, under a lock, and with
// onlyIfFirst writes nothing once anybody exists.
func TestPutFirstAdmitsExactlyOneFirstUser(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := NewStore(db)

	a := &usermodel.User{ID: "3e5f1c9d-7a2b-4c8e-9d1f-5b6a7c8d9e03", Email: "a@example.com"}
	first, err := s.PutFirst(t.Context(), a, false)
	if err != nil || !first || !a.Admin {
		t.Fatalf("first user: first=%v admin=%v err=%v", first, a.Admin, err)
	}

	b := &usermodel.User{ID: "4f6a2d0e-8b3c-4d9f-8e2a-6c7b8d9e0f04", Email: "b@example.com"}
	first, err = s.PutFirst(t.Context(), b, false)
	if err != nil || first || b.Admin {
		t.Fatalf("second user: first=%v admin=%v err=%v", first, b.Admin, err)
	}

	if got, _ := s.GetByID(t.Context(), b.ID); got == nil {
		t.Fatal("the second user was not written")
	}

	c := &usermodel.User{ID: "5a7b3e1f-9c4d-4e0a-9f3b-7d8c9e0f1a05", Email: "c@example.com"}
	first, err = s.PutFirst(t.Context(), c, true)
	if err != nil || first {
		t.Fatalf("bootstrap against a used table: first=%v err=%v", first, err)
	}

	if got, _ := s.GetByID(t.Context(), c.ID); got != nil {
		t.Fatal("onlyIfFirst wrote a row into a used table")
	}
}
