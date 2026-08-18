// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package auth

import (
	"errors"

	"github.com/gofiber/fiber/v2"

	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/systemmail"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	smodel "github.com/yousysadmin/mailyard/internal/models/setting"
)

// SystemMailStatus reports the platform mail configuration to a
// platform admin: the address, and which shared pool server it would
// leave through. Secrets are never echoed - the pool page is where a
// server is configured, and this page only says which one is in use.
func (h *Handler) SystemMailStatus(c *fiber.Ctx) error {
	out := SystemMailStatus{
		Enabled:  h.Runtime.SystemMail.Enabled(),
		From:     h.Runtime.Settings.String(smodel.KeyPlatformMailFrom),
		FromName: h.Runtime.Settings.String(smodel.KeyPlatformMailFromName),
	}
	srv, err := h.Runtime.SystemMail.Server(c.UserContext())
	switch {
	case err == nil:
		out.Server = srv.Name
		out.Reserved = srv.PlatformOnly
	case errors.Is(err, systemmail.ErrNoServer):
		out.Problem = "no enabled server in the shared SMTP pool"
	default:
		return response.Internal(c, err)
	}

	return response.Success(c, SystemMailStatusResponse{SystemMail: out})
}

// SystemMailTest dials the configured server, and delivers a real
// message when the caller names a recipient. Platform admin only --
// it is an outbound request to an operator-chosen host.
func (h *Handler) SystemMailTest(c *fiber.Ctx) error {
	in, resp, ok := validation.Bind[systemMailTestInput](c)
	if !ok {
		return resp
	}

	if err := h.Runtime.SystemMail.TestConnection(c.UserContext()); err != nil {
		return response.BadRequest(c, "connection failed: "+err.Error())
	}

	if in.To == "" {
		return response.Success(c, MessageResponse{Message: "Connection succeeded."})
	}

	const subject = "Mailyard system mail test"
	body := "This is a test of the Mailyard system mail settings. If you are reading it, invitations and password resets can be delivered."
	if err := h.Runtime.SystemMail.Send(c.UserContext(), []string{in.To}, subject,
		"<p>"+body+"</p>", body+"\n"); err != nil {
		return response.BadRequest(c, "send failed: "+err.Error())
	}

	return response.Success(c, MessageResponse{Message: "Test message sent to " + in.To + "."})
}
