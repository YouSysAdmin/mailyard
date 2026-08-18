// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package trackingpage

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/tracking"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	cmodel "github.com/yousysadmin/mailyard/internal/models/campaign"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
)

// replayEmails counts opens the way the real UPDATE does, so the handler
// sees a rising count across replays.
type replayEmails struct {
	store.EmailStore
	row   *emailmodel.Email
	opens int64
}

func (r *replayEmails) GetAny(context.Context, string) (*emailmodel.Email, error) {
	return r.row, nil
}

func (r *replayEmails) MarkOpened(context.Context, string, time.Time, time.Time) (bool, int64, error) {
	r.opens++

	return r.opens == 1, r.opens, nil
}

// replayCampaigns records what actually reached tracking_events.
type replayCampaigns struct {
	store.CampaignStore
	events int
}

func (r *replayCampaigns) GetMessageByEmail(context.Context, string) (*cmodel.Message, error) {
	return nil, nil
}

func (r *replayCampaigns) InsertTrackingEvent(context.Context, *cmodel.TrackingEvent) error {
	r.events++

	return nil
}

// A signed tracking URL is replayable forever, so the write it causes has
// to be bounded.
//
// The pixel is unauthenticated by design, with the HMAC in the URL as
// the authority, and that signature never expires - so the URL in a
// recipient's mailbox works for good. Without a bound, every hit inserts
// a tracking_events row, and that table's retention is opt in, so one
// leaked URL grows it forever on a default install.
//
// There is deliberately no rate limiter on /tracking: Gmail fetches
// every image through its own proxies, so a per-IP bound throttles real
// opens from a shared proxy long before it inconveniences a replay loop.
// The ceiling is on the event instead, which is the thing that grows -
// the counters are increments on one row and stay exact.
//
// This drives the real handler through a real request, 200 times with a
// valid signature, and counts what reached the store.
func TestReplayingThePixelStopsWritingEvents(t *testing.T) {
	emails := &replayEmails{row: &emailmodel.Email{
		ID: "9e2f6f11-cdd3-4058-86f2-29f3ad60b06a", ProjectID: "p1",
	}}
	camps := &replayCampaigns{}
	signer := tracking.NewSigner("https://mail.example.com", "a-secret-at-least-32-characters-long")
	h := &Handler{Runtime: &env.Runtime{
		Tracking: signer,
		Store:    &store.Store{Email: emails, Campaign: camps},
	}}
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	app := fiber.New()
	app.Get("/tracking/open/:file", h.Open)

	const id = "9e2f6f11-cdd3-4058-86f2-29f3ad60b06a"

	// Built from the signer itself rather than hand-rolled, so the test
	// cannot pass against a signature scheme it does not share.
	full := signer.OpenURL(id)
	url := full[strings.Index(full, "/tracking/"):]

	const replays = 200
	for i := range replays {
		req := httptest.NewRequest(fiber.MethodGet, url, nil)
		// A real mail client, or the bot filter drops the hit before any
		// of this and the test proves nothing.
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh) AppleWebKit/605.1.15 Safari/605.1.15")
		res, err := app.Test(req)
		if err != nil {
			t.Fatalf("replay %d: %v", i, err)
		}

		_ = res.Body.Close()

		if res.StatusCode != fiber.StatusOK {
			t.Fatalf("replay %d answered %d, want the pixel", i, res.StatusCode)
		}
	}

	// The counters saw every hit - that is what they are for.
	if emails.opens != replays {
		t.Errorf("counted %d opens, want all %d - the counters must stay exact", emails.opens, replays)
	}

	// The event timeline stopped.
	if camps.events != trackedEventsPerEmail {
		t.Errorf("wrote %d tracking_events rows for %d replays, want the ceiling of %d - "+
			"an unauthenticated URL that never expires must not grow a table without bound",
			camps.events, replays, trackedEventsPerEmail)
	}
}

// A ceiling low enough to break the feature is not a fix. The timeline
// exists so an operator can see when a message was opened and from where.
func TestTheEventCeilingIsHighEnoughToBeUseful(t *testing.T) {
	if trackedEventsPerEmail < 10 {
		t.Errorf("the ceiling is %d, too low to answer what the timeline is for", trackedEventsPerEmail)
	}
}
