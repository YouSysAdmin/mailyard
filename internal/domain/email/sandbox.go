// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package email

import (
	"encoding/base64"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
	"github.com/yousysadmin/mailyard/internal/domain"
	"github.com/yousysadmin/mailyard/internal/domain/sandbox"
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

// captureSandbox stores req in the project sandbox and writes the
// response. Callers check the bool: true means the request is fully
// answered and nothing else should touch it.
//
// The message is RENDERED first, through the same builder the SMTP
// client would have used. That costs one Build and is what makes the
// raw view worth having - a developer sees an actual RFC 5322 message
// with the headers we would have put on the wire, rather than a
// pretty-printed copy of our internal struct.
func (h *Handler) captureSandbox(c fiber.Ctx, rc *domain.RequestContext, req *SendRequest, retentionDays int) (bool, error) {
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
			return true, response.Internal(c, err)
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
	msg := &smtpclient.Message{
		From:                  req.From,
		To:                    req.To,
		Subject:               req.Subject,
		HTML:                  req.HTML,
		Text:                  req.Text,
		Attachments:           toClientAttachments(req.Attachments),
		Headers:               req.Headers,
		ListUnsubscribeURL:    req.ListUnsubscribeURL,
		ListUnsubscribeMailto: req.ListUnsubscribeMailto,
		ListUnsubscribePost:   req.ListUnsubscribePost,
	}
	apiKeyID := ""
	if rc.APIKey != nil {
		apiKeyID = rc.APIKey.ID
	}

	e, err := svc.Capture(c.Context(), &sandbox.Request{
		ProjectID:     rc.Project.ID,
		Source:        sbmodel.SourceAPI,
		APIKeyID:      apiKeyID,
		EnvelopeFrom:  req.From,
		Recipients:    req.To,
		Raw:           msg.Build(),
		RetentionDays: retentionDays,
	})
	if err != nil {
		return true, response.Internal(c, err)
	}

	// 201 with the sandbox row, not a fake email row. A caller that
	// treats this like a send and looks the id up in /emails would get
	// a 404, and a shape that invited that would be worse than one
	// that plainly says what happened.
	return true, response.Created(c, SandboxCaptureResponse{SandboxEmail: e, Sandboxed: true})
}
