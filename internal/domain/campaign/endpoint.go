// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package campaign

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/paging"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/email"
	cmodel "github.com/yousysadmin/mailyard/internal/models/campaign"
)

// Handler owns the /api/campaigns surface.
type Handler struct {
	Runtime *env.Runtime
}

// validateCampaignRefs checks the template and list exist in the
// project and the variant split is sane.
//
// Two returns, and the bool is the one that matters. Returning a lone
// error does not work here: `response.*` writes the status and returns
// nil, so every refusal comes back as nil, the caller's `if err != nil`
// never fires, and Create carries on. A campaign could then be created
// naming a template from another project, one with no active version, a
// list that does not exist, a made-up SMTP group, or variant splits over
// 100 - and the caller sees 201, because the 4xx body is written first
// and the created one overwrites it. The runner then acts on that row.
//
// Same trap as verifySession, passkeySelf, enrolmentScope and
// refuseCAOverAnAssignedName.
func (h *Handler) validateCampaignRefs(c fiber.Ctx, projID string, in *upsertInput) (bool, error) {
	t, err := h.Runtime.Store.Template.Get(c.Context(), projID, in.TemplateID)
	if err != nil {
		return false, response.Internal(c, err)
	}

	if t == nil {
		return false, response.NotFound(c, "template not found")
	}

	if t.ActiveVersionID == nil {
		return false, response.BadRequest(c, "template has no active version")
	}

	if in.SMTPGroup != "" {
		g, err := h.Runtime.Store.SMTPGroup.GetBySlug(c.Context(), projID, in.SMTPGroup)
		if err != nil {
			return false, response.Internal(c, err)
		}

		if g == nil {
			return false, response.BadRequest(c, "smtp server group "+in.SMTPGroup+" does not exist")
		}

		// Stash the id so toModel does not have to look it up again.
		in.smtpGroupID = g.ID
	}

	l, err := h.Runtime.Store.SubscriberList.Get(c.Context(), projID, in.ListID)
	if err != nil {
		return false, response.Internal(c, err)
	}

	if l == nil {
		return false, response.NotFound(c, "subscriber list not found")
	}

	if in.ABTestEnabled {
		if len(in.ABVariants) < 2 {
			return false, response.BadRequest(c, "ab testing requires at least two variants")
		}

		sum := 0
		seen := map[string]bool{}
		for _, v := range in.ABVariants {
			if seen[v.Name] {
				return false, response.BadRequest(c, "duplicate variant name "+v.Name)
			}

			seen[v.Name] = true
			sum += v.SplitPercentage
			if v.TemplateID != "" {
				vt, err := h.Runtime.Store.Template.Get(c.Context(), projID, v.TemplateID)
				if err != nil {
					return false, response.Internal(c, err)
				}

				if vt == nil {
					return false, response.NotFound(c, "variant template not found: "+v.Name)
				}
			}
		}

		if sum > 100 {
			return false, response.BadRequest(c, "variant split percentages exceed 100")
		}
	}

	return true, nil
}

