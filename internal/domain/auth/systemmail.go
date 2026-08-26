// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package auth

import (
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/safetext"
	"github.com/yousysadmin/mailyard/internal/core/systemmail"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	smodel "github.com/yousysadmin/mailyard/internal/models/setting"
)

// SystemMailStatus reports the platform mail configuration to a
// platform admin: the address, and which shared pool server it would
// leave through. Secrets are never echoed - the pool page is where a
// server is configured, and this page only says which one is in use.
func (h *Handler) SystemMailStatus(c fiber.Ctx) error {
	out := SystemMailStatus{
		Enabled:  h.Runtime.SystemMail.Enabled(),
		From:     h.Runtime.Settings.String(smodel.KeyPlatformMailFrom),
		FromName: h.Runtime.Settings.String(smodel.KeyPlatformMailFromName),
	}
	srv, err := h.Runtime.SystemMail.Server(c.Context())
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
func (h *Handler) SystemMailTest(c fiber.Ctx) error {
	in, resp, ok := validation.Bind[systemMailTestInput](c)
	if !ok {
		return resp
	}

	// The detail goes to the log, the answer says which step failed. A
	// dial error carries the peer's greeting or the resolver's view of
	// the name, which belongs with the operator's logs, not in a
	// response - platform admin or not, an API answer is copied around.
	if err := h.Runtime.SystemMail.TestConnection(c.Context()); err != nil {
		slog.Warn("systemmail: connection test failed", "err", err)

		return response.BadRequest(c, "connection failed, see the server log for the reason")
	}

	if in.To == "" {
		return response.Success(c, MessageResponse{Message: "Connection succeeded."})
	}

	const subject = "Mailyard system mail test"
	body := "This is a test of the Mailyard system mail settings. If you are reading it, invitations and password resets can be delivered."
	if err := h.Runtime.SystemMail.Send(c.Context(), []string{in.To}, subject,
		"<p>"+body+"</p>", body+"\n"); err != nil {
		slog.Warn("systemmail: test send failed", "to", safetext.MaskAddress(in.To), "err", err)

		return response.BadRequest(c, "send failed, see the server log for the reason")
	}

	return response.Success(c, MessageResponse{Message: "Test message sent to " + in.To + "."})
}
