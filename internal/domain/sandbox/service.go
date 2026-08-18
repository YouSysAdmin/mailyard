// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package sandbox

import (
	"context"
	"log/slog"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/mailparse"
	"github.com/yousysadmin/mailyard/internal/core/metrics"
	"github.com/yousysadmin/mailyard/internal/core/quota"
	"github.com/yousysadmin/mailyard/internal/core/settings"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	sbmodel "github.com/yousysadmin/mailyard/internal/models/sandbox"
	smodel "github.com/yousysadmin/mailyard/internal/models/setting"
)

// Service captures messages into a project's sandbox.
type Service struct {
	Store    store.SandboxStore
	Settings *settings.Service
	Log      *slog.Logger

	// All is the whole store, for the two questions that are not the
	// sandbox's own: which plan bounds this project's captures, and what
	// window the project chose. A sandbox belongs to a project, so what
	// limits it cannot be read from an installation-wide setting.
	//
	// Optional: nil falls back to the platform defaults, which is what
	// every capture did before plans could say anything about a sandbox.
	All *store.Store
}

// Request is one message to capture.
//
// Raw is always the whole message, on both surfaces. The submission
// listener already has the wire bytes. The API path renders the MIME
// it would otherwise have handed to the SMTP client and passes that,
// which costs one Build and means a developer inspecting an
// API-submitted message sees a real message rather than a rendering of
// our internal struct.
type Request struct {
	ProjectID    string
	Source       string
	CredentialID string
	APIKeyID     string
	ClientIP     string

	// EnvelopeFrom and Recipients are the SMTP envelope. Kept because
	// they are what a real receiver would have routed on, and they
	// differ from the headers exactly where a developer is most likely
	// to be confused.
	EnvelopeFrom string
	Recipients   []string

	Raw []byte

	// RetentionDays shortens how long this message is kept. Zero uses
	// the platform default. A value LONGER than the default is clamped
	// down to it - a caller may ask for less, never for more, so no
	// application can quietly pin a project's sandbox open.
	RetentionDays int
}

// Capture stores a message and returns the row.
//
// It never consults the sending pipeline: no verified sender, no SMTP
// server, no suppression list, no plan quota. A developer testing a
// signup flow from test@localhost in a project with nothing configured
// is exactly who this is for, and every one of those checks would
// refuse them.
func (s *Service) Capture(ctx context.Context, req *Request) (*sbmodel.Email, error) {
	now := time.Now().UTC()

	e := &sbmodel.Email{
		ID:           ids.New(),
		ProjectID:    req.ProjectID,
		Source:       req.Source,
		CredentialID: req.CredentialID,
		APIKeyID:     req.APIKeyID,
		ClientIP:     req.ClientIP,
		Sender:       req.EnvelopeFrom,
		Recipients:   req.Recipients,
		Raw:          req.Raw,
		Size:         int64(len(req.Raw)),
		ExpiresAt:    s.expiryFor(ctx, req.ProjectID, now, req.RetentionDays),
		ReceivedAt:   now,
		CreatedAt:    now,
	}

	// A message that will not parse is still stored. Malformed MIME is
	// a thing a developer comes here to look at, and dropping it would
	// hide the one case where the raw view is the entire point.
	if parsed, err := mailparse.Parse(req.Raw); err == nil {
		e.Subject = parsed.Subject
		e.TextBody = parsed.TextBody
		e.HTMLBody = parsed.HTMLBody
		e.Headers = parsed.Headers
		for _, a := range parsed.Attachments {
			size := a.Size
			if size == 0 {
				size = int64(len(a.Content))
			}

			e.Attachments = append(e.Attachments, sbmodel.Attachment{
				Filename:    a.Filename,
				ContentType: a.ContentType,
				Size:        size,
			})
		}

		// Envelope recipients are authoritative, but the API path has
		// no envelope of its own worth the name, so fall back to the
		// header addresses rather than showing a message addressed to
		// nobody.
		if len(e.Recipients) == 0 {
			e.Recipients = parsed.To
		}

		if e.Sender == "" {
			e.Sender = parsed.From
		}
	}

	if err := s.Store.Put(ctx, e); err != nil {
		return nil, err
	}

	metrics.SandboxCaptures.Inc()

	// Trim after the insert, not before: the cap is on what is kept,
	// and taking the count first would leave one slot short whenever
	// two captures raced. A failure here is logged and swallowed - the
	// message IS stored, and refusing a submission because a cleanup
	// query failed would be reporting the wrong problem.
	if keep := s.keepFor(ctx, req.ProjectID); keep > 0 {
		if n, err := s.Store.Trim(ctx, req.ProjectID, keep); err != nil {
			s.Log.Warn("sandbox: trim failed", "project_id", req.ProjectID, "err", err)
		} else if n > 0 {
			s.Log.Debug("sandbox: trimmed oldest", "project_id", req.ProjectID, "removed", n)
		}
	}

	return e, nil
}

// keepFor is the ring buffer for this project: the plan's cap, falling
// back to the platform setting.
//
// The plan wins because the cap is what a plan SELLS. The setting is
// still the floor for an installation that has no plans, or a plan that
// says nothing about sandboxes - without it, removing the setting would
// leave the one table a developer under test can fill without limit
// entirely unbounded, which is exactly what this cap exists to prevent.
func (s *Service) keepFor(ctx context.Context, projID string) int {
	if s.All != nil {
		if msgs, _, err := quota.Sandbox(ctx, s.All, projID); err != nil {
			// A limit we could not read must not refuse the capture, and
			// must not silently become unlimited either - so fall through
			// to the installation's own cap.
			s.Log.Warn("sandbox: could not read the plan cap",
				"project_id", projID, "err", err)
		} else if msgs > 0 {
			return msgs
		}
	}

	return s.Settings.Int(smodel.KeySandboxMaxMessages)
}

// expiryFor resolves the retention window for one message. Nil means
// keep until something else removes it.
//
// Three things have a say, in this order:
//
//   - the project's window (projects.sandbox_retention_days), because a
//     sandbox is a project's and one team wanting a day of captures is
//     not the installation's business
//   - the PLAN's ceiling, which the project cannot exceed - it may ask
//     for less than its plan allows, never more
//   - the platform setting, as the default for a project that has not
//     chosen.
//
// And then the per-message request, which may only SHORTEN whatever came
// out of that.
func (s *Service) expiryFor(ctx context.Context, projID string, now time.Time, requested int) *time.Time {
	def := s.Settings.Int(smodel.KeySandboxRetentionDays)
	ceiling := 0
	if s.All != nil {
		if w, err := s.All.Project.Get(ctx, projID); err == nil && w != nil && w.SandboxRetentionDays > 0 {
			def = w.SandboxRetentionDays
		}

		if _, maxDays, err := quota.Sandbox(ctx, s.All, projID); err == nil && maxDays > 0 {
			ceiling = maxDays
		}
	}

	if ceiling > 0 && (def <= 0 || def > ceiling) {
		def = ceiling
	}

	days := def
	// Shorten only. A request for longer than the project allows is
	// not an error worth refusing a message over - it is a caller
	// asking for something the operator did not offer, and the answer
	// is the project's window.
	if requested > 0 && (def <= 0 || requested < def) {
		days = requested
	}

	if days <= 0 {
		return nil
	}

	t := now.AddDate(0, 0, days)

	return &t
}
