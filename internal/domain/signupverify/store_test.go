// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package signupverify

import (
	"sync"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	svmodel "github.com/yousysadmin/mailyard/internal/models/signupverify"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db := dbtest.Open(t)
	dbtest.Schema(t, db, `
        CREATE TABLE signup_verifications (
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
	if err := s.Put(t.Context(), &svmodel.Token{
		ID:        id,
		UserID:    "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1",
		TokenHash: svmodel.Hash("tok-" + id),
		ExpiresAt: time.Now().UTC().Add(svmodel.TTL),
	}); err != nil {
		t.Fatal(err)
	}
}

// A verification link must work exactly once - it signs the account
// in, so a replay is a session for whoever else holds the link.
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

// Two redemptions racing on the same link must produce exactly one
// winner - same guarantee as password reset, same conditional UPDATE.
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

// Verifying by another route (OIDC, an admin) must kill every
// outstanding link, or a stale mail keeps a way in.
func TestInvalidateForUserBurnsEverything(t *testing.T) {
	s := newTestStore(t)
	seed(t, s, "a")
	seed(t, s, "b")
	now := time.Now().UTC()

	if err := s.InvalidateForUser(t.Context(), "1ce81574-b36f-4f3e-8e6e-dabcaaf8f5b1", now); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"a", "b"} {
		ok, err := s.MarkUsed(t.Context(), id, now)
		if err != nil {
			t.Fatal(err)
		}

		if ok {
			t.Errorf("token %s survived InvalidateForUser", id)
		}
	}
}
