// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/ids"
	"github.com/yousysadmin/mailyard/internal/core/keyset"
	"github.com/yousysadmin/mailyard/internal/core/paging"
	"github.com/yousysadmin/mailyard/internal/core/quota"
	"github.com/yousysadmin/mailyard/internal/core/response"
	coretracking "github.com/yousysadmin/mailyard/internal/core/tracking"
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

	// The reserved {{ mailyard_* }} names resolve here as they do in a
	// template: marked now, and swapped for this message's links once
	// Send has minted its id. Nothing else in the body is read as a
	// variable.
	markSystemVars(req)

	// Before ResolveRoute and before Validate, and that ordering is
	// the feature. Both of those ask questions about DELIVERY - is
	// there a server, is the sender's domain verified, is the project
	// within its plan - and a developer testing a signup flow in a
	// project with nothing configured answers no to all of them. The
	// sandbox is for exactly that developer.
	svc := NewService(h.Runtime)
	if want, refusal := sandboxIntent(rc, &in); refusal != "" {
		return response.BadRequest(c, refusal)
	} else if want {
		if denied := optInRefusal(rc); denied != "" {
			return response.Forbidden(c, denied)
		}

		// A capture is not a delivery, so the links the reserved names
		// stand for do not exist. Removed, as a template capture does.
		stripSystemVars(req)

		// The message half of Validate still applies: a capture is a
		// message the builder is handed, and the sandbox is not a way
		// around a line break in a header or the attachment ceiling.
		if err := svc.ValidateShape(req); err != nil {
			return sendFailure(c, err)
		}

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

		return h.captureOnce(c, rc, req, in.SandboxRetentionDays)
	}

	// Resolved before validation so an unknown group is a 400 naming
	// the group, not a confusing "no smtp server accepts this sender".
	if req.Route, err = svc.ResolveRoute(c.Context(), rc.Project.ID, in.SMTPServerID, in.SMTPGroup); err != nil {
		return sendFailure(c, err)
	}

	if in.DisableTracking {
		req.Track = TrackOff
	}

	// The checks Send runs, through the service, so a dry run cannot
	// disagree with the send it stands for - the plan's volume and a
	// list-scoped opt-out included.
	if in.DryRun {
		blocked, err := svc.DryRun(c.Context(), rc.Project.ID, req)
		if err != nil {
			return sendFailure(c, err)
		}

		return response.Success(c, DryRunResponse{
			DryRun: true, Valid: true, Recipients: len(req.To),
			Suppressed: emptyIfNil(blocked),
		})
	}

	return h.sendOnce(c, rc, func() (*emailmodel.Email, []string, error) {
		return svc.Send(c.Context(), rc.Project.ID, callerID(rc), apiKeyID(rc), req)
	})
}

// maxIdempotencyKey bounds the header. A key is an id the caller
// minted, and anything longer than this is a body in a header.
const maxIdempotencyKey = 255

// idempotencyKey reads the request's Idempotency-Key, trimmed. The
// refusal is non-empty when the key is too long.
func idempotencyKey(c fiber.Ctx) (key, refusal string) {
	key = strings.TrimSpace(c.Get("Idempotency-Key"))
	if len(key) > maxIdempotencyKey {
		return "", "Idempotency-Key must be at most 255 characters"
	}

	return key, ""
}

// sendOnce runs send under the request's Idempotency-Key, when it
// carries one.
//
// The key is reserved BEFORE the send and completed after, so a
// duplicate that arrives while the first request is still running is
// told to wait rather than handed nothing or given a second message.
// A send that fails releases the key, because the retry that follows
// a 4xx is a corrected request, not a duplicate. Scoped to the
// project, the way every send resource is.
func (h *Handler) sendOnce(c fiber.Ctx, rc *domain.RequestContext, send func() (*emailmodel.Email, []string, error)) error {
	key, refusal := idempotencyKey(c)
	if refusal != "" {
		return response.BadRequest(c, refusal)
	}

	if key == "" {
		e, blocked, err := send()
		if err != nil {
			return sendFailure(c, err)
		}

		return response.Created(c, SendResponse{Email: e, Suppressed: emptyIfNil(blocked)})
	}

	keys := h.Runtime.Store.Email
	held, reserved, err := keys.ReserveKey(c.Context(), rc.Project.ID, key)
	if err != nil {
		return response.Internal(c, err)
	}

	if !reserved {
		return h.replay(c, rc, held)
	}

	e, blocked, err := send()
	if err != nil {
		h.releaseKey(c, rc, key)

		return sendFailure(c, err)
	}

	if cerr := keys.CompleteKey(c.Context(), rc.Project.ID, key, e.ID); cerr != nil {
		h.Runtime.Log.Warn("email: completing an idempotency key", "project_id", rc.Project.ID, "email_id", e.ID, "err", cerr)
	}

	return response.Created(c, SendResponse{Email: e, Suppressed: emptyIfNil(blocked)})
}

