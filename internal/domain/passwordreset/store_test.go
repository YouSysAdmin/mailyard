// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package passwordreset

import (
	"sync"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	prmodel "github.com/yousysadmin/mailyard/internal/models/passwordreset"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db := dbtest.Open(t)
	dbtest.Schema(t, db, `
        CREATE TABLE password_resets (
            id         TEXT PRIMARY KEY,
            user_id    TEXT NOT NULL,
            token_hash TEXT NOT NULL,
            expires_at TIMESTAMPTZ NOT NULL,
            used_at    TIMESTAMPTZ,
            request_ip TEXT NOT NULL DEFAULT '',
            created_at TIMESTAMPTZ NOT NULL DEFAULT now()
        )`)

	return &Store{Base: database.NewBase(db)}
}

func seed(t *testing.T, s *Store, id string) {
	t.Helper()
	if err := s.Put(t.Context(), &prmodel.Token{
		ID:        id,
		UserID:    "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1",
		TokenHash: prmodel.Hash("tok-" + id),
		ExpiresAt: time.Now().UTC().Add(30 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
}

// A reset link must work exactly once. The `used_at IS NULL` guard is
// what makes the UPDATE atomic, but it only enforces anything if the
// caller reads the result - so the store has to report who won, and
// the second redemption has to come back false rather than silently
// succeeding.
func TestMarkUsedIsSingleUse(t *testing.T) {
	s := newTestStore(t)
	seed(t, s, "c8a80495-0aba-431e-8ecc-441cb5572bbd")
	now := time.Now().UTC()

	first, err := s.MarkUsed(t.Context(), "c8a80495-0aba-431e-8ecc-441cb5572bbd", now)
	if err != nil {
		t.Fatal(err)
	}

	if !first {
		t.Fatal("first redemption must win")
	}

	second, err := s.MarkUsed(t.Context(), "c8a80495-0aba-431e-8ecc-441cb5572bbd", now)
	if err != nil {
		t.Fatal(err)
	}

	if second {
		t.Error("a spent token must not be claimable again")
	}

	// An id that names nothing is a loss too, not an error.
	missing, err := s.MarkUsed(t.Context(), "nope", now)
	if err != nil {
		t.Fatal(err)
	}

	if missing {
		t.Error("claiming a token that does not exist must report a loss")
	}
}

// Two redemptions racing on the same link - the phished-link case, or
// simply a double-clicked button - must produce exactly one winner.
func TestMarkUsedRaceHasOneWinner(t *testing.T) {
	s := newTestStore(t)
	seed(t, s, "daefd3c7-ae9d-4f19-801a-5d7c4fd26dfd")
	now := time.Now().UTC()

	const racers = 8
	var wg sync.WaitGroup
	results := make([]bool, racers)
	for i := range racers {
		wg.Go(func() {
			ok, err := s.MarkUsed(t.Context(), "daefd3c7-ae9d-4f19-801a-5d7c4fd26dfd", now)
			if err != nil {
				t.Errorf("racer %d: %v", i, err)

				return
			}

			results[i] = ok
		})
	}

	wg.Wait()

	won := 0
	for _, r := range results {
		if r {
			won++
		}
	}

	if won != 1 {
		t.Errorf("%d of %d redemptions claimed the token, want exactly 1", won, racers)
	}
}
