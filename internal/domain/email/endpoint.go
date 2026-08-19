// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"errors"
	"regexp"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/paging"
	"github.com/yousysadmin/mailyard/internal/core/quota"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain"
	emailmodel "github.com/yousysadmin/mailyard/internal/models/email"
)

// Handler owns the /api/emails surface. Mounted behind requireAuth +
// requireProject.
type Handler struct {
	Runtime *env.Runtime
}

// Send accepts an email for asynchronous delivery (202-style: the
// response carries the queued row, delivery status is polled).
func (h *Handler) Send(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[sendInput](c)
	if !ok {
		return resp
	}

	req, err := in.toRequest()
	if err != nil {
		return response.BadRequest(c, err.Error())
	}

	// Before ResolveRoute and before Validate, and that ordering is
	// the feature. Both of those ask questions about DELIVERY - is
	// there a server, is the sender's domain verified, is the project
	// within its plan - and a developer testing a signup flow in a
	// project with nothing configured answers no to all of them. The
	// sandbox is for exactly that developer.
	if want, refusal := sandboxIntent(rc, &in); refusal != "" {
		return response.BadRequest(c, refusal)
	} else if want {
		// dry_run means "run the checks, persist nothing" - and a
		// capture IS persistence: it took a slot of the sandbox ring
		// buffer, evicted a real capture, and answered 201 as if
		// stored. A sandbox send skips the delivery questions by
		// design, so what a dry run promises here is that the request
		// parsed and would have been captured.
		if in.DryRun {
			return response.Success(c, DryRunResponse{
				DryRun: true, Valid: true, Recipients: len(req.To),
				Suppressed: emptyIfNil(nil),
			})
		}

		_, err := h.captureSandbox(c, rc, req, in.SandboxRetentionDays)

		return err
	}

	svc := NewService(h.Runtime)
	// Resolved before validation so an unknown group is a 400 naming
	// the group, not a confusing "no smtp server accepts this sender".
	if req.Route, err = svc.ResolveRoute(c.Context(), rc.Project.ID, in.SMTPServerID, in.SMTPGroup); err != nil {
		return sendFailure(c, err)
	}

	if in.DisableTracking {
		req.Track = TrackOff
	}

	if in.DryRun {
		if err := svc.Validate(c.Context(), rc.Project.ID, req); err != nil {
			return sendFailure(c, err)
		}

		// The same filter the real send uses. FilterSuppressed sees only
		// global blocks, so a dry run submitted with unsubscribe_list_id
		// reported a recipient as deliverable when Service.Send - which
		// calls FilterSuppressedForList - was going to drop them for
		// having opted out of that list. A dry run that disagrees with
		// to send is worse than no dry run.
		_, blocked, err := h.Runtime.Store.Suppression.FilterSuppressedForList(
			c.Context(), rc.Project.ID, req.UnsubscribeListID, req.To)
		if err != nil {
			return response.Internal(c, err)
		}

		return response.Success(c, DryRunResponse{
			DryRun: true, Valid: true, Recipients: len(req.To),
			Suppressed: emptyIfNil(blocked),
		})
	}

	e, blocked, err := svc.Send(c.Context(), rc.Project.ID, callerID(rc), apiKeyID(rc), req)
	if err != nil {
		return sendFailure(c, err)
	}

	return response.Created(c, SendResponse{Email: e, Suppressed: emptyIfNil(blocked)})
}

// maxAttachmentsPerEmail mirrors the max=10 on sendInput.Attachments.
// Kept as a constant so Limits cannot drift from the validate tag.
const maxAttachmentsPerEmail = 10

// Limits reports what send will accept, so a client can reject an
// oversized file before spending the upload. Hardcoding these in the
// console would be a second copy of a number an operator can change.
func (h *Handler) Limits(c fiber.Ctx) error {
	s := h.Runtime.Config.Sending

	return response.Success(c, LimitsResponse{Limits: SendingLimits{
		MaxRecipients:          s.MaxRecipients,
		MaxAttachments:         maxAttachmentsPerEmail,
		MaxAttachmentSize:      s.MaxAttachmentSize,
		MaxTotalAttachmentSize: s.MaxTotalAttachmentSize,
	}})
}

// emptyIfNil keeps JSON arrays arrays.
func emptyIfNil(in []string) []string {
	if in == nil {
		return []string{}
	}

	return in
}

// List serves GET /api/v1/emails.
func (h *Handler) List(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	f := Filter{
		Status: c.Query("status"),
		Search: paging.Search(c, "search"),
		Limit:  paging.From(c).Limit,
	}
	if before := c.Query("before"); before != "" {
		t, err := time.Parse(time.RFC3339, before)
		if err != nil {
			return response.BadRequest(c, "before must be an RFC 3339 timestamp")
		}

		f.Before = &t
		// The other half of the cursor. Optional, so `?before=` on its
		// own keeps working - but without it two messages sharing a
		// created_at across a page boundary are skipped entirely, and the
		// caller never learns a row went missing.
		f.BeforeID = c.Query("before_id")
	}

	emails, err := h.Runtime.Store.Email.List(c.Context(), rc.Project.ID, f)
	if err != nil {
		return response.Internal(c, err)
	}

	if emails == nil {
		emails = []*emailmodel.Email{}
	}

	return response.Success(c, ListResponse{Emails: emails})
}

