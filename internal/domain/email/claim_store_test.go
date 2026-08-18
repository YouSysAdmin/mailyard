// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
)

// newClaimStore runs the real migrations rather than a copy of the
// emails table. The copy drifted the first time a column was added
// elsewhere, and the failure surfaced as a column-does-not-exist deep
// inside an unrelated suite.
//
// emails.project_id has a foreign key, so a project row has to exist
// before any of these inserts.
func newClaimStore(t *testing.T) *Store {
	t.Helper()
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	dbtest.Schema(t, db, `
        INSERT INTO projects (id, name, slug, default_language, created_at)
        VALUES ('e66e7a4d-9e6c-4884-869a-cf9ffcf22181', 'Test', 'test', 'en', now())`)

	return &Store{Base: database.NewBase(db)}
}

// peerStore is a second node against the same rows.
func peerStore(t *testing.T) *Store {
	t.Helper()

	return &Store{Base: database.NewBase(dbtest.Peer(t))}
}

func insertQueued(t *testing.T, s *Store, id, status string, due time.Time) {
	t.Helper()
	if _, err := s.DB().ExecContext(t.Context(), s.Q(`
        INSERT INTO emails (id, project_id, sender, recipients, subject,
                            status, next_attempt_at, created_at)
        VALUES (?, 'e66e7a4d-9e6c-4884-869a-cf9ffcf22181', 'a@b.com', '["c@d.com"]', 'subject', ?, ?, now())`),
		id, status, due); err != nil {
		t.Fatal(err)
	}
}

func idsOf(claimed []*emailmodel.Email) []string {
	out := make([]string, 0, len(claimed))
	for _, e := range claimed {
		out = append(out, e.ID)
	}

	return out
}

func TestClaimDueTakesOnlyDueQueuedRows(t *testing.T) {
	s := newClaimStore(t)
	now := time.Now().UTC()
	past := now.Add(-time.Minute)
	future := now.Add(time.Hour)

	insertQueued(t, s, "9f4774e9-f1b4-4904-81eb-e09fc5d838a6", emailmodel.StatusQueued, past)
	insertQueued(t, s, "3e0097d0-7fcd-43b9-800c-88101ca45476", emailmodel.StatusScheduled, past)
	insertQueued(t, s, "4a208dd4-008d-42f3-851e-e136cd1da64a", emailmodel.StatusQueued, future)
	insertQueued(t, s, "2fce3853-c525-415c-86e3-9d7c9a7ab5c3", emailmodel.StatusProcessing, past)
	insertQueued(t, s, "d6decb7d-9e6c-4529-8fd4-c5b221c12bbc", emailmodel.StatusSent, past)

	claimed, err := s.ClaimDue(t.Context(), now, 10)
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]bool{}
	for _, e := range claimed {
		got[e.ID] = true
		if e.Status != emailmodel.StatusProcessing {
			t.Errorf("%s came back with status %q, want %q", e.ID, e.Status, emailmodel.StatusProcessing)
		}

		if e.Attempts != 1 {
			t.Errorf("%s came back with attempts %d, want 1", e.ID, e.Attempts)
		}

		if e.ClaimedAt == nil {
			t.Errorf("%s came back with no claimed_at", e.ID)
		}
	}

	if len(got) != 2 || !got["9f4774e9-f1b4-4904-81eb-e09fc5d838a6"] || !got["3e0097d0-7fcd-43b9-800c-88101ca45476"] {
		t.Fatalf("claimed %v, want exactly due-queued and due-scheduled", idsOf(claimed))
	}
}

func TestClaimDueHonoursTheLimit(t *testing.T) {
	s := newClaimStore(t)
	now := time.Now().UTC()
	for range 5 {
		insertQueued(t, s, ids.New(), emailmodel.StatusQueued, now.Add(-time.Minute))
	}

	claimed, err := s.ClaimDue(t.Context(), now, 2)
	if err != nil {
		t.Fatal(err)
	}

	if len(claimed) != 2 {
		t.Fatalf("claimed %d rows with limit 2: %v", len(claimed), idsOf(claimed))
	}
}

