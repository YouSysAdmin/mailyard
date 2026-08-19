// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpcredential

import (
	"errors"
	"net"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain"
	scmodel "github.com/yousysadmin/mailyard/internal/models/smtpcredential"
)

// Handler owns the console /api/smtp-credentials surface. Mounted
// behind requireAuth + requireProject with per-route role gates.
type Handler struct {
	Runtime *env.Runtime
}

// List returns the project credentials plus the listener settings
// the client needs to actually connect. Bundling them means the
// console can render ready-to-paste connection details and warn when
// the listener is switched off, without a second round trip.
func (h *Handler) List(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	creds, err := h.Runtime.Store.SMTPCredential.List(c.Context(), rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if creds == nil {
		creds = []*scmodel.Credential{}
	}

	return response.Success(c, ListResponse{
		SMTPCredentials: creds,
		Submission:      h.listenerInfo(),
	})
}

// Create mints a credential and returns the plaintext password
// EXACTLY ONCE.
func (h *Handler) Create(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[createInput](c)
	if !ok {
		return resp
	}

	// GetByUsername returns a single row and the column is UNIQUE, so
	// a collision would be a hard insert failure rather than a silent
	// shadow. 64 bits makes it vanishingly unlikely either way, and a
	// retry at mint time removes the edge entirely.
	var username, password, hash string
	var err error
	for attempt := 0; ; attempt++ {
		username, password, hash, err = scmodel.Generate()
		if err != nil {
			return response.Internal(c, err)
		}

		existing, gerr := h.Runtime.Store.SMTPCredential.GetByUsername(c.Context(), username)
		if gerr != nil {
			return response.Internal(c, gerr)
		}

		if existing == nil {
			break
		}

		if attempt >= 4 {
			return response.Internal(c, errors.New("could not mint a unique username"))
		}
	}

	createdBy := ""
	if rc.User != nil {
		createdBy = rc.User.ID
	}

	groupID := ""
	if in.SMTPGroup != "" {
		g, err := h.Runtime.Store.SMTPGroup.GetBySlug(c.Context(), rc.Project.ID, in.SMTPGroup)
		if err != nil {
			return response.Internal(c, err)
		}

		if g == nil {
			return response.BadRequest(c, "smtp server group "+in.SMTPGroup+" does not exist")
		}

		groupID = g.ID
	}

	cred := &scmodel.Credential{
		ID:           ids.New(),
		ProjectID:    rc.Project.ID,
		CreatedBy:    createdBy,
		Name:         in.Name,
		Username:     username,
		PasswordHash: hash,
		AllowedIPs:   in.AllowedIPs,
		SMTPGroupID:  groupID,
		Sandbox:      in.Sandbox,
	}
	if cred.AllowedIPs == nil {
		cred.AllowedIPs = []string{}
	}

	if err := h.Runtime.Store.SMTPCredential.Put(c.Context(), cred); err != nil {
		return response.Internal(c, err)
	}

	return response.Created(c, CreatedResponse{
		SMTPCredential: cred,
		Password:       password,
		Submission:     h.listenerInfo(),
	})
}

// Revoke serves POST /api/v1/smtp-credentials/:id/revoke.
func (h *Handler) Revoke(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	cred, err := h.Runtime.Store.SMTPCredential.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if cred == nil {
		return response.NotFound(c, "smtp credential not found")
	}

	if err := h.Runtime.Store.SMTPCredential.Revoke(c.Context(), rc.Project.ID, cred.ID); err != nil {
		return response.Internal(c, err)
	}

	cred.Revoked = true

	return response.Success(c, CredentialResponse{SMTPCredential: cred})
}

// Delete serves DELETE /api/v1/smtp-credentials/:id.
func (h *Handler) Delete(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	cred, err := h.Runtime.Store.SMTPCredential.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if cred == nil {
		return response.NotFound(c, "smtp credential not found")
	}

	if err := h.Runtime.Store.SMTPCredential.Delete(c.Context(), rc.Project.ID, cred.ID); err != nil {
		return response.Internal(c, err)
	}

	return response.NoContent(c)
}

// listenerInfo describes the listener a client would point at. Host
// is the announced hostname (the EHLO name), which is the operator's
// own statement of where submission lives, falling back to the bind
// host when it looks routable.
func (h *Handler) listenerInfo() ListenerInfo {
	cfg := h.Runtime.Config.Submission
	host, port := splitAddr(cfg.Addr)
	if cfg.Hostname != "" {
		host = cfg.Hostname
	}

	return ListenerInfo{
		Enabled:  cfg.Enabled,
		Host:     host,
		Port:     port,
		STARTTLS: cfg.TLS.Enabled,
	}
}

// splitAddr pulls host and port out of a bind address. A wildcard or
// empty host is reported as localhost - it is the only thing a
// reader can actually dial.
func splitAddr(addr string) (string, string) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "localhost", strings.TrimPrefix(addr, ":")
	}

	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}

	return host, port
}
