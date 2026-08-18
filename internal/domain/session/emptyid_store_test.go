// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package session

import (
	"context"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	smodel "github.com/yousysadmin/mailyard/internal/models/session"
)

// seedSessions gives one user three live sessions and returns their ids.
func seedSessions(t *testing.T, s *Store, ctx context.Context) (userID string, sessions []string) {
	t.Helper()

	userID = ids.New()
	if _, err := s.DB().ExecContext(ctx, `
		INSERT INTO users (id, email, account_type, created_at)
		VALUES ($1, $2, 1, now())`, userID, userID+"@x.test"); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	now := time.Now().UTC()
	for range 3 {
		m := &smodel.Session{
			ID: ids.New(), UserID: userID, UserAgent: "curl", IP: "10.0.0.1",
			ExpiresAt: now.Add(time.Hour),
		}
		if err := s.Put(ctx, m); err != nil {
			t.Fatalf("seed session: %v", err)
		}

		sessions = append(sessions, m.ID)
	}

	return userID, sessions
}

func liveCount(t *testing.T, s *Store, ctx context.Context, userID string) int {
	t.Helper()

	rows, err := s.ListForUser(ctx, userID, time.Now().UTC())
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	return len(rows)
}

// RevokeOthers with no session to keep, which is a REACHABLE state.
//
// The empty path, taken on purpose - the rule tests/nulluuid_test.go
// settled on rather than a static allowlist, because nothing static
// reaches this: `id <> ?` on a uuid column PREPAREs, so the schema guard
// is satisfied, and it is a non-null primary key so the null-uuid guard
// does not cover it either.
//
// Reachable because a token with no jti predates session tracking and is
// deliberately ACCEPTED - which leaves rc.SessionID empty. So an
// operator holding one of those cookies pressed "sign out everywhere",
// the statement answered 22P02, MalformedID softened it to a 404, and the
// page reported that there was nothing to revoke while every other
// session stayed live. The exact person most likely to want it, and the
// one control they most needed to work.
func TestRevokingOtherSessionsWithNoSessionToKeep(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := context.Background()

	userID, _ := seedSessions(t, s, ctx)
	if got := liveCount(t, s, ctx, userID); got != 3 {
		t.Fatalf("%d live sessions, want 3", got)
	}

	// No jti to keep: every one of them goes.
	n, err := s.RevokeOthers(ctx, userID, "")
	if err != nil {
		t.Fatalf("with no session to keep: %v", err)
	}

	if n != 3 {
		t.Errorf("revoked %d, want all 3 - a caller whose token carries no jti got "+
			"a 404 and kept every session", n)
	}

	if got := liveCount(t, s, ctx, userID); got != 0 {
		t.Errorf("%d live sessions left, want 0", got)
	}
}

// And the ordinary case still keeps exactly the caller's own.
func TestRevokingOtherSessionsKeepsTheCallersOwn(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := context.Background()

	userID, sessions := seedSessions(t, s, ctx)
	keep := sessions[1]

	n, err := s.RevokeOthers(ctx, userID, keep)
	if err != nil {
		t.Fatalf("revoke others: %v", err)
	}

	if n != 2 {
		t.Errorf("revoked %d, want 2", n)
	}

	left, err := s.ListForUser(ctx, userID, time.Now().UTC())
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if len(left) != 1 || left[0].ID != keep {
		t.Errorf("left %d sessions, want only the caller's own (%s)", len(left), keep)
	}

	// Another user's sessions are untouched, which is the tenancy half.
	other, _ := seedSessions(t, s, ctx)
	if _, err := s.RevokeOthers(ctx, userID, ""); err != nil {
		t.Fatalf("second revoke: %v", err)
	}

	if got := liveCount(t, s, ctx, other); got != 3 {
		t.Errorf("another account lost %d sessions", 3-got)
	}
}
