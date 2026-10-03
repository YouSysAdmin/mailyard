// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package paging

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// A window error is one sentence naming the parameter, so a caller
// shows it as it is rather than prefixing it.
func TestAWindowErrorNamesItsParameter(t *testing.T) {
	cases := map[string]string{
		"/x?from=bogus":                    "from must be a date (2026-08-01) or an RFC 3339 timestamp",
		"/x?to=bogus":                      "to must be a date (2026-08-01) or an RFC 3339 timestamp",
		"/x?from=2026-08-02&to=2026-08-01": "to must be after from",
	}

	for path, want := range cases {
		app := fiber.New()
		got := ""
		app.Get("/x", func(c fiber.Ctx) error {
			if _, _, err := TimeWindow(c); err != nil {
				got = err.Error()
			}

			return nil
		})

		res, err := app.Test(httptest.NewRequest("GET", path, nil))
		if err != nil {
			t.Fatal(err)
		}

		_ = res.Body.Close()
		if got != want {
			t.Errorf("%s: %q, want %q", path, got, want)
		}
	}
}
