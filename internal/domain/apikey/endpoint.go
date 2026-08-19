// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package apikey

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/quota"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain"
	akmodel "github.com/yousysadmin/mailyard/internal/models/apikey"
	perm "github.com/yousysadmin/mailyard/internal/models/permission"
)

// Handler owns the console /api/api-keys surface. Mounted behind
// requireAuth + requireProject with per-route role gates.
type Handler struct {
	Runtime *env.Runtime
}

// List serves GET /api/v1/api-keys.
func (h *Handler) List(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	keys, err := h.Runtime.Store.APIKey.List(c.Context(), rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if keys == nil {
		keys = []*akmodel.Key{}
	}

	return response.Success(c, ListResponse{APIKeys: keys})
}

// Create mints a key and returns the plaintext token EXACTLY ONCE.
func (h *Handler) Create(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[createInput](c)
	if !ok {
		return resp
	}

	if err := quota.CheckResource(c.Context(), h.Runtime.Store, rc.Project.ID, quota.ResAPIKeys, 1); err != nil {
		if qe, ok := errors.AsType[*quota.Error](err); ok {
			return response.TooManyRequests(c, qe.Error())
		}

		return response.Internal(c, err)
	}

	perms, bad := normalizePermissions(in.Permissions)
	if bad != "" {
		return response.BadRequest(c, bad)
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

	// GetByPrefix returns a single row, so two keys sharing the
	// 12-char prefix would shadow each other at auth time. The odds
	// are negligible (~48 bits) but a retry at mint time removes the edge entirely.
	var plaintext, prefix, hash string
	var err error
	for attempt := 0; ; attempt++ {
		plaintext, prefix, hash, err = akmodel.Generate()
		if err != nil {
			return response.Internal(c, err)
		}

		existing, gerr := h.Runtime.Store.APIKey.GetByPrefix(c.Context(), prefix)
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

	k := &akmodel.Key{
		ID:          ids.New(),
		ProjectID:   rc.Project.ID,
		CreatedBy:   createdBy,
		Name:        in.Name,
		KeyHash:     hash,
		KeyPrefix:   prefix,
		Permissions: perms,
		Sandbox:     in.Sandbox,
		AllowedIPs:  in.AllowedIPs,
		ExpiresAt:   expiresAt,
	}
	if k.AllowedIPs == nil {
		k.AllowedIPs = []string{}
	}

	if err := h.Runtime.Store.APIKey.Put(c.Context(), k); err != nil {
		return response.Internal(c, err)
	}

	return response.Created(c, CreatedResponse{APIKey: k, Token: plaintext})
}

// Revoke serves POST /api/v1/api-keys/:id/revoke.
func (h *Handler) Revoke(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	k, err := h.Runtime.Store.APIKey.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if k == nil {
		return response.NotFound(c, "api key not found")
	}

	if err := h.Runtime.Store.APIKey.Revoke(c.Context(), rc.Project.ID, k.ID); err != nil {
		return response.Internal(c, err)
	}

	k.Revoked = true

	return response.Success(c, APIKeyResponse{APIKey: k})
}

// Delete serves DELETE /api/v1/api-keys/:id.
func (h *Handler) Delete(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	k, err := h.Runtime.Store.APIKey.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if k == nil {
		return response.NotFound(c, "api key not found")
	}

	if err := h.Runtime.Store.APIKey.Delete(c.Context(), rc.Project.ID, k.ID); err != nil {
		return response.Internal(c, err)
	}

	return response.NoContent(c)
}

// normalizePermissions is the strict write-side check, the same
// asymmetry a custom group has: reading skips what it cannot parse
// because stored data outlives code, writing refuses because a typo
// accepted here would be skipped on every read forever and the
// operator would see a key that saves fine and is mysteriously
// refused.
//
// It differs from the group validator in one place: the wildcard is
// ALLOWED. See permission.ForKey for why a key may hold what a group may not.
//
// An empty result is legal and means a key that may do nothing. That
// is not a mistake worth guessing about - the old code defaulted an
// empty list to send, which was a sensible default only while sending
// was the whole surface.
func normalizePermissions(in []string) ([]string, string) {
	if len(in) > 2*len(perm.Registry) {
		return nil, "too many permissions - the catalogue is smaller than this list"
	}

	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		p := strings.ToLower(strings.TrimSpace(raw))
		if p == "" {
			continue
		}

		if p != perm.All {
			if _, _, ok := perm.Parse(p); !ok {
				return nil, "unknown permission " + raw
			}
		}

		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}

	return out, ""
}
