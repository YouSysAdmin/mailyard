// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package apikey

import (
	"errors"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain"
	akmodel "github.com/yousysadmin/mailyard/internal/models/apikey"
)

// AdminHandler owns /api/v1/admin/api-keys. Mounted behind
// requireAdmin like the rest of that surface.
type AdminHandler struct {
	Runtime *env.Runtime
}

// List serves GET /api/v1/admin/api-keys.
func (h *AdminHandler) List(c *fiber.Ctx) error {
	keys, err := h.Runtime.Store.AdminAPIKey.List(c.UserContext())
	if err != nil {
		return response.Internal(c, err)
	}

	if keys == nil {
		keys = []*akmodel.Admin{}
	}

	return response.Success(c, AdminListResponse{AdminAPIKeys: keys})
}

// Create mints a platform credential and returns the plaintext token EXACTLY ONCE.
//
// No permission list to validate: a key here is admin or it does not
// exist. That is why minting one is deliberately a platform-admin act
// and why the console asks for confirmation - the credential it hands
// back can create users and rewrite the installation's settings.
func (h *AdminHandler) Create(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[adminCreateInput](c)
	if !ok {
		return resp
	}

	var expiresAt *time.Time
	if in.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, in.ExpiresAt)
		if err != nil {
			return response.BadRequest(c, "expires_at must be an RFC 3339 timestamp")
		}

		if t.Before(time.Now()) {
			return response.BadRequest(c, "expires_at is in the past")
		}

		utc := t.UTC()
		expiresAt = &utc
	}

	// Same retry as the tenant path: GetByPrefix returns one row, so
	// two keys sharing the 12-character prefix would shadow each other
	// at auth time. Negligible odds, removed entirely at mint time.
	var plaintext, prefix, hash string
	var err error
	for attempt := 0; ; attempt++ {
		plaintext, prefix, hash, err = akmodel.GenerateAdmin()
		if err != nil {
			return response.Internal(c, err)
		}

		existing, gerr := h.Runtime.Store.AdminAPIKey.GetByPrefix(c.UserContext(), prefix)
		if gerr != nil {
			return response.Internal(c, gerr)
		}

		if existing == nil {
			break
		}

		if attempt >= 4 {
			return response.Internal(c, errors.New("could not mint a unique key prefix"))
		}
	}

	createdBy := ""
	if rc.User != nil {
		createdBy = rc.User.ID
	}

	k := &akmodel.Admin{
		ID:         ids.New(),
		CreatedBy:  createdBy,
		Name:       in.Name,
		KeyHash:    hash,
		KeyPrefix:  prefix,
		AllowedIPs: in.AllowedIPs,
		ExpiresAt:  expiresAt,
	}
	if k.AllowedIPs == nil {
		k.AllowedIPs = []string{}
	}

	if err := h.Runtime.Store.AdminAPIKey.Put(c.UserContext(), k); err != nil {
		return response.Internal(c, err)
	}

	return response.Created(c, AdminCreatedResponse{AdminAPIKey: k, Token: plaintext})
}

// Revoke serves POST /api/v1/admin/api-keys/:id/revoke.
func (h *AdminHandler) Revoke(c *fiber.Ctx) error {
	k, err := h.Runtime.Store.AdminAPIKey.Get(c.UserContext(), c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if k == nil {
		return response.NotFound(c, "api key not found")
	}

	if err := h.Runtime.Store.AdminAPIKey.Revoke(c.UserContext(), k.ID); err != nil {
		return response.Internal(c, err)
	}

	k.Revoked = true

	return response.Success(c, AdminAPIKeyResponse{AdminAPIKey: k})
}

// Delete serves DELETE /api/v1/admin/api-keys/:id.
func (h *AdminHandler) Delete(c *fiber.Ctx) error {
	k, err := h.Runtime.Store.AdminAPIKey.Get(c.UserContext(), c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if k == nil {
		return response.NotFound(c, "api key not found")
	}

	if err := h.Runtime.Store.AdminAPIKey.Delete(c.UserContext(), k.ID); err != nil {
		return response.Internal(c, err)
	}

	return response.NoContent(c)
}
