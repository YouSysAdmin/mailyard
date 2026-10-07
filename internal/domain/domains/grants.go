// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package domains

import (
	"github.com/gofiber/fiber/v3"

	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain"
	dmodel "github.com/yousysadmin/mailyard/internal/models/domain"
)

// Sharing a verified domain with another project.
//
// The owner keeps the domain: its DNS records, its DKIM key, its
// inbound mail. A grant lets the other project SEND as the domain and
// its subdomains, through its own servers, signed with the owner's key
// - store.DomainStore.GetVerifiedCoveringFor is where that is decided.
//
// A SHARE IS AN OFFER UNTIL THE OTHER PROJECT ACCEPTS IT. The owner
// names a slug, and nothing stops it naming any slug on the
// installation, so before this a project could be made to send as a
// stranger's domain with no say in it, and the stranger learned the
// project's name from the grant list. A pending grant covers no
// sending and shows the owner the slug it typed. The grantee sees the
// offer under shared on its domain list, and answers it through
// /domains/:id/shares, which is addressed by the OWNER's domain id and
// scoped to the caller's project: the owner's grant routes start from
// the owner-scoped Get, these two start from the caller's grant.

// Grants serves GET /api/v1/domains/:id/grants.
func (h *Handler) Grants(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	d, resp, ok := h.owned(c, rc)
	if !ok {
		return resp
	}

	return h.grantsResponse(c, rc, d)
}

// Share serves POST /api/v1/domains/:id/grants.
func (h *Handler) Share(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[grantInput](c)
	if !ok {
		return resp
	}

	d, resp, ok := h.owned(c, rc)
	if !ok {
		return resp
	}

	// An unverified domain covers nothing, so a grant on it would be a
	// promise the sending checks never keep.
	if !d.Verified {
		return response.BadRequest(c, "verify the domain before sharing it")
	}

	p, err := h.Runtime.Store.Project.GetBySlug(c.Context(), in.ProjectSlug)
	if err != nil {
		return response.Internal(c, err)
	}

	if p == nil {
		return response.NotFound(c, "project not found")
	}

	if p.ID == rc.Project.ID {
		return response.BadRequest(c, "a project cannot share a domain with itself")
	}

	grantedBy := ""
	if rc.User != nil {
		grantedBy = rc.User.Email
	}

	if err := h.Runtime.Store.Domain.Grant(c.Context(), &dmodel.Grant{
		DomainID: d.ID, ProjectID: p.ID, GrantedBy: grantedBy,
	}); err != nil {
		return response.Internal(c, err)
	}

	return h.grantsResponse(c, rc, d)
}

// AcceptShare serves POST /api/v1/domains/:id/shares/accept, the grantee
// accepting a domain offered to it. Accepting again changes nothing.
func (h *Handler) AcceptShare(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	found, err := h.Runtime.Store.Domain.AcceptGrant(c.Context(), c.Params("id"), rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if !found {
		return response.NotFound(c, "share not found")
	}

	sh, err := h.Runtime.Store.Domain.GetShared(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if sh == nil {
		return response.NotFound(c, "share not found")
	}

	return response.Success(c, ShareResponse{Shared: sh})
}

// LeaveShare serves DELETE /api/v1/domains/:id/shares: the grantee
// declining an offer, or giving up a domain it had accepted. The same
// row goes either way.
func (h *Handler) LeaveShare(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	gone, err := h.Runtime.Store.Domain.Revoke(c.Context(), c.Params("id"), rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if !gone {
		return response.NotFound(c, "share not found")
	}

	return response.NoContent(c)
}

// Unshare serves DELETE /api/v1/domains/:id/grants/:project_id.
func (h *Handler) Unshare(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	d, resp, ok := h.owned(c, rc)
	if !ok {
		return resp
	}

	gone, err := h.Runtime.Store.Domain.Revoke(c.Context(), d.ID, c.Params("project_id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if !gone {
		return response.NotFound(c, "grant not found")
	}

	return response.NoContent(c)
}

// owned loads the domain in the path from the caller's project. A
// domain another project owns, or one only shared with the caller, is
// not found.
func (h *Handler) owned(c fiber.Ctx, rc *domain.RequestContext) (*dmodel.Domain, error, bool) {
	d, err := h.Runtime.Store.Domain.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return nil, response.Internal(c, err), false
	}

	if d == nil {
		return nil, response.NotFound(c, "domain not found"), false
	}

	return d, nil, true
}

func (h *Handler) grantsResponse(c fiber.Ctx, rc *domain.RequestContext, d *dmodel.Domain) error {
	grants, err := h.Runtime.Store.Domain.ListGrants(c.Context(), rc.Project.ID, d.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, GrantsResponse{Grants: grants})
}