// The claim must step over a row another node is holding rather than
// queue behind it. This is the whole point of SKIP LOCKED: a plain
// FOR UPDATE is equally correct and would make every worker wait on
// the slowest one, so the two are only distinguishable by a test that
// holds a lock open while claiming.
func TestClaimDueSkipsRowsAnotherNodeHolds(t *testing.T) {
	s := newClaimStore(t)
	peer := peerStore(t)
	now := time.Now().UTC()
	past := now.Add(-time.Minute)

	// Ordered by due time so "7c373329-8631-415d-8fb5-c35f7c4b84dc" is the row a claimer reaches first.
	insertQueued(t, s, "7c373329-8631-415d-8fb5-c35f7c4b84dc", emailmodel.StatusQueued, past.Add(-time.Hour))
	insertQueued(t, s, "05ec6695-b80b-48fe-8ba1-4bcadaa7cd23", emailmodel.StatusQueued, past)

	tx, err := peer.DB().BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = tx.Rollback() }()
	var locked string
	//sqlconst:allow constant statement, the test's own fixture
	if err := tx.QueryRowContext(t.Context(),
		`SELECT id FROM emails WHERE id = '7c373329-8631-415d-8fb5-c35f7c4b84dc' FOR UPDATE`).Scan(&locked); err != nil {
		t.Fatal(err)
	}

	// The lock is still held here. Without SKIP LOCKED this call
	// blocks until the deferred rollback, which never runs while the
	// test is stuck inside it - so a regression shows up as a timeout
	// rather than a wrong answer.
	done := make(chan []*emailmodel.Email, 1)
	go func() {
		claimed, cerr := s.ClaimDue(t.Context(), now, 10)
		if cerr != nil {
			t.Errorf("claim while a peer holds a row: %v", cerr)
		}

		done <- claimed
	}()

	select {
	case claimed := <-done:
		if len(claimed) != 1 || claimed[0].ID != "05ec6695-b80b-48fe-8ba1-4bcadaa7cd23" {
			t.Fatalf("claimed %v, want just free - held was locked by the peer", idsOf(claimed))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ClaimDue blocked on a row another node holds, so it is not skipping locked rows")
	}
}

// Two nodes claiming the same queue must partition it. A row handed
// to both would be delivered twice, which is the one failure a mail
// sender cannot take back.
func TestClaimDueNeverHandsOneRowToTwoNodes(t *testing.T) {
	s := newClaimStore(t)
	peer := peerStore(t)
	now := time.Now().UTC()

	const rows = 40
	for range rows {
		insertQueued(t, s, ids.New(), emailmodel.StatusQueued, now.Add(-time.Minute))
	}

	var (
		mu      sync.Mutex
		seen    = map[string]int{}
		total   int
		start   = make(chan struct{})
		wg      sync.WaitGroup
		nodes   = []*Store{s, peer}
		perCall = 7
	)
	for _, node := range nodes {
		wg.Go(func() {
			<-start
			for {
				claimed, err := node.ClaimDue(t.Context(), now, perCall)
				if err != nil {
					t.Errorf("concurrent claim: %v", err)

					return
				}

				if len(claimed) == 0 {
					return
				}

				mu.Lock()
				for _, e := range claimed {
					seen[e.ID]++
					total++
				}

				mu.Unlock()
			}
		})
	}

	close(start)
	wg.Wait()

	if total != rows {
		t.Errorf("claimed %d rows in total, want %d", total, rows)
	}

	for id, n := range seen {
		if n != 1 {
			t.Errorf("%s was claimed %d times, want exactly 1", id, n)
		}
	}

	// Every row claimed exactly once must also have been ATTEMPTED
	// exactly once. The counter drives the retry budget, so a double
	// increment silently halves it.
	var maxAttempts int
	//sqlconst:allow constant statement, the test's own assertion
	if err := s.DB().QueryRowContext(t.Context(),
		`SELECT max(attempts) FROM emails`).Scan(&maxAttempts); err != nil && err != sql.ErrNoRows {
		t.Fatal(err)
	}

	if maxAttempts != 1 {
		t.Errorf("highest attempts is %d, want 1", maxAttempts)
	}
}
