// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"context"
	"testing"

	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
)

// Erasure has to find the address however the mailbox was stored.
//
// A predicate of `LOWER(sender) = ?` plus a quoted needle over the
// recipients array assumes both columns hold bare addresses. They do
// not: withRegisteredName stores `"Acme" <no-reply@acme.com>` whenever
// the project registered a name for the address, and a recipient is
// stored exactly as the caller sent it.
// So an erasure request answered 200 with deleted:0 and the subject's
// mail stayed - along with the attachment blobs, because the key query
// carried the same predicate.
//
// The cases below are the shapes the send path actually produces, not
// invented ones.
func TestErasureFindsEveryStoredMailboxShape(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := context.Background()

	projID := ids.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, owner_id, created_at)
		VALUES ($1, 'acme', $2, NULL, now())`, projID, projID); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	const target = "bob@x.test"
	var want int
	for _, tc := range []struct {
		what      string
		sender    string
		recipient string
		involves  bool
	}{
		{"bare sender", target, "z@y.test", true},
		{"sender with a registered display name", `"Acme" <bob@x.test>`, "z@y.test", true},
		{"recipient with a display name", "q@y.test", "Bob Smith <bob@x.test>", true},
		{"recipient in mixed case", "q@y.test", "BOB@X.test", true},
		// Neither of these involves the subject, and the anchoring is
		// what keeps them out: a bare substring match would take both.
		{"a different address", "rob@x.test", "rob@x.test", false},
		{"an address containing it", "q@y.test", "notbob@x.test", false},
	} {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO emails (id, project_id, sender, recipients, subject, status, created_at)
			VALUES ($1, $2, $3, $4, 's', 'sent', now())`,
			ids.New(), projID, tc.sender, `["`+tc.recipient+`"]`); err != nil {
			t.Fatalf("%s: seed: %v", tc.what, err)
		}

		if tc.involves {
			want++
		}
	}

	deleted, err := s.PurgeForAddress(ctx, projID, target)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}

	if int(deleted) != want {
		t.Errorf("deleted %d rows, want %d - a stored mailbox shape was missed", deleted, want)
	}

	// The two rows that do not involve the subject must survive.
	var left int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM emails WHERE project_id = $1`, projID).Scan(&left); err != nil {
		t.Fatalf("count: %v", err)
	}

	if left != 2 {
		t.Errorf("%d rows left, want the 2 unrelated ones - erasure took somebody else's mail", left)
	}
}

// The key query and the DELETE must select the same rows, or a blob is
// dropped under a live row (or left with nothing naming it). They share
// addressMatchClause for that reason, and this is the assertion behind it.
func TestTheKeySetMatchesWhatErasureDeletes(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db)
	s := &Store{Base: database.NewBase(db)}
	ctx := context.Background()

	projID := ids.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, slug, owner_id, created_at)
		VALUES ($1, 'acme', $2, NULL, now())`, projID, projID); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	const target = "bob@x.test"
	seed := func(sender, recipient, status, attachments string) {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO emails (id, project_id, sender, recipients, subject, status,
			                    created_at, attachments_json)
			VALUES ($1, $2, $3, $4, 's', $5, now(), $6)`,
			ids.New(), projID, sender, `["`+recipient+`"]`, status, attachments); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	// Two blob-bearing rows that will go, one blob-bearing row that is
	// IN FLIGHT and must be exempt from both, one that will go with no
	// blob at all.
	seed(target, "z@y.test", "sent", `[{"storage_key":"blob-a"}]`)
	seed("q@y.test", "Bob Smith <bob@x.test>", "failed", `[{"storage_key":"blob-b"}]`)
	seed(target, "z@y.test", "scheduled", `[{"storage_key":"blob-c"}]`)
	seed(target, "z@y.test", "sent", `[]`)

	keys, err := s.StorageKeysForAddress(ctx, projID, target)
	if err != nil {
		t.Fatalf("keys: %v", err)
	}

	if len(keys) != 2 {
		t.Fatalf("collected %d keys (%v), want blob-a and blob-b", len(keys), keys)
	}

	for _, k := range keys {
		if k == "blob-c" {
			t.Error("collected the key of a scheduled message, whose row is exempt - " +
				"the object would be deleted while the send still needs it")
		}
	}

	deleted, err := s.PurgeForAddress(ctx, projID, target)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}

	// Three rows go: the two with blobs and the one without. The
	// scheduled one stays. So rows-with-blobs deleted must equal the key
	// count, which is the invariant.
	if deleted != 3 {
		t.Errorf("deleted %d rows, want 3 (the scheduled one is exempt)", deleted)
	}

	var scheduledLeft int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM emails WHERE project_id = $1 AND status = 'scheduled'`,
		projID).Scan(&scheduledLeft); err != nil {
		t.Fatalf("count scheduled: %v", err)
	}

	if scheduledLeft != 1 {
		t.Error("erasure took an in-flight message, which is work the queue still owns")
	}
}
