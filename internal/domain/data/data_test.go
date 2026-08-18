// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package data

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/domain/store"
	supmodel "github.com/yousysadmin/mailyard/internal/models/suppression"
)

// clampingSuppressions mimics the real store's listLimit backstop:
// a limit outside 1..201 is answered with 51 rows, not refused.
type clampingSuppressions struct {
	store.SuppressionStore
	rows []*supmodel.Suppression
}

// List pages s.rows the way the real store does, clamp included.
func (s *clampingSuppressions) List(_ context.Context, _ string, f store.SuppressionFilter) ([]*supmodel.Suppression, error) {
	limit := f.Limit
	if limit < 1 || limit > 201 {
		limit = 51
	}

	start := 0
	if !f.Cursor.CreatedAt.IsZero() {
		for i, r := range s.rows {
			if r.CreatedAt.Before(f.Cursor.CreatedAt) {
				start = i

				break
			}
		}
	}

	end := min(start+limit, len(s.rows))

	return s.rows[start:end], nil
}

// TestExportWalksEverySuppression pins the walker's page size to the
// store's backstop. It asked for 500 once: the store silently answered
// 51, the short-page test read that as the end, and an export carried
// at most 51 suppressions with Truncated staying empty.
func TestExportWalksEverySuppression(t *testing.T) {
	const total = 700
	fake := &clampingSuppressions{}
	base := time.Now().UTC()
	for i := range total {
		fake.rows = append(fake.rows, &supmodel.Suppression{
			ID:        fmt.Sprintf("row-%04d", i),
			Email:     fmt.Sprintf("u%d@example.test", i),
			CreatedAt: base.Add(-time.Duration(i) * time.Second),
		})
	}

	st := &store.Store{Suppression: fake}
	rows, cut, err := allSuppressions(t.Context(), st, "proj")
	if err != nil {
		t.Fatalf("allSuppressions: %v", err)
	}

	if cut {
		t.Fatal("truncated below the export cap")
	}

	if len(rows) != total {
		t.Fatalf("exported %d of %d suppressions", len(rows), total)
	}
}
