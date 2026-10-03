// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package language

import (
	"fmt"
	"sync"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	lmodel "github.com/yousysadmin/mailyard/internal/models/language"
)

// Two writes marking different languages default at once each cleared
// the flag before either set it, and both stayed marked. Two nodes
// race here, each on a connection of its own.
func TestOnlyOneLanguageIsDefaultUnderConcurrency(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)

	const projID = "e66e7a4d-9e6c-4884-869a-cf9ffcf22181"
	dbtest.Schema(t, db, `
        INSERT INTO projects (id, name, slug, default_language, created_at)
        VALUES ('`+projID+`', 'Test', 'test', 'en', now())`)

	stores := []*Store{NewStore(db), NewStore(dbtest.Peer(t))}
	ctx := t.Context()

	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for n, s := range stores {
		wg.Go(func() {
			for i := range 20 {
				l := &lmodel.Language{
					ID: ids.New(), ProjectID: projID, Code: fmt.Sprintf("l%d%d", n, i), Name: "x", IsDefault: true,
				}
				if err := s.Put(ctx, l); err != nil {
					errs <- err
				}
			}
		})
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("put: %v", err)
	}

	var defaults int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM languages WHERE is_default`).Scan(&defaults); err != nil {
		t.Fatal(err)
	}

	if defaults != 1 {
		t.Errorf("%d defaults, want 1", defaults)
	}
}