// captureOnce is sendOnce for a sandboxed send: the same ledger, with
// the capture recorded against the key instead of a message.
func (h *Handler) captureOnce(c fiber.Ctx, rc *domain.RequestContext, req *SendRequest, retentionDays int) error {
	key, refusal := idempotencyKey(c)
	if refusal != "" {
		return response.BadRequest(c, refusal)
	}

	if key == "" {
		e, err := h.captureSandbox(c, rc, req, retentionDays)
		if err != nil {
			return response.Internal(c, err)
		}

		return response.Created(c, SandboxCaptureResponse{SandboxEmail: e, Sandboxed: true})
	}

	keys := h.Runtime.Store.Email
	held, reserved, err := keys.ReserveKey(c.Context(), rc.Project.ID, key)
	if err != nil {
		return response.Internal(c, err)
	}

	if !reserved {
		return h.replay(c, rc, held)
	}

	e, err := h.captureSandbox(c, rc, req, retentionDays)
	if err != nil {
		h.releaseKey(c, rc, key)

		return response.Internal(c, err)
	}

	if cerr := keys.CompleteSandboxKey(c.Context(), rc.Project.ID, key, e.ID); cerr != nil {
		h.Runtime.Log.Warn("email: completing an idempotency key", "project_id", rc.Project.ID, "sandbox_email_id", e.ID, "err", cerr)
	}

	return response.Created(c, SandboxCaptureResponse{SandboxEmail: e, Sandboxed: true})
}

// replay answers a request whose Idempotency-Key was already reserved:
// wait while the first request runs, else what it produced.
func (h *Handler) replay(c fiber.Ctx, rc *domain.RequestContext, held emailmodel.KeyHolder) error {
	if held.Pending() {
		key := strings.TrimSpace(c.Get("Idempotency-Key"))

		return response.Conflict(c, fmt.Sprintf("a request with Idempotency-Key %q is still being processed", key))
	}

	if held.SandboxEmailID != "" {
		sb, err := h.Runtime.Store.Sandbox.Get(c.Context(), rc.Project.ID, held.SandboxEmailID)
		if err != nil {
			return response.Internal(c, err)
		}

		if sb == nil {
			return response.Gone(c, "the capture this Idempotency-Key produced is no longer in the sandbox")
		}

		return response.Success(c, SandboxCaptureResponse{SandboxEmail: sb, Sandboxed: true, Replayed: true})
	}

	e, err := h.Runtime.Store.Email.Get(c.Context(), rc.Project.ID, held.EmailID)
	if err != nil {
		return response.Internal(c, err)
	}

	if e == nil {
		return response.Gone(c, "the message this Idempotency-Key produced is no longer in the log")
	}

	return response.Success(c, SendResponse{Email: e, Suppressed: emptyIfNil(nil), Replayed: true})
}

