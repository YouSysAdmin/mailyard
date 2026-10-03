// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package setting

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/cron"
	"github.com/yousysadmin/mailyard/internal/core/env"
)

// An unknown job is a missing resource, one already in flight a
// conflict, and a job that ran and failed an answered request carrying
// the roster with its error on the row.
func TestRunJobAnswersWhatHappened(t *testing.T) {
	m := cron.New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	release := make(chan struct{})
	started := make(chan struct{})
	m.Register(cron.Job{Name: "broken", Schedule: cron.DailyAt(3, 0), Run: func(context.Context) error {
		return errors.New(`relation "x" does not exist`)
	}})
	m.Register(cron.Job{Name: "slow", Schedule: cron.DailyAt(3, 0), Run: func(context.Context) error {
		close(started)
		<-release

		return nil
	}})

	h := &Handler{Runtime: &env.Runtime{Cron: m}}
	app := fiber.New()
	app.Post("/jobs/:name/run", h.RunJob)

	call := func(name string) (int, []byte) {
		res, err := app.Test(httptest.NewRequest("POST", "/jobs/"+name+"/run", nil))
		if err != nil {
			t.Fatal(err)
		}

		body, _ := io.ReadAll(res.Body)

		return res.StatusCode, body
	}

	if code, _ := call("nope"); code != fiber.StatusNotFound {
		t.Errorf("unknown job answered %d, want 404", code)
	}

	code, body := call("broken")
	if code != fiber.StatusOK {
		t.Fatalf("failed run answered %d, want 200: %s", code, body)
	}

	var out RunJobResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}

	if !out.Failed || out.Ran != "broken" || len(out.Jobs) != 2 {
		t.Errorf("failed run answered %+v", out)
	}

	go func() { _ = m.RunNow(context.Background(), "slow") }()
	<-started

	if code, _ := call("slow"); code != fiber.StatusConflict {
		t.Errorf("running job answered %d, want 409", code)
	}

	close(release)
}
