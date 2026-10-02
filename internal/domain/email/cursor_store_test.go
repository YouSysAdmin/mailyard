// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"slices"
	"testing"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/core/keyset"
	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
	"github.com/yousysadmin/mailyard/internal/domain/store"
)

// Paging the log must not lose a message.
//
// The cursor was created_at ALONE, and two messages can share one: the
// column is microsecond precision and a batch mints its rows in a tight
// loop. When the tie lands across a page boundary, the next page asks for
// `created_at < that instant` and every row holding it is skipped - not
// duplicated, skipped. They appear on neither page, so the log does not
// contain a message that was sent, and nothing about the result says so.
//
// Six rows sharing one timestamp, paged three at a time. The house rule
// this asserts is written down: the cursor is `(created_at, id)`, never
// created_at alone.
func TestPagingTheLogDoesNotSkipMessagesThatShareATimestamp(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := t.Context()

	projID := ids.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, owner_id, created_at)
		VALUES ($1, 'acme', $2, NULL, now())`, projID, projID); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	// One instant, six messages - a batch send.
	at := time.Now().UTC().Truncate(time.Microsecond)
	want := map[string]bool{}
	for range 6 {
		id := ids.New()
		want[id] = true
		if _, err := db.ExecContext(ctx, `
			INSERT INTO emails (id, project_id, sender, recipients, subject, status, created_at)
			VALUES ($1, $2, 'from@x.test', '["to@y.test"]', 's', 'sent', $3)`,
			id, projID, at); err != nil {
			t.Fatalf("seed email: %v", err)
		}
	}

	seen := map[string]bool{}
	f := store.EmailFilter{Limit: 3}
	for page := range 5 {
		rows, err := s.List(ctx, projID, f)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}

		if len(rows) == 0 {
			break
		}

		for _, e := range rows {
			if seen[e.ID] {
				t.Errorf("page %d returned %s again - the cursor is not advancing", page, e.ID)
			}

			seen[e.ID] = true
		}

		last := rows[len(rows)-1]
		f.Cursor = keyset.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}

	if len(seen) != len(want) {
		t.Fatalf("paged over %d of %d messages - the rest share a created_at with a page "+
			"boundary and were skipped by both pages", len(seen), len(want))
	}

	for id := range want {
		if !seen[id] {
			t.Errorf("message %s appeared on no page", id)
		}
	}
}

// Every filter narrows the same statement, and the address filters
// match a whole address without regard to case: a log search that
// misses Bob@x.test for bob@x.test reads as "never sent".
func TestTheLogFiltersNarrowTogether(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := t.Context()

	projID := ids.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, owner_id, created_at)
		VALUES ($1, 'acme', $2, NULL, now())`, projID, projID); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	base := time.Now().UTC().Truncate(time.Microsecond)
	seed := func(sender, recipients, status, template string, at time.Time) string {
		id := ids.New()
		if _, err := db.ExecContext(ctx, `
			INSERT INTO emails (id, project_id, sender, recipients, subject, status, template_name, created_at)
			VALUES ($1, $2, $3, $4, 'Receipt', $5, $6, $7)`,
			id, projID, sender, recipients, status, template, at); err != nil {
			t.Fatalf("seed email: %v", err)
		}

		return id
	}

	old := seed(`"Shop" <Shop@Example.test>`, `["Bob Smith <Bob@x.test>"]`, "sent", "receipt", base.Add(-2*time.Hour))
	failed := seed("shop@example.test", `["ann@x.test","bob@x.test"]`, "failed", "receipt", base.Add(-time.Hour))
	other := seed("ops@example.test", `["carol@x.test","notbob@x.test"]`, "sent", "", base)

	cases := []struct {
		name string
		f    store.EmailFilter
		want []string
	}{
		{"sender substring, any case", store.EmailFilter{Sender: "SHOP"}, []string{failed, old}},
		{"exact sender, bare and inside a mailbox", store.EmailFilter{Sender: "SHOP@example.test", Exact: true}, []string{failed, old}},
		{"exact sender refuses a prefix", store.EmailFilter{Sender: "shop@", Exact: true}, nil},
		{"recipient substring", store.EmailFilter{Recipient: "bob"}, []string{other, failed, old}},
		{"exact recipient, bare and inside a mailbox", store.EmailFilter{Recipient: "BOB@x.test", Exact: true}, []string{failed, old}},
		{"exact recipient refuses a superstring", store.EmailFilter{Recipient: "ob@x.test", Exact: true}, nil},
		{"two statuses", store.EmailFilter{Statuses: []string{"failed", "queued"}}, []string{failed}},
		{"template", store.EmailFilter{Template: "receipt"}, []string{failed, old}},
		{"window", store.EmailFilter{From: ptr(base.Add(-90 * time.Minute)), To: ptr(base)}, []string{failed}},
		{"after", store.EmailFilter{After: ptr(base.Add(-time.Hour))}, []string{other}},
		{"search ignores case", store.EmailFilter{Search: "ANN@X.TEST"}, []string{failed}},
		{"combined", store.EmailFilter{Sender: "shop@example.test", Exact: true, Statuses: []string{"sent"}}, []string{old}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := s.List(ctx, projID, tc.f)
			if err != nil {
				t.Fatalf("list: %v", err)
			}

			var got []string
			for _, e := range rows {
				got = append(got, e.ID)
			}

			if !slices.Equal(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }
