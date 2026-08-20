// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpserver

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/quota"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/smtpclient"
	"github.com/yousysadmin/mailyard/internal/core/transport"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

// Handler owns the /api/smtp-servers surface. Mounted behind
// requireAuth + requireProject, with editor / admin tiers applied
// per route in server/routes.go. rc.Project is always set here.
type Handler struct {
	Runtime *env.Runtime
}

// List returns the project's servers. Passwords never serialize.
func (h *Handler) List(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	servers, err := h.Runtime.Store.SMTPServer.List(c.Context(), rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if servers == nil {
		servers = []*ssmodel.Server{}
	}

	return response.Success(c, ListResponse{SMTPServers: servers, Providers: transport.Providers()})
}

// Get serves GET /api/v1/smtp-servers/:id.
func (h *Handler) Get(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	srv, err := h.Runtime.Store.SMTPServer.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if srv == nil {
		return response.NotFound(c, "smtp server not found")
	}

	return response.Success(c, ServerResponse{SMTPServer: srv})
}

// Create serves POST /api/v1/smtp-servers.
func (h *Handler) Create(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[createInput](c)
	if !ok {
		return resp
	}

	if err := quota.CheckResource(c.Context(), h.Runtime.Store, rc.Project.ID, quota.ResSMTPServers, 1); err != nil {
		if qe, ok := errors.AsType[*quota.Error](err); ok {
			return response.TooManyRequests(c, qe.Error())
		}

		return response.Internal(c, err)
	}

	srv := &ssmodel.Server{
		ID:             ids.New(),
		ProjectID:      rc.Project.ID,
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
	}
	// Refused here, not at the first send. A row missing an option its
	// provider requires would be stored as enabled and fail every
	// message, with the reason only in the delivery log.
	if err := transport.ValidateOptions(in.Provider, in.ProviderConfig); err != nil {
		return response.BadRequest(c, err.Error())
	}

	groupID, resp, ok := h.resolveGroup(c, rc.Project.ID, in.GroupID)
	if !ok {
		return resp
	}

	srv.GroupID = groupID
	if srv.Encryption == "" {
		srv.Encryption = smtpclient.EncryptionNone
	}

	if srv.AllowedEmails == nil {
		srv.AllowedEmails = []string{}
	}

	if srv.AllowedDomains == nil {
		srv.AllowedDomains = []string{}
	}

	if err := h.Runtime.Store.SMTPServer.Put(c.Context(), srv); err != nil {
		return response.Internal(c, err)
	}

	h.invalidateSESTopics()

	return response.Created(c, ServerResponse{SMTPServer: srv})
}

// Update serves PATCH /api/v1/smtp-servers/:id.
func (h *Handler) Update(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	srv, err := h.Runtime.Store.SMTPServer.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if srv == nil {
		return response.NotFound(c, "smtp server not found")
	}

	in, resp, ok := validation.Bind[updateInput](c)
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
		// Validated against the row's own provider, which a PATCH cannot
		// change - so clearing a required option is refused rather than
		// leaving a row that looks fine and fails every message.
		if err := transport.ValidateOptions(srv.Provider, srv.ProviderConfig); err != nil {
			return response.BadRequest(c, err.Error())
		}
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

	if in.Priority != nil {
		srv.Priority = *in.Priority
	}

	if in.GroupID != "" {
		groupID, resp, ok := h.resolveGroup(c, rc.Project.ID, in.GroupID)
		if !ok {
			return resp
		}

		srv.GroupID = groupID
	}

	// Dial settings changed under an "invalid" verdict: force a fresh
	// test rather than carrying a stale error forward.
	if srv.Status == ssmodel.StatusInvalid {
		srv.Status = ssmodel.StatusEnabled
		srv.ValidationError = ""
		srv.ValidatedAt = nil
	}

	if err := h.Runtime.Store.SMTPServer.Put(c.Context(), srv); err != nil {
		return response.Internal(c, err)
	}

	h.invalidateSESTopics()

	return response.Success(c, ServerResponse{SMTPServer: srv})
}

// Delete serves DELETE /api/v1/smtp-servers/:id.
func (h *Handler) Delete(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	srv, err := h.Runtime.Store.SMTPServer.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if srv == nil {
		return response.NotFound(c, "smtp server not found")
	}

	if err := h.Runtime.Store.SMTPServer.Delete(c.Context(), rc.Project.ID, srv.ID); err != nil {
		return response.Internal(c, err)
	}

	h.invalidateSESTopics()

	return response.NoContent(c)
}

// Test dials the server (auth included when credentials are set) and
// records the verdict on the row: success re-enables an invalid
// server, failure marks it invalid so the delivery worker skips it.
func (h *Handler) Test(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	srv, err := h.Runtime.Store.SMTPServer.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if srv == nil {
		return response.NotFound(c, "smtp server not found")
	}

	now := new(time.Now().UTC())
	// Through the provider, so "test" means whatever proving reachability
	// means for it: a dial and an AUTH for SMTP, an account read for an
	// API. Neither sends anything.
	testErr := testTransport(c.Context(), srv, h.Runtime.RelayNodeTLS)
	if testErr != nil {
		if err := h.Runtime.Store.SMTPServer.SetStatus(c.Context(),
			rc.Project.ID, srv.ID, ssmodel.StatusInvalid, testErr.Error(), now); err != nil {
			return response.Internal(c, err)
		}

		return response.Success(c, TestResponse{Ok: false, Error: testErr.Error()})
	}

	status := srv.Status
	if status == ssmodel.StatusInvalid {
		status = ssmodel.StatusEnabled
	}

	if err := h.Runtime.Store.SMTPServer.SetStatus(c.Context(),
		rc.Project.ID, srv.ID, status, "", now); err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, TestResponse{Ok: true})
}

