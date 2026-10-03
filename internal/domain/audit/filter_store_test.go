// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package audit

import (
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	amodel "github.com/yousysadmin/mailyard/internal/models/audit"
)

// The type filter matches regardless of the case it is written in.
func TestTheTypeFilterIgnoresCase(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := NewStore(db)
	ctx := t.Context()

	if err := s.Put(ctx, &amodel.Event{ID: ids.New(), Category: amodel.CategorySecurity, Type: "auth.login.failed"}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	for _, typ := range []string{"auth.login.failed", "AUTH.Login.Failed"} {
		got, err := s.ListSecurity(ctx, store.AuditFilter{Type: typ, Limit: 10})
		if err != nil || len(got) != 1 {
			t.Errorf("type %q: %d rows, %v, want the event", typ, len(got), err)
		}
	}
}
