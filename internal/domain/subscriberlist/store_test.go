// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package subscriberlist

import (
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
)

// RemoveMember says whether the subscriber was on the list, so the
// route can answer a missing or foreign one with a 404.
func TestRemoveMemberReportsWhetherTheSubscriberWasOnTheList(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := NewStore(db)
	ctx := t.Context()

	mine, theirs, list, sub := ids.New(), ids.New(), ids.New(), ids.New()
	for _, p := range []string{mine, theirs} {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO projects (id, name, slug, created_at) VALUES ($1, 'p', $2, now())`, p, p); err != nil {
			t.Fatalf("insert project: %v", err)
		}
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO subscriber_lists (id, project_id, name, type, created_at)
		VALUES ($1, $2, 'list', 'static', now())`, list, mine); err != nil {
		t.Fatalf("insert list: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO subscribers (id, project_id, email, created_at)
		VALUES ($1, $2, 'sub@example.invalid', now())`, sub, mine); err != nil {
		t.Fatalf("insert subscriber: %v", err)
	}

	if err := s.AddMember(ctx, mine, list, sub); err != nil {
		t.Fatalf("add member: %v", err)
	}

	if removed, err := s.RemoveMember(ctx, theirs, list, sub); err != nil || removed {
		t.Fatalf("another project's remove: removed=%v err=%v, want false", removed, err)
	}

	if removed, err := s.RemoveMember(ctx, mine, list, sub); err != nil || !removed {
		t.Fatalf("remove: removed=%v err=%v, want true", removed, err)
	}

	if removed, err := s.RemoveMember(ctx, mine, list, sub); err != nil || removed {
		t.Fatalf("a second remove: removed=%v err=%v, want false", removed, err)
	}
}