// Enable flips the server to enabled (also clearing an invalid
// verdict - the operator overrides the last test).
func (h *Handler) Enable(c fiber.Ctx) error {
	return h.setStatus(c, ssmodel.StatusEnabled)
}

// Disable takes the server out of the delivery rotation.
func (h *Handler) Disable(c fiber.Ctx) error {
	return h.setStatus(c, ssmodel.StatusDisabled)
}

func (h *Handler) setStatus(c fiber.Ctx, status string) error {
	rc := domain.GetRequestContext(c)
	srv, err := h.Runtime.Store.SMTPServer.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if srv == nil {
		return response.NotFound(c, "smtp server not found")
	}

	if err := h.Runtime.Store.SMTPServer.SetStatus(c.Context(),
		rc.Project.ID, srv.ID, status, "", srv.ValidatedAt); err != nil {
		return response.Internal(c, err)
	}

	srv.Status = status
	srv.ValidationError = ""

	return response.Success(c, ServerResponse{SMTPServer: srv})
}

// callerID returns the authenticated user's id, or "" when auth is
// disabled.
// resolveGroup validates a requested group id and falls back to the
// project's default. A server must always land in exactly one group -
// one that belongs to no group is invisible to every resolution path,
// so it would silently stop being used with nothing to show why.
func (h *Handler) resolveGroup(c fiber.Ctx, projID, groupID string) (string, error, bool) {
	if groupID != "" {
		g, err := h.Runtime.Store.SMTPGroup.Get(c.Context(), projID, groupID)
		if err != nil {
			return "", response.Internal(c, err), false
		}

		if g == nil {
			return "", response.BadRequest(c, "smtp server group not found"), false
		}

		return g.ID, nil, true
	}

	def, err := h.Runtime.Store.SMTPGroup.EnsureDefault(c.Context(), projID)
	if err != nil {
		return "", response.Internal(c, err), false
	}

	return def.ID, nil, true
}

func callerID(rc *domain.RequestContext) string {
	if rc == nil || rc.User == nil {
		return ""
	}

	return rc.User.ID
}

// invalidateSESTopics drops the cached topic allowlist.
//
// A server write is the only thing that can change which SNS topics
// are ours, and the SES receiver answers from a cache to keep a public
// endpoint off the database. Without this an operator pasting an ARN
// would watch notifications be refused for up to a minute with no
// explanation.
func (h *Handler) invalidateSESTopics() {
	if h.Runtime.SESTopics != nil {
		h.Runtime.SESTopics.Invalidate()
	}
}

// testTransport proves a row is usable without sending anything.
//
// One helper for both surfaces - a project's own servers and the shared
// pool - because "is this row reachable" is one question and the two
// handlers answering it differently is how one of them ends up still
// dialling a provider that has no host.
//
// A RELAY NODE is dialled the way the delivery worker dials it, which
// is the whole point of nodeTLS: mutual TLS against our own authority,
// no AUTH. Passing nil instead asks the SYSTEM roots about a
// certificate our own CA signed, which cannot pass - so the button
// answered "certificate signed by unknown authority" for a node that
// delivers perfectly well, and then WROTE that verdict onto the row,
// which is what takes a node out of the rotation. The delivery path
// resolves the same thing in email.Processor.nodeTLS.
func testTransport(ctx context.Context, srv *ssmodel.Server,
	nodeTLS func(context.Context, string) (*tls.Config, error)) error {
	var dialTLS *tls.Config
	if srv.IsNode() {
		if nodeTLS == nil {
			// Same answer as the delivery path: dialling without the
			// certificate would fail at the handshake anyway, and much
			// less clearly.
			return fmt.Errorf("server %s is a relay node but no client identity is configured", srv.Name)
		}

		built, err := nodeTLS(ctx, srv.Host)
		if err != nil {
			return err
		}

		dialTLS = built
	}

	t, err := transport.Open(srv.Spec(dialTLS))
	if err != nil {
		// A row naming a provider that will not open is a configuration
		// error, and reporting it as the test result is exactly right:
		// the console shows the reason on the row.
		return err
	}

	return t.Test(ctx)
}
