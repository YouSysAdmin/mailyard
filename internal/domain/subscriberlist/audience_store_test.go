// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package subscriberlist

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	slmodel "github.com/yousysadmin/mailyard/internal/models/subscriberlist"
)

// An opt-out stays part of the audience, apart from the recipients, so
// the campaign can record it as skipped. IsOptedOut answers the same
// question for one subscriber, which is what the runner asks as it
// delivers.
func TestTheAudienceKeepsItsOptOutsApart(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := NewStore(db)
	ctx := t.Context()
	project, sub := seedProjectAndSubscriber(t, db)
	list := seedList(t, db, project, slmodel.TypeStatic)
	if err := s.AddMember(ctx, project, list, sub); err != nil {
		t.Fatalf("add member: %v", err)
	}

	l, err := s.Get(ctx, project, list)
	if err != nil || l == nil {
		t.Fatalf("get list: %v", err)
	}

	if opted, err := s.IsOptedOut(ctx, project, list, sub); err != nil || opted {
		t.Fatalf("before: opted=%v err=%v", opted, err)
	}

	if err := s.Unsubscribe(ctx, project, list, sub, "footer"); err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}

	if opted, err := s.IsOptedOut(ctx, project, list, sub); err != nil || !opted {
		t.Errorf("after: opted=%v err=%v, want true", opted, err)
	}

	if opted, _ := s.IsOptedOut(ctx, ids.New(), list, sub); opted {
		t.Error("another project saw the opt-out")
	}

	recipients, optedOut, err := s.ResolveAudience(ctx, nil, project, l)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if len(recipients) != 0 || len(optedOut) != 1 || optedOut[0].ID != sub {
		t.Errorf("recipients=%d optedOut=%+v, want the subscriber apart", len(recipients), optedOut)
	}
}

// An opt-out naming a subscriber that is gone is no row, not a foreign
// key failure the hosted page turned into a 500.
func TestAnOptOutForADeletedSubscriberIsNoRow(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := NewStore(db)
	ctx := t.Context()
	project, _ := seedProjectAndSubscriber(t, db)
	list := seedList(t, db, project, slmodel.TypeStatic)

	if err := s.Unsubscribe(ctx, project, list, ids.New(), "footer"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("err = %v, want sql.ErrNoRows", err)
	}
}

// Find answers the whole list without a limit and a window with one,
// with the total either way.
func TestFindPagesOnRequest(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := NewStore(db)
	ctx := t.Context()
	project, _ := seedProjectAndSubscriber(t, db)
	for range 3 {
		seedList(t, db, project, slmodel.TypeStatic)
	}

	all, total, err := s.Find(ctx, project, 0, 0)
	if err != nil || len(all) != 3 || total != 3 {
		t.Fatalf("whole: %d rows, total %d, err %v", len(all), total, err)
	}

	page, total, err := s.Find(ctx, project, 2, 2)
	if err != nil || len(page) != 1 || total != 3 {
		t.Errorf("page: %d rows, total %d, err %v, want 1 of 3", len(page), total, err)
	}
}
