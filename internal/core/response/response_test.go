// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package response

import (
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgconn"
)

// A path id that is not a uuid must read as a missing resource.
//
// It answered 500 on every route with an :id - reads, writes and
// deletes, across the whole of /api/v1 - because ids are uuid columns
// and Postgres refuses the comparison rather than matching no rows.
// Anything scanning an installation produces those, so the log filled
// with internal errors that were nothing of the kind.
//
// The fabricated error is safe here: what Postgres actually sends is
// pinned by TestOnlyAMalformedUUIDReadsAsAMissingResource against a real
// server. This test pins what the API does with it.
func TestAnIDThatCannotBeAUUIDIsNotFound(t *testing.T) {
	malformed := &pgconn.PgError{
		Code:    "22P02",
		Routine: "string_to_uuid",
		Message: `invalid input syntax for type uuid: "banana"`,
	}

	cases := []struct {
		name string
		err  error
		want int
		body string
	}{
		{"an id that is not a uuid", malformed, fiber.StatusNotFound, `{"error":"not found"}`},
		// Wrapped, because a store returns fmt.Errorf("get template: %w", err).
		{"the same error wrapped by a store", fmt.Errorf("get template: %w", malformed),
			fiber.StatusNotFound, `{"error":"not found"}`},
		{"anything else", errors.New("connection refused"),
			fiber.StatusInternalServerError, `{"error":"internal server error"}`},
		// A different 22P02. A malformed integer is not something a
		// caller can produce by editing a path, so it stays a 500.
		{"a malformed integer", &pgconn.PgError{Code: "22P02", Routine: "pg_strtoint32_safe"},
			fiber.StatusInternalServerError, `{"error":"internal server error"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			app.Get("/x", func(c *fiber.Ctx) error { return Internal(c, tc.err) })
			res, err := app.Test(httptest.NewRequest("GET", "/x", nil))
			if err != nil {
				t.Fatal(err)
			}

			defer func() { _ = res.Body.Close() }()
			if res.StatusCode != tc.want {
				t.Errorf("status %d, want %d", res.StatusCode, tc.want)
			}

			body, _ := io.ReadAll(res.Body)
			if string(body) != tc.body {
				t.Errorf("body %s, want %s", body, tc.body)
			}
		})
	}
}