// releaseKey gives a reserved key back after a failed send.
func (h *Handler) releaseKey(c fiber.Ctx, rc *domain.RequestContext, key string) {
	if err := h.Runtime.Store.Email.ReleaseKey(c.Context(), rc.Project.ID, key); err != nil {
		h.Runtime.Log.Warn("email: releasing an idempotency key", "project_id", rc.Project.ID, "err", err)
	}
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
	w := paging.WindowFrom(c)
	f := Filter{
		Sender:       paging.Search(c, "sender"),
		Recipient:    paging.Search(c, "recipient"),
		Template:     paging.Search(c, "template"),
		Tag:          paging.Search(c, "tag"),
		APIKeyID:     strings.TrimSpace(c.Query("api_key_id")),
		SMTPServerID: strings.TrimSpace(c.Query("smtp_server_id")),
		Search:       paging.Search(c, "search"),
		Limit:        w.Fetch(),
		Cursor:       w.Cursor,
	}

	var err error
	if f.Exact, err = paging.Exact(c); err != nil {
		return response.BadRequest(c, err.Error())
	}

	// A list, so one request answers "anything that did not go out".
	// Each value is checked by name: an unknown status would match
	// nothing and read as an empty log.
	for status := range strings.SplitSeq(c.Query("status"), ",") {
		status = strings.ToLower(strings.TrimSpace(status))
		if status == "" {
			continue
		}

		if !emailmodel.ValidStatus(status) {
			return response.BadRequest(c, "unknown status "+status)
		}

		f.Statuses = append(f.Statuses, status)
	}

	for name, id := range map[string]string{"api_key_id": f.APIKeyID, "smtp_server_id": f.SMTPServerID} {
		if id != "" && !ids.Valid(id) {
			return response.BadRequest(c, name+" must be a uuid")
		}
	}

	if f.From, f.To, err = paging.TimeWindow(c); err != nil {
		return response.BadRequest(c, err.Error())
	}

	if f.After, err = paging.Instant(c, "after"); err != nil {
		return response.BadRequest(c, "after "+err.Error())
	}

	rows, err := h.Runtime.Store.Email.List(c.Context(), rc.Project.ID, f)
	if err != nil {
		return response.Internal(c, err)
	}

	page, more := keyset.Cut(rows, w.Limit)
	if page == nil {
		page = []*emailmodel.Email{}
	}

	next := ""
	if more && len(page) > 0 {
		last := page[len(page)-1]
		next = keyset.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}.Encode()
	}

	return response.Success(c, ListResponse{Emails: page, NextCursor: next})
}

// Stats returns per-status counts for the project.
func (h *Handler) Stats(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	from, to, err := paging.TimeWindow(c)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}

	counts, err := h.Runtime.Store.Email.CountByStatus(c.Context(), rc.Project.ID, from, to)
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

	return response.Success(c, EmailResponse{
		Email:      e,
		SentVia:    resolveSentVia(c.Context(), h.Runtime, e),
		Addressing: splitRecipients(e),
	})
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
	raw, err := LoadAttachment(c.Context(), h.Runtime.Store.Template, h.Runtime.Blob, h.Runtime.Attachments, rc.Project.ID, &a)
	if errors.Is(err, ErrAttachmentGone) {
		return response.NotFound(c, "attachment content is no longer stored")
	}

	if err != nil {
		return response.Internal(c, err)
	}

	return response.Attachment(c, a.Filename, a.ContentType, raw)
}

// EML serves GET /api/v1/emails/:id/eml, the message as an .eml file.
//
// Built by the same code that builds it for delivery, so the file is
// what the recipient was sent - short of the DKIM signature, which is
// applied on the way out, and the Message-ID, minted per attempt and
// not stored, so the file carries a stable one of its own. Dated when
// it was sent, or when it was accepted while it still waits.
func (h *Handler) EML(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	e, err := h.Runtime.Store.Email.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if e == nil {
		return response.NotFound(c, "email not found")
	}

	attachments, err := rehydrate(c.Context(), h.Runtime.Store.Template, h.Runtime.Blob, h.Runtime.Attachments, e)
	if errors.Is(err, ErrAttachmentGone) {
		return response.NotFound(c, "attachment content is no longer stored")
	}

	if err != nil {
		return response.Internal(c, err)
	}

	msg := newMessage(e, attachments)
	msg.Date = e.CreatedAt
	if e.SentAt != nil {
		msg.Date = *e.SentAt
	}

	// Stable across downloads, so a mail client sees one message and
	// not one per download, and under a reserved domain because this
	// id was never on the wire - the sent one is minted per attempt.
	msg.MessageID = e.ID + "@outbound.invalid"

	// Signed as the delivery was, with the sender's current key - the
	// signature itself is minted per build, so this one is fresh, and
	// verifies the same way.
	if e.Signing != "" {
		if err := signAs(c.Context(), h.Runtime.Store, e.ProjectID, e.Sender, msg); err != nil {
			return response.Internal(c, err)
		}
	}

	raw, err := msg.Build()
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Attachment(c, e.ID+".eml", "message/rfc822", raw)
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

	return response.Success(c, EmailResponse{
		Email:      e,
		SentVia:    resolveSentVia(c.Context(), h.Runtime, e),
		Addressing: splitRecipients(e),
	})
}

