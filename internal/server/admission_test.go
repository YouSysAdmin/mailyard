// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package server

import (
	"encoding/json/v2"
	"io"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// At the ceiling a request is refused in the JSON envelope, a probe is
// still answered, and one caller cannot take every slot.
func TestAdmissionRefusesInTheEnvelopeAndSparesTheProbes(t *testing.T) {
	for _, tc := range []struct {
		name         string
		limit, perIP int
		want         int
	}{
		{"overall", 2, 0, fiber.StatusServiceUnavailable},
		{"per caller", 10, 2, fiber.StatusTooManyRequests},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adm := newAdmission(tc.limit, tc.perIP, nil, nil)
			release := make(chan struct{})
			var holding sync.WaitGroup
			holding.Add(2)

			app := fiber.New()
			app.Use(adm.handler)
			app.Get("/slow", func(c fiber.Ctx) error {
				holding.Done()
				<-release

				return c.SendString("ok")
			})
			app.Get("/fast", func(c fiber.Ctx) error { return c.SendString("ok") })
			app.Get("/healthz", func(c fiber.Ctx) error { return c.SendString("ok") })

			var done sync.WaitGroup
			for range 2 {
				done.Go(func() {
					res, err := app.Test(httptest.NewRequest("GET", "/slow", nil), fiber.TestConfig{Timeout: 0})
					if err == nil {
						_ = res.Body.Close()
					}
				})
			}

			holding.Wait()

			res, err := app.Test(httptest.NewRequest("GET", "/fast", nil))
			if err != nil {
				t.Fatal(err)
			}

			body, _ := io.ReadAll(res.Body)
			_ = res.Body.Close()

			var env struct {
				Error string `json:"error"`
			}

			if res.StatusCode != tc.want || json.Unmarshal(body, &env) != nil || env.Error == "" {
				t.Errorf("past the bound: %d %s, want %d in the envelope", res.StatusCode, body, tc.want)
			}

			if res.Header.Get(fiber.HeaderRetryAfter) == "" {
				t.Error("no Retry-After on the refusal")
			}

			probe, err := app.Test(httptest.NewRequest("GET", "/healthz", nil))
			if err != nil {
				t.Fatal(err)
			}

			_ = probe.Body.Close()

			if probe.StatusCode != fiber.StatusOK {
				t.Errorf("the probe answered %d at the ceiling", probe.StatusCode)
			}

			close(release)
			done.Wait()

			after, err := app.Test(httptest.NewRequest("GET", "/fast", nil))
			if err != nil {
				t.Fatal(err)
			}

			_ = after.Body.Close()

			if after.StatusCode != fiber.StatusOK {
				t.Errorf("after the slots were released: %d", after.StatusCode)
			}
		})
	}
}

func TestTheConnectionCeilingSitsAboveTheRequestLimit(t *testing.T) {
	if got := connectionCeiling(0); got != 0 {
		t.Errorf("0 must stay the library default, got %d", got)
	}

	for _, limit := range []int{1, 100, 4096} {
		if got := connectionCeiling(limit); got <= limit {
			t.Errorf("ceiling %d for a limit of %d leaves admission no room", got, limit)
		}
	}
}
