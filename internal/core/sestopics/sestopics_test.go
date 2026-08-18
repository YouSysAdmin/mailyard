// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package sestopics

import (
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/database/dbtest"
)

func testAllowlist(t *testing.T) *Allowlist {
	t.Helper()
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)

	return NewAllowlist(db)
}

// smtp_servers.project_id is a real foreign key - that constraint is
// the tenancy guarantee, so the fixture makes a real project rather
// than working around it.
func addProjectServer(t *testing.T, a *Allowlist, arn string) {
	t.Helper()
	projID := ids.New()
	if _, err := a.Exec(t.Context(), `
		INSERT INTO projects (id, name, slug, owner_id, created_at, updated_at)
		VALUES (?, 'test', ?, NULL, now(), now())`, projID, projID); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	if _, err := a.Exec(t.Context(), `
		INSERT INTO smtp_servers (id, project_id, name, host, port, group_id, ses_topic_arn)
		VALUES (?, ?, 'srv', 'smtp.example.com', 587, NULL, ?)`,
		ids.New(), projID, arn); err != nil {
		t.Fatalf("insert server: %v", err)
	}
}

func addSharedServer(t *testing.T, a *Allowlist, arn string) {
	t.Helper()
	_, err := a.Exec(t.Context(), `
		INSERT INTO shared_smtp_servers (id, name, host, port, ses_topic_arn)
		VALUES (?, 'pool', 'smtp.example.com', 587, ?)`,
		ids.New(), arn)
	if err != nil {
		t.Fatalf("insert shared server: %v", err)
	}
}

// With nothing configured the endpoint accepts nothing. That is what
// lets it exist unconditionally: it gates itself on the data instead
// of on a config key that could disagree with it.
func TestNothingConfiguredAcceptsNothing(t *testing.T) {
	a := testAllowlist(t)
	for _, arn := range []string{"", "arn:aws:sns:eu-west-1:1:anything"} {
		ok, err := a.Allowed(t.Context(), arn)
		if err != nil {
			t.Fatalf("Allowed: %v", err)
		}

		if ok {
			t.Errorf("%q was accepted with no server carrying a topic", arn)
		}
	}
}

// A tenant's own SES server and a platform-owned one are the same
// thing to SES, so both tables count.
func TestBothServerTablesCount(t *testing.T) {
	a := testAllowlist(t)
	addProjectServer(t, a, "arn:project")
	addSharedServer(t, a, "arn:shared")

	for _, arn := range []string{"arn:project", "arn:shared"} {
		ok, err := a.Allowed(t.Context(), arn)
		if err != nil {
			t.Fatalf("Allowed: %v", err)
		}

		if !ok {
			t.Errorf("%q was refused", arn)
		}
	}

	ok, err := a.Allowed(t.Context(), "arn:somebody-else")
	if err != nil {
		t.Fatalf("Allowed: %v", err)
	}

	if ok {
		t.Error("an unconfigured topic was accepted")
	}
}

// An empty column is a server that does not use SES, not a wildcard.
// Selecting it would make every ordinary server admit the empty ARN.
func TestServersWithoutATopicDoNotWiden(t *testing.T) {
	a := testAllowlist(t)
	addProjectServer(t, a, "")
	addSharedServer(t, a, "")

	if ok, _ := a.Allowed(t.Context(), ""); ok {
		t.Error("the empty topic was accepted")
	}

	if ok, _ := a.Allowed(t.Context(), "arn:anything"); ok {
		t.Error("a server with no topic widened the allowlist")
	}
}

// The whole reason this is cached: the endpoint is public and
// unauthenticated, so a query per request is load anyone can generate.
func TestRepeatedChecksDoNotRequery(t *testing.T) {
	a := testAllowlist(t)
	addProjectServer(t, a, "arn:project")

	if ok, _ := a.Allowed(t.Context(), "arn:project"); !ok {
		t.Fatal("first check failed")
	}

	// Remove it behind the cache. A cached answer must still be given
	// until the TTL, which is what proves no query happened.
	if _, err := a.Exec(t.Context(), `DELETE FROM smtp_servers`); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if ok, _ := a.Allowed(t.Context(), "arn:project"); !ok {
		t.Error("the second check went back to the database")
	}
}

// An operator pasting an ARN should not watch notifications be
// refused for a minute with no explanation.
func TestInvalidateTakesEffectAtOnce(t *testing.T) {
	a := testAllowlist(t)
	if ok, _ := a.Allowed(t.Context(), "arn:new"); ok {
		t.Fatal("accepted before anything was configured")
	}

	addProjectServer(t, a, "arn:new")
	if ok, _ := a.Allowed(t.Context(), "arn:new"); ok {
		t.Error("the cache was bypassed without Invalidate, so the TTL does nothing")
	}

	a.Invalidate()
	if ok, _ := a.Allowed(t.Context(), "arn:new"); !ok {
		t.Error("a saved topic was still refused after Invalidate")
	}
}

func TestTheCacheExpires(t *testing.T) {
	a := testAllowlist(t)
	now := time.Unix(1700000000, 0)
	a.now = func() time.Time { return now }

	if ok, _ := a.Allowed(t.Context(), "arn:late"); ok {
		t.Fatal("accepted before anything was configured")
	}

	addProjectServer(t, a, "arn:late")

	now = now.Add(2 * time.Minute)
	if ok, _ := a.Allowed(t.Context(), "arn:late"); !ok {
		t.Error("the cache never expired")
	}
}

// A read failure is not evidence that a topic is ours. Answering an
// authorization question from a stale map would be answering it with
// a guess.
func TestAReadFailureIsAnError(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	a := NewAllowlist(db)
	_ = db.Close()

	if _, err := a.Allowed(t.Context(), "arn:project"); err == nil {
		t.Error("a database failure was reported as a clean answer")
	}
}
