// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package dbtest

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// The race itself cannot be reproduced deterministically without
// dropping pg_trgm out from under every concurrently running package,
// so what is pinned is the CLASSIFIER: exactly the two SQLSTATEs the
// race produces, exactly on the one statement shape that is safe to
// forgive, and nothing else.
func TestOnlyTheExtensionRaceIsForgiven(t *testing.T) {
	ext := `CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;`
	dup := &pgconn.PgError{Code: "23505", ConstraintName: "pg_extension_name_index"}
	already := &pgconn.PgError{Code: "42710"}

	for name, tc := range map[string]struct {
		err  error
		stmt string
		want bool
	}{
		"the losing insert":            {dup, ext, true},
		"lost after the winner won":    {already, ext, true},
		"wrapped, as drivers hand out": {fmt.Errorf("exec: %w", dup), ext, true},

		// A duplicate key anywhere else is a bug in a migration and
		// must keep failing loudly.
		"23505 from a data insert": {dup, `INSERT INTO plans (id) VALUES ('x')`, false},
		// The statement matches but the error is something real - a
		// refused connection, a syntax error.
		"another error on the statement": {errors.New("connection refused"), ext, false},
		"a non-postgres error":           {fmt.Errorf("timeout"), ext, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := extensionRace(tc.err, tc.stmt); got != tc.want {
				t.Errorf("extensionRace = %v, want %v", got, tc.want)
			}
		})
	}
}
