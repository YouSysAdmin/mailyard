// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package suppression

import (
	"slices"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	supmodel "github.com/yousysadmin/mailyard/internal/models/suppression"
)

// The send-time filter: a recipient is blocked by a global row or by
// an opt-out of the list the send is scoped to, matched on the bare
// lowercased address whatever form the caller wrote, and returned in
// the form the caller wrote. Another project's rows block nothing.
func TestTheSendFilterBlocksWhatTheRowsSay(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := t.Context()

	proj, listA, listB := seedScopes(t, s, ctx, "both@example.com")
	if err := s.Upsert(ctx, &supmodel.Suppression{
		ProjectID: proj, Email: "global@example.com", Kind: supmodel.KindComplaint,
	}); err != nil {
		t.Fatal(err)
	}

	if err := s.Upsert(ctx, &supmodel.Suppression{
		ProjectID: proj, Email: "onlya@example.com", Kind: supmodel.KindListUnsubscribe, UnsubscribeListID: listA,
	}); err != nil {
		t.Fatal(err)
	}

	other, _, _ := seedScopes(t, s, ctx, "elsewhere@example.com")
	_ = other

	in := []string{
		`"Global" <Global@Example.com>`,
		"ONLYA@example.com",
		"elsewhere@example.com",
		"clean@example.com",
		"both@example.com",
		// Probes: none of these is a stored address, so none may match.
		"%@example.com",
		"x' OR '1'='1@example.com",
		"_oth@example.com",
	}

	cases := []struct {
		name, list string
		blocked    []string
	}{
		{"unscoped", "", []string{`"Global" <Global@Example.com>`, "both@example.com"}},
		{"list A", listA, []string{`"Global" <Global@Example.com>`, "ONLYA@example.com", "both@example.com"}},
		{"list B", listB, []string{`"Global" <Global@Example.com>`, "both@example.com"}},
		{"unknown list", ids.New(), []string{`"Global" <Global@Example.com>`, "both@example.com"}},
	}
	for _, c := range cases {
		allowed, blocked, err := s.FilterSuppressedForList(ctx, proj, c.list, in)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}

		if !slices.Equal(blocked, c.blocked) {
			t.Errorf("%s: blocked %q, want %q", c.name, blocked, c.blocked)
		}

		if len(allowed)+len(blocked) != len(in) {
			t.Errorf("%s: %d allowed and %d blocked out of %d", c.name, len(allowed), len(blocked), len(in))
		}

		for _, a := range allowed {
			if slices.Contains(blocked, a) {
				t.Errorf("%s: %q is both allowed and blocked", c.name, a)
			}
		}
	}

	allowed, blocked, err := s.FilterSuppressed(ctx, proj, nil)
	if err != nil || len(allowed) != 0 || len(blocked) != 0 {
		t.Fatalf("no recipients: %q %q %v", allowed, blocked, err)
	}
}
