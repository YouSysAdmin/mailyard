// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package auth

import (
	"log/slog"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/safetext"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	smodel "github.com/yousysadmin/mailyard/internal/models/setting"
)

// SystemMailStatus reports the platform mail configuration to a
// platform admin: the address, the project it is sent as, and what, if
// anything, stops it. Secrets are never echoed - the project's servers
// are configured on their own pages, and this only says which project
// is in use.
func (h *Handler) SystemMailStatus(c fiber.Ctx) error {
	out := SystemMailStatus{
		Enabled:   h.Runtime.SystemMail.Enabled(),
		From:      h.Runtime.Settings.String(smodel.KeyPlatformMailFrom),
		FromName:  h.Runtime.Settings.String(smodel.KeyPlatformMailFromName),
		ProjectID: h.Runtime.SystemMail.Project(),
	}

	if out.ProjectID != "" {
		p, err := h.Runtime.Store.Project.Get(c.Context(), out.ProjectID)
		if err != nil {
			return response.Internal(c, err)
		}

		if p == nil {
			out.Problem = "platform_mail_project names a project that no longer exists"

			return response.Success(c, SystemMailStatusResponse{SystemMail: out})
		}

		out.Project = p.Name
	}

	// Check names whichever setting is missing, or what stops the
	// project carrying the address - the answer an admin acts on.
	if err := h.Runtime.SystemMail.Check(c.Context()); err != nil {
		out.Problem = err.Error()
	}

	return response.Success(c, SystemMailStatusResponse{SystemMail: out})
}

// SystemMailTest checks that platform mail can leave, and queues a real
// message when the caller names a recipient. Platform admin only.
func (h *Handler) SystemMailTest(c fiber.Ctx) error {
	in, resp, ok := validation.Bind[systemMailTestInput](c)
	if !ok {
		return resp
	}

	// The detail goes to the log, the answer says which step failed -
	// platform admin or not, an API answer is copied around.
	if err := h.Runtime.SystemMail.Check(c.Context()); err != nil {
		slog.Warn("systemmail: check failed", "err", err)

		return response.BadRequest(c, "platform mail cannot be sent, see the server log for the reason")
	}

	if in.To == "" {
		return response.Success(c, MessageResponse{Message: "Platform mail is ready to send."})
	}

	const subject = "Mailyard system mail test"
	body := "This is a test of the Mailyard system mail settings. If you are reading it, invitations and password resets can be delivered."
	if err := h.Runtime.SystemMail.Send(c.Context(), []string{in.To}, subject,
		"<p>"+body+"</p>", body+"\n"); err != nil {
		slog.Warn("systemmail: test send failed", "to", safetext.MaskAddress(in.To), "err", err)

		return response.BadRequest(c, "send failed, see the server log for the reason")
	}

	// Queued, not sent on the spot: the outcome is the worker's to
	// report, and the message's status is in the log.
	return response.Success(c, MessageResponse{Message: "Test message queued for delivery to " + in.To + "."})
}