// Cancel serves POST /api/v1/emails/:id/cancel.
func (h *Handler) Cancel(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	svc := NewService(h.Runtime)
	e, err := svc.Cancel(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return sendFailure(c, err)
	}

	if e == nil {
		return response.NotFound(c, "email not found")
	}

	return response.Success(c, EmailResponse{
		Email:      e,
		SentVia:    resolveSentVia(c.Context(), h.Runtime, e),
		Addressing: splitRecipients(e),
	})
}

// callerAttachments keeps what a caller may say about an attachment.
// A storage key, a template attachment id or an image id names stored
// bytes, and an embedded part is the template's, which only the server
// decides.
func callerAttachments(in []emailmodel.Attachment) []emailmodel.Attachment {
	if len(in) == 0 {
		return in
	}

	out := make([]emailmodel.Attachment, len(in))
	for i, a := range in {
		a.StorageKey = ""
		a.TemplateAttachmentID = ""
		a.TemplateAssetID = ""
		a.ContentID = ""
		out[i] = a
	}

	return out
}

// markSystemVars turns the reserved names in a plain send's subject and
// bodies into the placeholders Send resolves.
func markSystemVars(req *SendRequest) {
	req.Subject = coretracking.MarkSystemVars(req.Subject)
	req.HTML = coretracking.MarkSystemVars(req.HTML)
	req.Text = coretracking.MarkSystemVars(req.Text)
}

// stripSystemVars removes the placeholders from a message that will
// not be delivered.
func stripSystemVars(req *SendRequest) {
	req.Subject = coretracking.SubstituteSystemLinks(req.Subject, coretracking.Links{})
	req.HTML = coretracking.SubstituteSystemLinks(req.HTML, coretracking.Links{})
	req.Text = coretracking.SubstituteSystemLinks(req.Text, coretracking.Links{})
}

// toRequest converts the bound input into the service request, parsing send_at.
func (in *sendInput) toRequest() (*SendRequest, error) {
	req := &SendRequest{
		From:        in.From,
		ReplyTo:     in.ReplyTo,
		Subject:     in.Subject,
		HTML:        in.HTML,
		Text:        in.Text,
		Headers:     in.Headers,
		Tags:        in.Tags,
		Metadata:    in.Metadata,
		Attachments: callerAttachments(in.Attachments),

		UnsubscribeListID:     in.UnsubscribeListID,
		ListUnsubscribeURL:    in.ListUnsubscribeURL,
		ListUnsubscribeMailto: in.ListUnsubscribeMailto,
		ListUnsubscribePost:   in.ListUnsubscribePost,
		// With the request rather than beside DisableTracking in the
		// handler: the sandbox capture reads it, and that runs before
		// the delivery questions.
		DisableSigning: in.DisableSigning,
	}
	req.To, req.HeaderTo, req.Cc = foldRecipients(in.To, in.Cc, in.Bcc)
	if in.SendAt != "" {
		t, err := time.Parse(time.RFC3339, in.SendAt)
		if err != nil {
			return nil, errors.New("send_at must be an RFC 3339 timestamp")
		}

		req.SendAt = new(t.UTC())
	}

	return req, nil
}

// sendFailure maps service errors: caller mistakes to 400, the rest to 500.
func sendFailure(c fiber.Ctx, err error) error {
	if re, ok := errors.AsType[*RequestError](err); ok {
		return response.BadRequest(c, re.Error())
	}

	if ce, ok := errors.AsType[*ConflictError](err); ok {
		return response.Conflict(c, ce.Error())
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