// List serves GET /api/v1/campaigns.
func (h *Handler) List(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	campaigns, err := h.Runtime.Store.Campaign.List(c.Context(), rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if campaigns == nil {
		campaigns = []*cmodel.Campaign{}
	}

	return response.Success(c, ListResponse{Campaigns: campaigns})
}

// Get serves GET /api/v1/campaigns/:id.
func (h *Handler) Get(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	cam, err := h.Runtime.Store.Campaign.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if cam == nil {
		return response.NotFound(c, "campaign not found")
	}

	totals, byVariant, err := h.Runtime.Store.Campaign.MessageStats(c.Context(), cam.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	opened, clicked, err := h.Runtime.Store.Campaign.EngagementStats(c.Context(), cam.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, CampaignDetailResponse{
		Campaign: cam, Stats: totals, StatsByVariant: byVariant,
		Engagement: engagementOf(totals["sent"], opened, clicked),
	})
}

// Analytics is the deep-dive readout for one campaign: per-link click
// tallies plus daily open and click series from the raw event log.
// The headline counts stay on Get - this endpoint adds what a chart
// needs. Series only reach back as far as tracking-event retention,
// while the counters on Get are aggregated and survive the sweep.
func (h *Handler) Analytics(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	cam, err := h.Runtime.Store.Campaign.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if cam == nil {
		return response.NotFound(c, "campaign not found")
	}

	links, err := h.Runtime.Store.Campaign.ListTrackedLinks(c.Context(), cam.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if links == nil {
		links = []*cmodel.TrackedLink{}
	}

	openSeries, err := h.Runtime.Store.Campaign.EventSeries(c.Context(), cam.ID, cmodel.EventOpen)
	if err != nil {
		return response.Internal(c, err)
	}

	clickSeries, err := h.Runtime.Store.Campaign.EventSeries(c.Context(), cam.ID, cmodel.EventClick)
	if err != nil {
		return response.Internal(c, err)
	}

	if openSeries == nil {
		openSeries = []cmodel.DayCount{}
	}

	if clickSeries == nil {
		clickSeries = []cmodel.DayCount{}
	}

	return response.Success(c, AnalyticsResponse{
		Links:       links,
		OpenSeries:  openSeries,
		ClickSeries: clickSeries,
	})
}

// Create serves POST /api/v1/campaigns.
func (h *Handler) Create(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[upsertInput](c)
	if !ok {
		return resp
	}

	if ok, resp := h.validateCampaignRefs(c, rc.Project.ID, &in); !ok {
		return resp
	}

	cam := in.toModel(rc.Project.ID)
	cam.ID = ids.New()
	cam.Status = cmodel.StatusDraft
	if rc.User != nil {
		cam.CreatedBy = rc.User.ID
	}

	if err := h.Runtime.Store.Campaign.Put(c.Context(), cam); err != nil {
		return response.Internal(c, err)
	}

	return response.Created(c, CampaignResponse{Campaign: cam})
}

// Update replaces the definition. Only draft campaigns are editable
// (a scheduled campaign must be cancelled back to draft first, sends
// in flight are immutable).
func (h *Handler) Update(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	cam, err := h.Runtime.Store.Campaign.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if cam == nil {
		return response.NotFound(c, "campaign not found")
	}

	if cam.Status != cmodel.StatusDraft {
		return response.Conflict(c, "only draft campaigns can be edited")
	}

	in, resp, ok := validation.Bind[upsertInput](c)
	if !ok {
		return resp
	}

	if ok, resp := h.validateCampaignRefs(c, rc.Project.ID, &in); !ok {
		return resp
	}

	updated := in.toModel(rc.Project.ID)
	updated.ID = cam.ID
	updated.Status = cam.Status
	updated.CreatedBy = cam.CreatedBy
	updated.CreatedAt = cam.CreatedAt
	updated.UpdatedAt = new(time.Now().UTC())
	if err := h.Runtime.Store.Campaign.Put(c.Context(), updated); err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, CampaignResponse{Campaign: updated})
}

// Delete removes a campaign that never ran (draft or cancelled).
func (h *Handler) Delete(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	cam, err := h.Runtime.Store.Campaign.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if cam == nil {
		return response.NotFound(c, "campaign not found")
	}

	if cam.Status != cmodel.StatusDraft && cam.Status != cmodel.StatusCancelled {
		return response.Conflict(c, "only draft or cancelled campaigns can be deleted")
	}

	if err := h.Runtime.Store.Campaign.Delete(c.Context(), rc.Project.ID, cam.ID); err != nil {
		return response.Internal(c, err)
	}

	return response.NoContent(c)
}

// Send launches the campaign now, or schedules it when scheduled_at
// is set.
func (h *Handler) Send(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	cam, err := h.Runtime.Store.Campaign.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if cam == nil {
		return response.NotFound(c, "campaign not found")
	}

	if cam.Status != cmodel.StatusDraft && cam.Status != cmodel.StatusScheduled {
		return response.Conflict(c, "campaign is already running or finished")
	}

	// Bulk mail has to carry a working unsubscribe. Gmail and Yahoo
	// have required one-click unsubscribe from bulk senders since
	// February 2024, and mail without it is filtered rather than
	// bounced - so the failure is invisible from here.
	//
	// The hosted unsubscribe link is absolute and signed, so it cannot
	// be built without server.public_url. Refusing to start is the
	// only honest option: the alternative is what happened before this
	// check, which was sending the whole audience with no
	// List-Unsubscribe header and no indication anything was wrong.
	if h.Runtime.Tracking == nil || !h.Runtime.Tracking.Enabled() {
		return response.BadRequest(c,
			"campaigns need a public URL to mint unsubscribe links - set server.public_url "+
				"(and auth.jwt_secret), otherwise this would send bulk mail with no "+
				"List-Unsubscribe header and land in spam")
	}

	// The same sender check every send makes, asked once here instead
	// of once per recipient. Without it a campaign to a hundred
	// thousand subscribers would queue, run, and fail every single
	// message for a reason that was knowable before the first one.
	if err := email.RequireVerifiedSender(c.Context(), h.Runtime.Store, rc.Project.ID, cam.FromEmail); err != nil {
		return response.BadRequest(c, err.Error())
	}

	in, resp, ok := validation.Bind[sendInput](c)
	if !ok {
		return resp
	}

	now := time.Now().UTC()
	if in.ScheduledAt != "" {
		at, err := time.Parse(time.RFC3339, in.ScheduledAt)
		if err != nil {
			return response.BadRequest(c, "scheduled_at must be an RFC 3339 timestamp")
		}

		utc := at.UTC()
		if utc.Before(now) {
			return response.BadRequest(c, "scheduled_at is in the past")
		}

		cam.Status = cmodel.StatusScheduled
		cam.ScheduledAt = &utc
	} else {
		cam.Status = cmodel.StatusSending
		cam.StartedAt = &now
		cam.NextBatchAt = &now
	}

	cam.UpdatedAt = &now
	// One guarded statement, not a Put. The status check above read a
	// snapshot, and a Cancel can land between it and the write - Put
	// wrote the stale status back and resurrected the cancelled
	// campaign. Launch re-checks draft/scheduled inside the UPDATE.
	moved, err := h.Runtime.Store.Campaign.Launch(c.Context(), rc.Project.ID, cam.ID,
		cam.Status, cam.ScheduledAt, cam.StartedAt, cam.NextBatchAt)
	if err != nil {
		return response.Internal(c, err)
	}

	if !moved {
		return response.Conflict(c, "campaign is already running or finished")
	}

	if cam.Status == cmodel.StatusSending && h.Runtime.CampaignWake != nil {
		h.Runtime.CampaignWake()
	}

	return response.Success(c, CampaignResponse{Campaign: cam})
}

// Pause serves POST /api/v1/campaigns/:id/pause.
func (h *Handler) Pause(c fiber.Ctx) error {
	return h.transition(c, cmodel.StatusPaused, "campaign is not sending",
		cmodel.StatusSending)
}

// Resume serves POST /api/v1/campaigns/:id/resume.
func (h *Handler) Resume(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	ok, err := h.Runtime.Store.Campaign.TransitionStatus(c.Context(),
		rc.Project.ID, c.Params("id"), cmodel.StatusSending, cmodel.StatusPaused)
	if err != nil {
		return response.Internal(c, err)
	}

	if !ok {
		return response.Conflict(c, "campaign is not paused")
	}

	// Clear the lease so the runner picks it up immediately. Guarded on
	// sending, which the transition above just established - so a
	// concurrent cancel between the two statements wins rather than
	// being overwritten here.
	now := time.Now().UTC()
	if _, err := h.Runtime.Store.Campaign.SetRunState(c.Context(),
		c.Params("id"), cmodel.StatusSending, nil, nil, &now, cmodel.StatusSending); err != nil {
		return response.Internal(c, err)
	}

	if h.Runtime.CampaignWake != nil {
		h.Runtime.CampaignWake()
	}

	return h.respondWith(c, c.Params("id"))
}

// Cancel stops a campaign for good and skips the unsent remainder.
func (h *Handler) Cancel(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	ok, err := h.Runtime.Store.Campaign.TransitionStatus(c.Context(),
		rc.Project.ID, c.Params("id"), cmodel.StatusCancelled,
		cmodel.StatusScheduled, cmodel.StatusSending, cmodel.StatusPaused)
	if err != nil {
		return response.Internal(c, err)
	}

	if !ok {
		return response.Conflict(c, "campaign cannot be cancelled from its current state")
	}

	if _, err := h.Runtime.Store.Campaign.SkipPending(c.Context(),
		c.Params("id"), "campaign cancelled"); err != nil {
		return response.Internal(c, err)
	}

	return h.respondWith(c, c.Params("id"))
}

// Duplicate copies the definition into a fresh draft.
func (h *Handler) Duplicate(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	cam, err := h.Runtime.Store.Campaign.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if cam == nil {
		return response.NotFound(c, "campaign not found")
	}

	dup := *cam
	dup.ID = ids.New()
	dup.Name = cam.Name + " (copy)"
	dup.Status = cmodel.StatusDraft
	dup.ScheduledAt = nil
	dup.StartedAt = nil
	dup.CompletedAt = nil
	dup.NextBatchAt = nil
	dup.CreatedAt = time.Now().UTC()
	dup.UpdatedAt = nil
	if rc.User != nil {
		dup.CreatedBy = rc.User.ID
	}

	if err := h.Runtime.Store.Campaign.Put(c.Context(), &dup); err != nil {
		return response.Internal(c, err)
	}

	return response.Created(c, CampaignResponse{Campaign: &dup})
}

// Messages serves GET /api/v1/campaigns/:id/messages.
func (h *Handler) Messages(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	cam, err := h.Runtime.Store.Campaign.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if cam == nil {
		return response.NotFound(c, "campaign not found")
	}

	pg := paging.From(c)
	msgs, err := h.Runtime.Store.Campaign.ListMessages(c.Context(),
		rc.Project.ID, cam.ID, pg.Limit, pg.Offset)
	if err != nil {
		return response.Internal(c, err)
	}

	if msgs == nil {
		msgs = []*cmodel.Message{}
	}

	return response.Success(c, MessageListResponse{Messages: msgs})
}

func (h *Handler) transition(c fiber.Ctx, to, conflictMsg string, from ...string) error {
	rc := domain.GetRequestContext(c)
	ok, err := h.Runtime.Store.Campaign.TransitionStatus(c.Context(),
		rc.Project.ID, c.Params("id"), to, from...)
	if err != nil {
		return response.Internal(c, err)
	}

	if !ok {
		return response.Conflict(c, conflictMsg)
	}

	return h.respondWith(c, c.Params("id"))
}

func (h *Handler) respondWith(c fiber.Ctx, id string) error {
	rc := domain.GetRequestContext(c)
	cam, err := h.Runtime.Store.Campaign.Get(c.Context(), rc.Project.ID, id)
	if err != nil {
		return response.Internal(c, err)
	}

	if cam == nil {
		return response.NotFound(c, "campaign not found")
	}

	return response.Success(c, CampaignResponse{Campaign: cam})
}

func (in *upsertInput) toModel(projID string) *cmodel.Campaign {
	cam := &cmodel.Campaign{
		ProjectID:       projID,
		Name:            in.Name,
		Subject:         in.Subject,
		FromEmail:       in.FromEmail,
		FromName:        in.FromName,
		TemplateID:      in.TemplateID,
		Language:        in.Language,
		TemplateData:    in.TemplateData,
		ListID:          in.ListID,
		SMTPGroupID:     in.smtpGroupID,
		SendRate:        in.SendRate,
		SendAtLocalTime: in.SendAtLocalTime,
		ABTestEnabled:   in.ABTestEnabled,
		ABVariants:      in.ABVariants,
	}
	if cam.ABVariants == nil {
		cam.ABVariants = []cmodel.Variant{}
	}

	return cam
}
