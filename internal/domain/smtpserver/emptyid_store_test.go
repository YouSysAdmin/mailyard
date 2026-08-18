// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpserver

import (
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

// SlugTaken with no exception id, which is what a CREATE passes.
//
// This is the empty path, and taking it is the only thing that catches
// this class - the house rule written down in tests/nulluuid_test.go,
// which chose exercising the absent value over a static allowlist for
// exactly this reason. Neither of the repo-wide guards can see it: the
// statement PREPAREs perfectly, so the schema guard is satisfied, and
// the null-uuid guard covers NULLABLE columns while `id` here is a
// non-null primary key.
//
// What it cost: `id <> ?` against a uuid column with an empty string is
// 22P02, whose pgconn Routine is string_to_uuid - precisely what
// database.MalformedID matches, so response.Internal softened it into a
// 404 "not found". Every attempt to create an SMTP server group answered
// 404 before inserting anything, on both surfaces, and the log recorded a
// warn rather than an error. The UPDATE path passes a real id and worked
// throughout, which is why nothing noticed.
func TestSlugTakenWorksWithNoExceptionID(t *testing.T) {
	s, proj, ctx := groupStore(t)

	// Nothing there yet: the create path's first question.
	taken, err := s.SlugTaken(ctx, proj, "bulk", "")
	if err != nil {
		t.Fatalf("with no exception id: %v", err)
	}

	if taken {
		t.Error("an empty project reported the slug as taken")
	}

	g := &ssmodel.Group{ID: ids.New(), ProjectID: proj, Name: "Bulk", Slug: "bulk"}
	if err := s.Put(ctx, g); err != nil {
		t.Fatalf("put: %v", err)
	}

	// Now it is, still with no exception id.
	if taken, err := s.SlugTaken(ctx, proj, "bulk", ""); err != nil || !taken {
		t.Errorf("after the insert: taken=%v err=%v", taken, err)
	}

	// And the update path: a group keeps its own slug.
	if taken, err := s.SlugTaken(ctx, proj, "bulk", g.ID); err != nil || taken {
		t.Errorf("excepting the holder itself: taken=%v err=%v", taken, err)
	}

	// Another project's identical slug is not a collision.
	other := ids.New()
	if _, err := s.DB().ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, owner_id, created_at)
		VALUES ($1, 'other', $2, NULL, now())`, other, other); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	if taken, err := s.SlugTaken(ctx, other, "bulk", ""); err != nil || taken {
		t.Errorf("another project's slug: taken=%v err=%v", taken, err)
	}
}
