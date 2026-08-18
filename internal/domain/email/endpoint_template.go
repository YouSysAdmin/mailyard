// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"github.com/gofiber/fiber/v2"

	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain"
)

// SendTemplate renders a stored template and queues the result.
func (h *Handler) SendTemplate(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[templateSendInput](c)
	if !ok {
		return resp
	}

	base := &sendInput{From: in.From, To: in.To, Headers: in.Headers,
		Attachments: in.Attachments, SendAt: in.SendAt,
		UnsubscribeListID:     in.UnsubscribeListID,
		ListUnsubscribeURL:    in.ListUnsubscribeURL,
		ListUnsubscribeMailto: in.ListUnsubscribeMailto,
		ListUnsubscribePost:   in.ListUnsubscribePost,
		Sandbox:               in.Sandbox,
		SandboxRetentionDays:  in.SandboxRetentionDays}
	req, err := base.toRequest()
	if err != nil {
		return response.BadRequest(c, err.Error())
	}

	ref := &TemplateRef{ID: in.TemplateID, Name: in.TemplateName, Language: in.Language, Data: in.Data}

	svc := NewService(h.Runtime)

	// The template is RENDERED and only then captured. Capturing the
	// unrendered source would hand a developer a message full of
	// placeholders and tell them nothing about the thing they are
	// testing, which is what the template produces for this data.
	if want, refusal := sandboxIntent(rc, base); refusal != "" {
		return response.BadRequest(c, refusal)
	} else if want {
		out, t, rerr := svc.RenderTemplate(c.UserContext(), rc.Project.ID, ref)
		if rerr != nil {
			return sendFailure(c, rerr)
		}

		// A capture is not a delivery, so the per-message links the
		// reserved variables stand for do not exist. Stripped rather
		// than left, or the raw view - whose whole promise is "exactly
		// what would have been sent" - shows an internal path.
		StripSystemLinks(out)

		// dry_run persists nothing, and a capture is persistence - it
		// consumed a sandbox slot and evicted a real capture. The
		// render above already exercised what a sandbox send checks.
		if in.DryRun {
			return response.Success(c, TemplateDryRunResponse{
				DryRun: true, Valid: true, Template: t.Name, Preview: out,
			})
		}

		req.Subject, req.HTML, req.Text, req.TemplateName = out.Subject, out.HTML, out.Text, t.Name
		if aerr := svc.AttachTemplateFiles(c.UserContext(), rc.Project.ID, t.ID, req); aerr != nil {
			return response.Internal(c, aerr)
		}

		_, cerr := h.captureSandbox(c, rc, req, in.SandboxRetentionDays)

		return cerr
	}

	if req.Route, err = svc.ResolveRoute(c.UserContext(), rc.Project.ID, in.SMTPServerID, in.SMTPGroup); err != nil {
		return sendFailure(c, err)
	}

	if in.DisableTracking {
		req.Track = TrackOff
	}

	if in.DryRun {
		out, t, err := svc.RenderTemplate(c.UserContext(), rc.Project.ID, ref)
		if err != nil {
			return sendFailure(c, err)
		}

		// Nothing is persisted, so no id is minted and the reserved
		// variables have nothing to resolve to.
		StripSystemLinks(out)

		req.Subject = out.Subject
		req.HTML = out.HTML
		req.Text = out.Text
		if err := svc.Validate(c.UserContext(), rc.Project.ID, req); err != nil {
			return sendFailure(c, err)
		}

		return response.Success(c, TemplateDryRunResponse{
			DryRun: true, Valid: true, Template: t.Name, Preview: out,
		})
	}

	e, blocked, err := svc.SendWithTemplate(c.UserContext(), rc.Project.ID, callerID(rc), apiKeyID(rc), ref, req)
	if err != nil {
		return sendFailure(c, err)
	}

	return response.Created(c, SendResponse{Email: e, Suppressed: emptyIfNil(blocked)})
}

// Batch queues up to 100 sends in one call and reports per-item
// outcomes (207-style in a 200 body).
func (h *Handler) Batch(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[batchInput](c)
	if !ok {
		return resp
	}

	// Batch does not capture, and it must therefore REFUSE rather than
	// fall through. Falling through would deliver, for real, on the
	// one credential an operator handed out specifically so it could
	// not - and would look like success while doing it. A per-item
	// sandbox result shape is the missing piece, not the intent.
	if rc != nil && rc.APIKey != nil && rc.APIKey.Sandbox {
		return response.BadRequest(c,
			"batch sends are not available on a sandbox api key - "+
				"send the items individually, or use a key without the sandbox flag")
	}

	var ref *TemplateRef
	if in.TemplateID != "" || in.TemplateName != "" {
		ref = &TemplateRef{ID: in.TemplateID, Name: in.TemplateName, Language: in.Language}
	}

	// A conversion, not a field-by-field literal. The two types are
	// structurally identical by design (DTO with tags, domain type
	// without), and the conversion is what keeps them that way: add a
	// field to one and this stops compiling. The literal it replaced
	// would instead have silently dropped the new field, which for a
	// per-item send option is a bug nobody would see.
	items := make([]BatchItem, len(in.Items))
	for i, it := range in.Items {
		items[i] = BatchItem(it)
	}

	svc := NewService(h.Runtime)
	results := svc.SendBatch(c.UserContext(), rc.Project.ID, callerID(rc), apiKeyID(rc), in.From, ref, items)
	accepted := 0
	for _, r := range results {
		if r.Error == "" {
			accepted++
		}
	}

	return response.Success(c, BatchResponse{
		Total: len(results), Accepted: accepted, Results: results,
	})
}

// RenderPreview renders a template send without queueing anything.
func (h *Handler) RenderPreview(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[renderPreviewInput](c)
	if !ok {
		return resp
	}

	svc := NewService(h.Runtime)
	out, t, err := svc.RenderTemplate(c.UserContext(), rc.Project.ID,
		&TemplateRef{ID: in.TemplateID, Name: in.TemplateName, Language: in.Language, Data: in.Data})
	if err != nil {
		return sendFailure(c, err)
	}

	StripSystemLinks(out)

	return response.Success(c, PreviewResponse{Template: t.Name, Preview: out})
}

// SendTest sends a template to a handful of addresses for a live
// check from the editor. Registered on the templates route group
// (POST /api/templates/:id/send-test).
func (h *Handler) SendTest(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[testSendInput](c)
	if !ok {
		return resp
	}

	svc := NewService(h.Runtime)
	ref := &TemplateRef{ID: c.Params("id"), Language: in.Language, Data: in.Data}
	e, blocked, err := svc.SendWithTemplate(c.UserContext(), rc.Project.ID, callerID(rc), apiKeyID(rc),
		ref, &SendRequest{From: in.From, To: in.To})
	if err != nil {
		return sendFailure(c, err)
	}

	return response.Created(c, SendResponse{Email: e, Suppressed: emptyIfNil(blocked)})
}