// Stats returns per-status counts for the project.
func (h *Handler) Stats(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	counts, err := h.Runtime.Store.Email.CountByStatus(c.Context(), rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, StatsResponse{Counts: counts})
}

// Get serves GET /api/v1/emails/:id.
func (h *Handler) Get(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	e, err := h.Runtime.Store.Email.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if e == nil {
		return response.NotFound(c, "email not found")
	}

	return response.Success(c, EmailResponse{Email: e})
}

// clickHashRE pulls the link hash out of a click-redirect URL in a
// stored body: /tracking/click/<email id>/<hash>, hash being the 16
// hex characters tracking.HashLink emits.
var clickHashRE = regexp.MustCompile(`/tracking/click/[^/"'?]+/([0-9a-f]{16})`)

// TrackedLinks serves GET /api/v1/emails/:id/tracked-links: the
// original destination behind every click redirect in this message's
// body, keyed by link hash.
//
// It exists for the preview. Rendering our redirect would count a
// click by whoever is LOOKING at the message, so the preview strips
// the href - and without this map the anchor is left as bare text,
// which reads as the link having been destroyed. Its own endpoint
// rather than a field on Get, because the detail page polls Get every
// three seconds while a message is in flight and this mapping never
// changes after send.
func (h *Handler) TrackedLinks(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	e, err := h.Runtime.Store.Email.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if e == nil {
		return response.NotFound(c, "email not found")
	}

	seen := map[string]bool{}
	var hashes []string
	for _, m := range clickHashRE.FindAllStringSubmatch(e.HTMLBody, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			hashes = append(hashes, m[1])
		}
	}

	links, err := h.Runtime.Store.Campaign.TrackedLinkURLs(c.Context(), rc.Project.ID, hashes)
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, TrackedLinksResponse{Links: links})
}

// Attachment streams one attachment's decoded bytes, reading inline
// content or the blob store as appropriate.
func (h *Handler) Attachment(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	e, err := h.Runtime.Store.Email.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if e == nil {
		return response.NotFound(c, "email not found")
	}

	idx, err := strconv.Atoi(c.Params("idx"))
	if err != nil || idx < 0 || idx >= len(e.Attachments) {
		return response.NotFound(c, "attachment not found")
	}

	a := e.Attachments[idx]
	raw, err := LoadAttachment(c.Context(), h.Runtime.Blob, &a)
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Attachment(c, a.Filename, a.ContentType, raw)
}

// Status is the cheap polling endpoint: just the delivery state.
func (h *Handler) Status(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	e, err := h.Runtime.Store.Email.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if e == nil {
		return response.NotFound(c, "email not found")
	}

	return response.Success(c, StatusResponse{
		ID:           e.ID,
		Status:       e.Status,
		Attempts:     e.Attempts,
		ErrorMessage: e.ErrorMessage,
		SentAt:       e.SentAt,
	})
}

// Retry serves POST /api/v1/emails/:id/retry.
func (h *Handler) Retry(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	svc := NewService(h.Runtime)
	e, err := svc.Retry(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return sendFailure(c, err)
	}

	if e == nil {
		return response.NotFound(c, "email not found")
	}

	return response.Success(c, EmailResponse{Email: e})
}

// toRequest converts the bound input into the service request, parsing send_at.
func (in *sendInput) toRequest() (*SendRequest, error) {
	req := &SendRequest{
		From:        in.From,
		To:          in.To,
		Subject:     in.Subject,
		HTML:        in.HTML,
		Text:        in.Text,
		Headers:     in.Headers,
		Attachments: in.Attachments,

		UnsubscribeListID:     in.UnsubscribeListID,
		ListUnsubscribeURL:    in.ListUnsubscribeURL,
		ListUnsubscribeMailto: in.ListUnsubscribeMailto,
		ListUnsubscribePost:   in.ListUnsubscribePost,
	}
	if in.SendAt != "" {
		t, err := time.Parse(time.RFC3339, in.SendAt)
		if err != nil {
			return nil, errors.New("send_at must be an RFC 3339 timestamp")
		}

		utc := t.UTC()
		req.SendAt = &utc
	}

	return req, nil
}

// sendFailure maps service errors: caller mistakes to 400, the rest to 500.
func sendFailure(c fiber.Ctx, err error) error {
	if re, ok := errors.AsType[*RequestError](err); ok {
		return response.BadRequest(c, re.Error())
	}

	if qe, ok := errors.AsType[*quota.Error](err); ok {
		return response.TooManyRequests(c, qe.Error())
	}

	return response.Internal(c, err)
}

// callerID returns the authenticated user's id, or "" when auth is disabled.
func callerID(rc *domain.RequestContext) string {
	if rc == nil || rc.User == nil {
		return ""
	}

	return rc.User.ID
}

// apiKeyID returns the machine credential's id when the request came
// through machineAuth's key branch, "" when a session made the call.
func apiKeyID(rc *domain.RequestContext) string {
	if rc == nil || rc.APIKey == nil {
		return ""
	}

	return rc.APIKey.ID
}
