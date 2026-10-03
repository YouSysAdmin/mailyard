// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"encoding/base64"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/sandbox"
	perm "github.com/yousysadmin/mailyard/internal/models/permission"
	sbmodel "github.com/yousysadmin/mailyard/internal/models/sandbox"
)

// sandboxIntent decides whether this request is captured rather than
// delivered, and is the one place that decides it on the HTTP surface.
//
// Two ways in, and they are not symmetric:
//
//   - the API key is marked sandbox, which captures everything sent
//     with it. This is the intended use: a developer swaps the key and
//     the application is otherwise untouched.
//   - an ordinary key passes "sandbox": true for one message, which is
//     the ad-hoc case.
//
// There is no way OUT. A sandbox key passing "sandbox": false is
// refused rather than obeyed, because obeying it would mean an
// application could send real mail with the credential an operator
// handed out precisely so it could not. Refused rather than silently
// ignored, so nobody is left believing a message went somewhere.
func sandboxIntent(rc *domain.RequestContext, in *sendInput) (want bool, refusal string) {
	keyed := rc != nil && rc.APIKey != nil && rc.APIKey.Sandbox
	if keyed && in.Sandbox != nil && !*in.Sandbox {
		return false, "this api key is sandbox-only, so sandbox cannot be set to false - " +
			"use a key without the sandbox flag to send for real"
	}

	if keyed {
		return true, ""
	}

	return in.Sandbox != nil && *in.Sandbox, ""
}

// optInRefusal answers why the caller may not capture into the
// sandbox, or "" when it may. A capture is a sandbox write, so a caller
// opting in per message needs sandbox:write. A sandbox key always holds
// it, since the flag carries it.
func optInRefusal(rc *domain.RequestContext) string {
	if rc.Permissions.Has(perm.ResourceSandbox, perm.ActionWrite) {
		return ""
	}

	return "permission " + string(perm.Of(perm.ResourceSandbox, perm.ActionWrite)) + " required"
}

// captureSandbox stores req in the project sandbox and returns the row.
//
// The message is RENDERED first, through the same builder the SMTP
// client would have used. That costs one Build and is what makes the
// raw view worth having - a developer sees an actual RFC 5322 message
// with the headers we would have put on the wire, rather than a
// pretty-printed copy of our internal struct.
func (h *Handler) captureSandbox(c fiber.Ctx, rc *domain.RequestContext, req *SendRequest, retentionDays int) (*sbmodel.Email, error) {
	// Blob-backed attachments carry a storage key and no content, and
	// only the delivery processor rehydrates them - which a capture
	// never reaches. Without this, a template send with a configured
	// blob store was captured with every attachment at zero bytes, and
	// the raw view's whole promise is "exactly what would have been
	// sent".
	for i := range req.Attachments {
		a := &req.Attachments[i]
		if a.Content != "" || a.StorageKey == "" {
			continue
		}

		raw, err := LoadAttachment(c.Context(), h.Runtime.Blob, a)
		if err != nil {
			return nil, err
		}

		a.Content = base64.StdEncoding.EncodeToString(raw)
	}

	// Built here rather than hung off Runtime, the same way the email
	// service is: it holds no state between requests, so a field would
	// only be a second place for it to be missing from.
	svc := &sandbox.Service{
		Store:    h.Runtime.Store.Sandbox,
		Settings: h.Runtime.Settings,
		Log:      h.Runtime.Log,
		All:      h.Runtime.Store,
	}

	// The project's default headers, so the raw view shows what a
	// delivery would have carried.
	sender := NewService(h.Runtime)
	if err := sender.withProjectDefaults(c.Context(), rc.Project.ID, req); err != nil {
		return nil, err
	}

	msg := captureMessage(req)
	apiKeyID := ""
	if rc.APIKey != nil {
		apiKeyID = rc.APIKey.ID
	}

	// Signed as a delivery would be, so the captured record shows the
	// multipart/signed shape a recipient's client would see.
	if reg := NewService(h.Runtime).registeredSender(c.Context(), rc.Project.ID, req.From); signingFor(reg, req.DisableSigning) != "" {
		if err := signAs(c.Context(), h.Runtime.Store, rc.Project.ID, req.From, msg); err != nil {
			return nil, err
		}
	}

	raw, err := msg.Build()
	if err != nil {
		return nil, err
	}

	e, err := svc.Capture(c.Context(), &sandbox.Request{
		ProjectID:     rc.Project.ID,
		Source:        sbmodel.SourceAPI,
		APIKeyID:      apiKeyID,
		EnvelopeFrom:  req.From,
		Recipients:    req.To,
		Raw:           raw,
		RetentionDays: retentionDays,
	})
	if err != nil {
		return nil, err
	}

	return e, nil
}

// captureMessage is the message a capture renders: the same fields the
// processor hands the builder for a delivery, read off the request
// instead of the stored row. The display To and Cc go with them - a
// Bcc recipient is in req.To and in no header, and a raw record that
// printed the envelope as To would show one where the delivered message
// never would.
func captureMessage(req *SendRequest) *smtpclient.Message {
	return &smtpclient.Message{
		From:                  req.From,
		To:                    req.To,
		HeaderTo:              req.HeaderTo,
		Cc:                    req.Cc,
		ReplyTo:               req.ReplyTo,
		Subject:               req.Subject,
		HTML:                  req.HTML,
		Text:                  req.Text,
		Attachments:           toClientAttachments(req.Attachments),
		Headers:               req.Headers,
		ListUnsubscribeURL:    req.ListUnsubscribeURL,
		ListUnsubscribeMailto: req.ListUnsubscribeMailto,
		ListUnsubscribePost:   req.ListUnsubscribePost,
	}
}
