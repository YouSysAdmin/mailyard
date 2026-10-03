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

	before := srv.Server
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

	if in.Password != nil && *in.Password != "" {
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

	normalizeShared(srv)
	if err := h.Runtime.Store.SharedSMTP.Put(c.Context(), srv); err != nil {
		return response.Internal(c, err)
	}

	// Status moves only when the admin names one, the same override
	// Enable is on a project server. Otherwise a dial change under an
	// invalid verdict replaces the stale reason and a test decides.
	switch {
	case in.Status != "":
		if err := h.Runtime.Store.SharedSMTP.SetStatus(c.Context(),
			srv.ID, in.Status, "", srv.ValidatedAt); err != nil {
			return response.Internal(c, err)
		}
	case ssmodel.DialChanged(&before, &srv.Server):
		if err := h.Runtime.Store.SharedSMTP.NoteSettingsChanged(c.Context(), srv.ID); err != nil {
			return response.Internal(c, err)
		}
	}

	h.invalidateSESTopics()

	fresh, err := h.Runtime.Store.SharedSMTP.Get(c.Context(), srv.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if fresh == nil {
		return response.NotFound(c, "shared smtp server not found")
	}

	return response.Success(c, SharedResponse{SharedSMTPServer: fresh})
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

	if srv.IsNode() {
		return response.BadRequest(c, errServerIsANode)
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
// the fallback for every project that owns nothing. A relay node's row
// is the exception - see testNode.
func (h *SharedHandler) Test(c fiber.Ctx) error {
	srv, err := h.Runtime.Store.SharedSMTP.Get(c.Context(), c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if srv == nil {
		return response.NotFound(c, "shared smtp server not found")
	}

	if srv.IsNode() {
		return testNode(c, h.Runtime, &srv.Server)
	}

	now := new(time.Now().UTC())
	testErr := testTransport(c.Context(), &srv.Server, h.Runtime.RelayNodeTLS,
		h.Runtime.Config.Sending.AllowPrivateSMTPTargets)
	reason := ""
	if testErr != nil {
		reason = testErr.Error()
	}

	// Fenced in the store, as on a project server.
	status, err := h.Runtime.Store.SharedSMTP.RecordTest(c.Context(), srv.ID, reason)
	if err != nil {
		return response.Internal(c, err)
	}

	if testErr != nil {
		return response.Success(c, TestResponse{Ok: false, Error: reason})
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
