// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpserver

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
	"github.com/yousysadmin/mailyard/internal/core/transport"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

// SharedHandler owns /api/shared-smtp-servers, platform admin only.
//
// Note there is no project anywhere in this file. These servers
// belong to the platform, and no project-scoped route exposes them -
// a project gets delivery through the pool, never sight of it. The
// gate is requireAdmin at registration, not a check in here.
type SharedHandler struct {
	Runtime *env.Runtime
}

// List serves GET /api/v1/admin/shared-smtp-servers.
func (h *SharedHandler) List(c fiber.Ctx) error {
	servers, err := h.Runtime.Store.SharedSMTP.List(c.Context())
	if err != nil {
		return response.Internal(c, err)
	}

	if servers == nil {
		servers = []*ssmodel.Shared{}
	}

	return response.Success(c, SharedListResponse{
		SharedSMTPServers: servers,
		Providers:         transport.Providers(),
	})
}

// Get serves GET /api/v1/admin/shared-smtp-servers/:id.
func (h *SharedHandler) Get(c fiber.Ctx) error {
	srv, err := h.Runtime.Store.SharedSMTP.Get(c.Context(), c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if srv == nil {
		return response.NotFound(c, "shared smtp server not found")
	}

	return response.Success(c, SharedResponse{SharedSMTPServer: srv})
}

// Create serves POST /api/v1/admin/shared-smtp-servers.
func (h *SharedHandler) Create(c fiber.Ctx) error {
	in, resp, ok := validation.Bind[sharedCreateInput](c)
	if !ok {
		return resp
	}

	// Refused here, not at the first send - see the project handler.
	if err := transport.ValidateOptions(in.Provider, in.ProviderConfig); err != nil {
		return response.BadRequest(c, err.Error())
	}

	rc := domain.GetRequestContext(c)

	srv := &ssmodel.Shared{
		ID:             ids.New(),
		CreatedBy:      callerID(rc),
		Name:           in.Name,
		Host:           in.Host,
		Port:           in.Port,
		Username:       in.Username,
		Password:       in.Password,
		Encryption:     in.Encryption,
		SkipDKIM:       in.SkipDKIM,
		SESTopicARN:    in.SESTopicARN,
		Provider:       in.Provider,
		ProviderConfig: in.ProviderConfig,
		AllowedEmails:  in.AllowedEmails,
		AllowedDomains: in.AllowedDomains,
		Priority:       in.Priority,
		Status:         ssmodel.StatusEnabled,
		SecurityMode:   in.SecurityMode,
		PlatformOnly:   in.PlatformOnly,
	}
	normalizeShared(srv)
	if err := h.Runtime.Store.SharedSMTP.Put(c.Context(), srv); err != nil {
		return response.Internal(c, err)
	}

	h.invalidateSESTopics()
	h.Runtime.Log.Info("shared smtp: server created",
		"id", srv.ID, "host", srv.Host, "security_mode", srv.SecurityMode)

	return response.Created(c, SharedResponse{SharedSMTPServer: srv})
}

// Update serves PATCH /api/v1/admin/shared-smtp-servers/:id.
func (h *SharedHandler) Update(c fiber.Ctx) error {
	srv, err := h.Runtime.Store.SharedSMTP.Get(c.Context(), c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if srv == nil {
		return response.NotFound(c, "shared smtp server not found")
	}

	in, resp, ok := validation.Bind[sharedUpdateInput](c)
	if !ok {
		return resp
	}

	if in.Name != "" {
		srv.Name = in.Name
	}

	if in.Host != "" {
		srv.Host = in.Host
	}

	if in.Port != 0 {
		srv.Port = in.Port
	}

	if in.Username != nil {
		srv.Username = *in.Username
	}

	if in.Password != nil {
		srv.Password = *in.Password
	}

	if in.Encryption != "" {
		srv.Encryption = in.Encryption
	}

	if in.SkipDKIM != nil {
		srv.SkipDKIM = *in.SkipDKIM
	}

	if in.ProviderConfig != nil {
		srv.ProviderConfig = *in.ProviderConfig
	}

	if in.SESTopicARN != nil {
		srv.SESTopicARN = *in.SESTopicARN
	}

	if in.AllowedEmails != nil {
		srv.AllowedEmails = *in.AllowedEmails
	}

	if in.AllowedDomains != nil {
		srv.AllowedDomains = *in.AllowedDomains
	}

	if in.SecurityMode != "" {
		srv.SecurityMode = in.SecurityMode
	}

	if in.Priority != nil {
		srv.Priority = *in.Priority
	}

	if in.Status != "" {
		srv.Status = in.Status
	}

	if in.PlatformOnly != nil {
		srv.PlatformOnly = *in.PlatformOnly
	}

	// Dial settings changed under an invalid verdict: clear it rather
	// than carry a stale error forward, same as the per-project path.
	if srv.Status == ssmodel.StatusInvalid {
		srv.Status = ssmodel.StatusEnabled
		srv.ValidationError = ""
		srv.ValidatedAt = nil
	}

	normalizeShared(srv)
	if err := h.Runtime.Store.SharedSMTP.Put(c.Context(), srv); err != nil {
		return response.Internal(c, err)
	}

	h.invalidateSESTopics()

	return response.Success(c, SharedResponse{SharedSMTPServer: srv})
}

// Delete serves DELETE /api/v1/admin/shared-smtp-servers/:id.
func (h *SharedHandler) Delete(c fiber.Ctx) error {
	srv, err := h.Runtime.Store.SharedSMTP.Get(c.Context(), c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if srv == nil {
		return response.NotFound(c, "shared smtp server not found")
	}

	if err := h.Runtime.Store.SharedSMTP.Delete(c.Context(), srv.ID); err != nil {
		return response.Internal(c, err)
	}

	h.invalidateSESTopics()
	h.Runtime.Log.Info("shared smtp: server deleted", "id", srv.ID, "host", srv.Host)

	return response.NoContent(c)
}

// Test dials the server and records the verdict, exactly like the
// per-project test. A shared server that fails goes invalid and the
// delivery path stops considering it, which matters more here: it is
// the fallback for every project that owns nothing.
func (h *SharedHandler) Test(c fiber.Ctx) error {
	srv, err := h.Runtime.Store.SharedSMTP.Get(c.Context(), c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if srv == nil {
		return response.NotFound(c, "shared smtp server not found")
	}

	now := new(time.Now().UTC())
	testErr := testTransport(c.Context(), &srv.Server, h.Runtime.RelayNodeTLS,
		h.Runtime.Config.Sending.AllowPrivateSMTPTargets)
	if testErr != nil {
		if err := h.Runtime.Store.SharedSMTP.SetStatus(c.Context(),
			srv.ID, ssmodel.StatusInvalid, testErr.Error(), now); err != nil {
			return response.Internal(c, err)
		}

		return response.Success(c, TestResponse{Ok: false, Error: testErr.Error()})
	}

	status := srv.Status
	if status == ssmodel.StatusInvalid {
		status = ssmodel.StatusEnabled
	}

	if err := h.Runtime.Store.SharedSMTP.SetStatus(c.Context(),
		srv.ID, status, "", now); err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, SharedTestResponse{Ok: true, Status: status, ValidatedAt: now})
}

// normalizeShared fills the defaults the store and the delivery path
// both assume, so neither has to guess at a zero value.
func normalizeShared(srv *ssmodel.Shared) {
	if srv.Encryption == "" {
		srv.Encryption = smtpclient.EncryptionNone
	}

	if !ssmodel.ValidSecurityMode(srv.SecurityMode) {
		srv.SecurityMode = ssmodel.SecurityPermissive
	}

	if srv.AllowedEmails == nil {
		srv.AllowedEmails = []string{}
	}

	if srv.AllowedDomains == nil {
		srv.AllowedDomains = []string{}
	}
}

// invalidateSESTopics drops the cached topic allowlist. Same reason
// as the per-project handler's: see the note there.
func (h *SharedHandler) invalidateSESTopics() {
	if h.Runtime.SESTopics != nil {
		h.Runtime.SESTopics.Invalidate()
	}
}
