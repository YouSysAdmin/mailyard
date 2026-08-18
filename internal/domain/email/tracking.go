// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"context"

	"github.com/yousysadmin/mailyard/internal/core/tracking"
	cmodel "github.com/yousysadmin/mailyard/internal/models/campaign"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
)

// TrackPref is a caller's answer to "should this one be tracked".
//
// Three states, not a bool: a send that says nothing must fall back
// to the project default, and a send that says "no" has to be able to
// override a project default of yes. Two booleans would encode the
// same thing less obviously.
type TrackPref int

const (
	// TrackDefault defers to the project settings.
	TrackDefault TrackPref = iota

	// TrackOff suppresses tracking for this message whatever the
	// project says.
	TrackOff
)

// applyTracking rewrites the body of a NON-campaign send: the open
// pixel and the click redirects, according to the project defaults
// and whatever the caller asked for.
//
// Campaigns do their own in the runner, before this is reached, since
// they need the campaign id for link grouping and mint their unsubscribe
// link at the same time. This is the transactional path: API sends,
// template sends and SMTP submissions.
//
// Best effort by design. Tracking is reporting, and a message must go
// out even when the reporting around it cannot be set up - so every
// failure here logs and leaves the body alone.
func (s *Service) applyTracking(ctx context.Context, projID string, e *emailmodel.Email, pref TrackPref) {
	if pref == TrackOff || s.Tracking == nil || !s.Tracking.Enabled() || e.HTMLBody == "" {
		return
	}

	proj, err := s.Store.Project.Get(ctx, projID)
	if err != nil || proj == nil {
		if err != nil {
			s.Log.Warn("email: tracking skipped, project lookup failed",
				"email_id", e.ID, "project_id", projID, "err", err)
		}

		return
	}

	if !proj.TrackOpens && !proj.TrackClicks {
		return
	}

	html, links := s.Tracking.ProcessHTML(e.HTMLBody, tracking.TrackOpts{
		EmailID: e.ID,
		// Project scope, not campaign: the same URL across a project's
		// transactional mail is one tally.
		LinkScope: projID,
		Opens:     proj.TrackOpens,
		Clicks:    proj.TrackClicks,
	})
	e.HTMLBody = html
	e.Tracked = true

	for _, l := range links {
		if err := s.Store.Campaign.UpsertTrackedLink(ctx, &cmodel.TrackedLink{
			ProjectID: projID, OriginalURL: l.URL, Hash: l.Hash,
		}); err != nil {
			s.Log.Error("email: register tracked link",
				"email_id", e.ID, "url", l.URL, "err", err)
		}
	}
}
