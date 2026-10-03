// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"sync"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
)

// Twenty sends racing a limit of seven admit exactly seven. Each sender
// is a node of its own, with its own connection, or the race would be
// sequential and prove nothing.
func TestAVolumeLimitHoldsUnderConcurrentSends(t *testing.T) {
	s := newClaimStore(t)
	const (
		projID = "e66e7a4d-9e6c-4884-869a-cf9ffcf22181"
		limit  = 7
		racers = 20
	)

	nodes := make([]*Store, racers)
	for i := range nodes {
		nodes[i] = &Store{Base: database.NewBase(dbtest.Peer(t))}
	}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		accepted int
		refused  int
	)

	start := make(chan struct{})
	for _, node := range nodes {
		wg.Go(func() {
			<-start
			row := &emailmodel.Email{
				ID: ids.New(), ProjectID: projID, Sender: "a@b.test", Recipients: []string{"c@d.test"},
				Subject: "s", TextBody: "t", Status: emailmodel.StatusQueued, MaxAttempts: 3,
			}

			window, err := node.PutWithin(t.Context(), row, 0, limit)
			mu.Lock()
			defer mu.Unlock()

			switch {
			case err != nil:
				t.Errorf("PutWithin: %v", err)
			case window == "":
				accepted++
			default:
				refused++
			}
		})
	}

	close(start)
	wg.Wait()

	if accepted != limit || refused != racers-limit {
		t.Errorf("accepted %d and refused %d, want %d and %d", accepted, refused, limit, racers-limit)
	}

	var rows, counted int
	if err := s.QueryRow(t.Context(), `SELECT COUNT(*) FROM emails WHERE project_id = ?`, projID).Scan(&rows); err != nil {
		t.Fatal(err)
	}

	if err := s.QueryRow(t.Context(),
		`SELECT COALESCE(SUM(accepted), 0) FROM email_volume WHERE project_id = ?`, projID).Scan(&counted); err != nil {
		t.Fatal(err)
	}

	if rows != limit || counted != limit {
		t.Errorf("%d rows and a volume count of %d, want %d of each", rows, counted, limit)
	}
}
