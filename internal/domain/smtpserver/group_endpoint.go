// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpserver

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain"
	ssmodel "github.com/yousysadmin/mailyard/internal/models/smtpserver"
)

// GroupHandler owns /api/smtp-server-groups. Mounted behind
// requireAuth + requireProject, writes gated to project admin in
// routes.go - a group decides where a project's mail physically
// leaves from, which is infrastructure, not content.
type GroupHandler struct {
	Runtime *env.Runtime
}

// List returns the project's groups with their servers, default
// first. EnsureDefault rather than a plain read: this is where a
// project created since migration 00003 gets its default group, and
// answering with an empty list would show a console that offers no
// group to put a server in.
func (h *GroupHandler) List(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	if _, err := h.Runtime.Store.SMTPGroup.EnsureDefault(c.UserContext(), rc.Project.ID); err != nil {
		return response.Internal(c, err)
	}

	groups, err := h.Runtime.Store.SMTPGroup.List(c.UserContext(), rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	for _, g := range groups {
		servers, err := h.Runtime.Store.SMTPServer.ListInGroup(c.UserContext(), rc.Project.ID, g.ID)
		if err != nil {
			return response.Internal(c, err)
		}

		g.Servers = servers
	}

	if groups == nil {
		groups = []*ssmodel.Group{}
	}

	return response.Success(c, GroupListResponse{Groups: groups})
}

// Get serves GET /api/v1/smtp-server-groups/:id.
func (h *GroupHandler) Get(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	g, err := h.Runtime.Store.SMTPGroup.Get(c.UserContext(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if g == nil {
		return response.NotFound(c, "smtp server group not found")
	}

	g.Servers, err = h.Runtime.Store.SMTPServer.ListInGroup(c.UserContext(), rc.Project.ID, g.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, GroupResponse{Group: g})
}

// Create serves POST /api/v1/smtp-server-groups.
func (h *GroupHandler) Create(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[groupCreateInput](c)
	if !ok {
		return resp
	}

	slug := in.Slug
	if slug == "" {
		slug = slugify(in.Name)
	}
	if slug == "" {
		return response.BadRequest(c, "name must contain at least one letter or digit")
	}

	taken, err := h.Runtime.Store.SMTPGroup.SlugTaken(c.UserContext(), rc.Project.ID, slug, "")
	if err != nil {
		return response.Internal(c, err)
	}

	if taken {
		return response.BadRequest(c, "a group with slug "+slug+" already exists")
	}

	// Make sure the project has a default before adding a second
	// group, so send that names none always resolves. The new group
	// is never the default - promoting is an explicit PATCH.
	if _, err := h.Runtime.Store.SMTPGroup.EnsureDefault(c.UserContext(), rc.Project.ID); err != nil {
		return response.Internal(c, err)
	}

	g := &ssmodel.Group{
		ID:          ids.New(),
		ProjectID:   rc.Project.ID,
		Name:        in.Name,
		Slug:        slug,
		Description: in.Description,
	}
	if err := h.Runtime.Store.SMTPGroup.Put(c.UserContext(), g); err != nil {
		return response.Internal(c, err)
	}

	return response.Created(c, GroupResponse{Group: g})
}

// Update serves PATCH /api/v1/smtp-server-groups/:id.
func (h *GroupHandler) Update(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	g, err := h.Runtime.Store.SMTPGroup.Get(c.UserContext(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if g == nil {
		return response.NotFound(c, "smtp server group not found")
	}

	in, resp, ok := validation.Bind[groupUpdateInput](c)
	if !ok {
		return resp
	}

	if in.Name != "" {
		g.Name = in.Name
	}

	if in.Description != nil {
		g.Description = *in.Description
	}

	if in.Slug != "" && in.Slug != g.Slug {
		taken, err := h.Runtime.Store.SMTPGroup.SlugTaken(c.UserContext(), rc.Project.ID, in.Slug, g.ID)
		if err != nil {
			return response.Internal(c, err)
		}

		if taken {
			return response.BadRequest(c, "a group with slug "+in.Slug+" already exists")
		}

		g.Slug = in.Slug
	}

	if err := h.Runtime.Store.SMTPGroup.Put(c.UserContext(), g); err != nil {
		return response.Internal(c, err)
	}

	// Promotion last, and in one statement of its own. Demoting the old
	// default and promoting the new one as two writes from here leaves
	// the project with no default in between - permanently, if anything
	// interrupts, and the console cannot repair it. SetDefault does both
	// inside a transaction.
	//
	// After Put, so a failed promotion still leaves the rename applied
	// rather than pretending nothing happened, and Put no longer carries
	// is_default at all.
	if in.MakeDefault && !g.Default {
		promoted, err := h.Runtime.Store.SMTPGroup.SetDefault(c.UserContext(), rc.Project.ID, g.ID)
		if err != nil {
			return response.Internal(c, err)
		}

		if !promoted {
			return response.NotFound(c, "smtp server group not found")
		}

		g.Default = true
	}

	return response.Success(c, GroupResponse{Group: g})
}

// Delete removes a group and moves its servers to the default one.
//
// The servers survive on purpose: a group is a routing label, and
// deleting a label must not delete the credentials behind it.
func (h *GroupHandler) Delete(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	g, err := h.Runtime.Store.SMTPGroup.Get(c.UserContext(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if g == nil {
		return response.NotFound(c, "smtp server group not found")
	}

	if g.Default {
		return response.BadRequest(c,
			"the default group cannot be deleted - make another group the default first")
	}

	def, err := h.Runtime.Store.SMTPGroup.EnsureDefault(c.UserContext(), rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if def == nil {
		return response.Internal(c, errNoDefaultGroup)
	}

	if err := h.Runtime.Store.SMTPGroup.Delete(c.UserContext(), rc.Project.ID, g.ID, def.ID); err != nil {
		return response.Internal(c, err)
	}

	return response.NoContent(c)
}

// errNoDefaultGroup should be unreachable: EnsureDefault creates one.
var errNoDefaultGroup = &groupError{"project has no default smtp server group"}

type groupError struct{ msg string }

// Error renders the failure for a log or a caller.
func (e *groupError) Error() string { return e.msg }

// slugify turns a name into a URL and header safe handle. Same shape
// as the project one - a small duplicate beats a shared package that
// exists to hold one function.
func slugify(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}

	return strings.Trim(b.String(), "-")
}
