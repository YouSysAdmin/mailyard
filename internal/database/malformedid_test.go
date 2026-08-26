// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package database_test

import (
	"testing"

	"github.com/yousysadmin/mailyard/internal/database"
	"github.com/yousysadmin/mailyard/internal/database/dbtest"
)

// The discriminator is a real Postgres error, so it is read from a real
// Postgres.
//
// A fabricated PgError would only prove that the function compares two
// strings. What has to stay true is that the server sends
// routine="string_to_uuid" for a uuid it cannot parse and something else
// for every other 22P02 - if a future version renames that routine, an
// id that is not a uuid goes back to answering 500 and nothing else
// would notice.
func TestOnlyAMalformedUUIDReadsAsAMissingResource(t *testing.T) {
	db := dbtest.Open(t)
	ctx := t.Context()

	cases := []struct {
		name string
		sql  string
		want bool
	}{
		// The case this exists for: an id from a URL that is not a uuid.
		{"a uuid parameter", `SELECT 1 WHERE $1::uuid IS NOT NULL`, true},

		// Both also answer 22P02, and both are a bug rather than a
		// missing row - a caller cannot reach them by editing a path.
		{"an integer parameter", `SELECT 1 WHERE $1::int > 0`, false},
		{"a json parameter", `SELECT 1 WHERE $1::jsonb IS NOT NULL`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			//sqlconst:allow the three statements are constants in the table above, and
			// the point of the test is what Postgres answers for each type
			_, err := db.QueryContext(ctx, tc.sql, "banana")
			if err == nil {
				t.Fatal("postgres accepted `banana`, so this test proves nothing")
			}

			if got := database.MalformedID(err); got != tc.want {
				t.Errorf("MalformedID = %v, want %v, for: %v", got, tc.want, err)
			}
		})
	}

	// An ordinary error must not be softened into a 404 - a table that
	// does not exist is a bug that has to stay loud.
	_, err := db.QueryContext(ctx, `SELECT 1 FROM no_such_table_here`)
	if err == nil {
		t.Fatal("postgres answered a query against a table that does not exist")
	}

	if database.MalformedID(err) {
		t.Error("an undefined table reads as a malformed id, so a broken query would answer 404")
	}
}
