// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package sandbox

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain"
	scmodel "github.com/yousysadmin/mailyard/internal/models/smtpcredential"
)

// Sandbox credentials are minted here rather than on
// /api/smtp-credentials, which is project admin.
//
// The role would be useless otherwise: a developer who has to ask an
// admin for a credential every time they set up a machine is a
// developer who goes back to a hardcoded SMTP mock. And it is safe in
// a way the admin surface is not, because Sandbox is forced true on
// every write below. A credential minted here can do exactly one
// thing - write into a sandbox this caller can already read - and
// there is no field, anywhere, that turns it into a live one.

// ListCredentials returns the project's SANDBOX credentials only.
//
// Filtered rather than paged off the same store call as the admin
// page, because a live credential is not this caller's business: they
// may not create one, may not revoke one, and showing them the name
// of the production integration tells them something about the
// project they were deliberately not given.
func (h *Handler) ListCredentials(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	creds, err := h.Runtime.Store.SMTPCredential.List(c.UserContext(), rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	out := []*scmodel.Credential{} // we need empty slice here
	for _, cr := range creds {
		if cr.Sandbox {
			out = append(out, cr)
		}
	}

	return response.Success(c, CredentialListResponse{
		SMTPCredentials: out,
		Submission:      h.submissionInfo(),
	})
}

// CreateCredential mints a sandbox credential and returns the
// password EXACTLY ONCE.
func (h *Handler) CreateCredential(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[credentialInput](c)
	if !ok {
		return resp
	}

	// Same mint-and-retry as the admin surface: the column is UNIQUE,
	// so a collision would be an insert failure rather than a silent
	// shadow, and 64 bits makes it vanishingly unlikely anyway.
	var username, password, hash string
	var err error
	for attempt := 0; ; attempt++ {
		username, password, hash, err = scmodel.Generate()
		if err != nil {
			return response.Internal(c, err)
		}

		existing, gerr := h.Runtime.Store.SMTPCredential.GetByUsername(c.UserContext(), username)
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

	cred := &scmodel.Credential{
		ID:           ids.New(),
		ProjectID:    rc.Project.ID,
		CreatedBy:    createdBy,
		Name:         in.Name,
		Username:     username,
		PasswordHash: hash,
		AllowedIPs:   []string{},

		// Not from input, and there is no input field for it. This
		// endpoint mints one kind of credential.
		Sandbox: true,
	}
	if err := h.Runtime.Store.SMTPCredential.Put(c.UserContext(), cred); err != nil {
		return response.Internal(c, err)
	}

	return response.Created(c, CredentialCreatedResponse{
		SMTPCredential: cred,
		Password:       password,
		Submission:     h.submissionInfo(),
	})
}

// RevokeCredential retires one sandbox credential.
//
// The sandbox check is re-read from the row rather than trusted from
// the list: a caller passing the id of the project's live production
// credential must not be able to switch off its mail from here.
func (h *Handler) RevokeCredential(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	cred, err := h.Runtime.Store.SMTPCredential.Get(c.UserContext(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	// Identical answer for a credential that does not exist and one
	// that exists but is live, so this is not a way to enumerate the
	// project's real credentials.
	if cred == nil || !cred.Sandbox {
		return response.NotFound(c, "sandbox credential not found")
	}

	if err := h.Runtime.Store.SMTPCredential.Revoke(c.UserContext(), rc.Project.ID, cred.ID); err != nil {
		return response.Internal(c, err)
	}

	cred.Revoked = true

	return response.Success(c, CredentialResponse{SMTPCredential: cred})
}

// submissionInfo mirrors the listener details the admin credential
// page reports, so a developer can render connection settings without
// reaching a route they are refused.
func (h *Handler) submissionInfo() SandboxListenerInfo {
	cfg := h.Runtime.Config.Submission
	host := cfg.Hostname
	if host == "" {
		host = "localhost"
	}

	port := cfg.Addr
	for i := len(port) - 1; i >= 0; i-- {
		if port[i] == ':' {
			port = port[i+1:]
			break
		}
	}

	return SandboxListenerInfo{
		Enabled:  cfg.Enabled,
		Host:     host,
		Port:     port,
		STARTTLS: cfg.TLS.Enabled,
	}
}
