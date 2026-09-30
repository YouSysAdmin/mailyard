// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package subscriberlist

import (
	"database/sql"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	slmodel "github.com/yousysadmin/mailyard/internal/models/subscriberlist"
)

func seedProjectAndSubscriber(t *testing.T, db *sql.DB) (project, sub string) {
	t.Helper()
	project, sub = ids.New(), ids.New()
	if _, err := db.ExecContext(t.Context(), `
		INSERT INTO projects (id, name, slug, created_at) VALUES ($1, 'p', $2, now())`, project, project); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	if _, err := db.ExecContext(t.Context(), `
		INSERT INTO subscribers (id, project_id, email, created_at)
		VALUES ($1, $2, 'sub@example.invalid', now())`, sub, project); err != nil {
		t.Fatalf("insert subscriber: %v", err)
	}

	return project, sub
}

func seedList(t *testing.T, db *sql.DB, project, kind string) string {
	t.Helper()
	list := ids.New()
	if _, err := db.ExecContext(t.Context(), `
		INSERT INTO subscriber_lists (id, project_id, name, type, created_at)
		VALUES ($1, $2, $3, $4, now())`, list, project, kind+" list", kind); err != nil {
		t.Fatalf("insert %s list: %v", kind, err)
	}

	return list
}

// A member's per-list opt-out rides on the member row, the list counts
// it, and lifting it clears all three.
func TestAMemberCarriesTheirOptOut(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := NewStore(db)
	ctx := t.Context()
	project, sub := seedProjectAndSubscriber(t, db)
	list := seedList(t, db, project, slmodel.TypeStatic)
	if err := s.AddMember(ctx, project, list, sub); err != nil {
		t.Fatalf("add member: %v", err)
	}

	members, err := s.ListMembers(ctx, project, list, 10, 0)
	if err != nil || len(members) != 1 || members[0].OptedOutAt != nil {
		t.Fatalf("before: members=%+v err=%v, want one with no opt-out", members, err)
	}

	if err := s.Unsubscribe(ctx, project, list, sub, "the footer link"); err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}

	members, err = s.ListMembers(ctx, project, list, 10, 0)
	if err != nil || len(members) != 1 || members[0].OptedOutAt == nil || members[0].OptOutReason != "the footer link" {
		t.Fatalf("after: members=%+v err=%v, want the opt-out on the row", members, err)
	}

	if n, err := s.CountOptedOut(ctx, project, list); err != nil || n != 1 {
		t.Errorf("CountOptedOut = %d, err %v, want 1", n, err)
	}

	if out, err := s.ListOptedOut(ctx, project, list, 10, 0); err != nil || len(out) != 1 || out[0].Email != "sub@example.invalid" {
		t.Errorf("ListOptedOut = %+v, err %v, want the subscriber", out, err)
	}

	if err := s.Resubscribe(ctx, project, list, sub); err != nil {
		t.Fatalf("resubscribe: %v", err)
	}

	if n, err := s.CountOptedOut(ctx, project, list); err != nil || n != 0 {
		t.Errorf("CountOptedOut after resubscribe = %d, err %v, want 0", n, err)
	}
}

// A dynamic list has no members, so its opt-outs are reachable only
// through ListOptedOut and through the subscriber's own list of lists,
// where the segment appears as opted out and not a member.
func TestADynamicListShowsItsOptOuts(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := NewStore(db)
	ctx := t.Context()
	project, sub := seedProjectAndSubscriber(t, db)
	segment := seedList(t, db, project, slmodel.TypeDynamic)
	static := seedList(t, db, project, slmodel.TypeStatic)
	if err := s.AddMember(ctx, project, static, sub); err != nil {
		t.Fatalf("add member: %v", err)
	}

	if err := s.Unsubscribe(ctx, project, segment, sub, "one-click"); err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}

	out, err := s.ListOptedOut(ctx, project, segment, 10, 0)
	if err != nil || len(out) != 1 {
		t.Fatalf("ListOptedOut = %+v, err %v, want one row", out, err)
	}

	lists, err := s.ListsOf(ctx, project, sub)
	if err != nil {
		t.Fatalf("ListsOf: %v", err)
	}

	byID := map[string]bool{}
	for _, l := range lists {
		byID[l.ID] = true
		switch l.ID {
		case segment:
			if l.Member || l.OptedOutAt == nil {
				t.Errorf("segment: member=%v optedOut=%v, want opted out and not a member", l.Member, l.OptedOutAt)
			}
		case static:
			if !l.Member || l.OptedOutAt != nil {
				t.Errorf("static: member=%v optedOut=%v, want a plain member", l.Member, l.OptedOutAt)
			}
		}
	}

	if !byID[segment] || !byID[static] {
		t.Errorf("ListsOf = %d lists, want both the segment and the static list", len(lists))
	}

	// Another project sees none of it.
	other := ids.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, created_at) VALUES ($1, 'q', $2, now())`, other, other); err != nil {
		t.Fatalf("insert other project: %v", err)
	}

	if out, err := s.ListOptedOut(ctx, other, segment, 10, 0); err != nil || len(out) != 0 {
		t.Errorf("another project's ListOptedOut = %+v, err %v, want nothing", out, err)
	}
}
