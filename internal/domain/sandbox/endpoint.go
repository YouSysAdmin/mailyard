// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package sandbox

import (
	"github.com/gofiber/fiber/v3"
	"strconv"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/mailparse"
	"github.com/yousysadmin/mailyard/internal/core/paging"
	"github.com/yousysadmin/mailyard/internal/core/quota"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/domain"
	perm "github.com/yousysadmin/mailyard/internal/models/permission"
	smodel "github.com/yousysadmin/mailyard/internal/models/setting"
)

// Handler owns /api/sandbox. It is the one tenant surface mounted
// behind requireProjectMember rather than requireProject, so a member
// ranked below viewer - the developer role - can reach it and nothing
// else. See TestOnlySandboxSkipsTheViewerFloor.
type Handler struct {
	Runtime *env.Runtime
}

// List returns a page of captured messages, newest first, alongside
// the settings a developer needs to make sense of what they see: how
// long a message survives and how many the project keeps.
//
// Bundled rather than left to a second endpoint because "where did my
// message go" is the first question this page has to answer, and the
// answer is usually one of those two numbers.
func (h *Handler) List(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	p := paging.From(c)
	msgs, err := h.Runtime.Store.Sandbox.List(c.Context(), rc.Project.ID, p.Limit, p.Offset)
	if err != nil {
		return response.Internal(c, err)
	}

	total, err := h.Runtime.Store.Sandbox.Count(c.Context(), rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, ListResponse{
		SandboxEmails: msgs,
		Total:         total,
		Settings: RetentionInfo{
			RetentionDays: h.retentionFor(c, rc.Project.ID),
			MaxMessages:   h.maxMessagesFor(c, rc.Project.ID),
		},
	})
}

// Get serves GET /api/v1/sandbox/:id.
func (h *Handler) Get(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	e, err := h.Runtime.Store.Sandbox.Get(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if e == nil {
		return response.NotFound(c, "message not found")
	}

	return response.Success(c, EmailResponse{SandboxEmail: e})
}

// Raw streams the wire bytes as text/plain.
//
// Half of what a sandbox is for. A parsed view cannot be turned back
// into the message that produced it, and the questions a developer
// brings here - is my custom header actually set, why is this
// multipart wrong - are only answerable from the bytes.
func (h *Handler) Raw(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	raw, err := h.Runtime.Store.Sandbox.Raw(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if raw == nil {
		return response.NotFound(c, "message not found")
	}

	// text/plain, never message/rfc822: a browser offers to hand the
	// second to a mail client, and the point here is to read it.
	c.Set(fiber.HeaderContentType, "text/plain; charset=utf-8")

	return c.Send(raw)
}

// Attachment streams one attachment by index.
//
// Reparsed out of the raw message rather than read from a column,
// because the bytes are stored once. That costs a parse per download
// and buys a table with nothing to keep in sync and no blob store to
// leave orphans in when a message is trimmed away.
func (h *Handler) Attachment(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	raw, err := h.Runtime.Store.Sandbox.Raw(c.Context(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if raw == nil {
		return response.NotFound(c, "message not found")
	}

	parsed, perr := mailparse.Parse(raw)
	if perr != nil {
		return response.BadRequest(c, "this message could not be parsed, download the raw source instead")
	}

	idx, err := strconv.Atoi(c.Params("idx"))
	if err != nil || idx < 0 || idx >= len(parsed.Attachments) {
		return response.NotFound(c, "attachment not found")
	}

	a := parsed.Attachments[idx]

	// response.Attachment, not a local copy of it. This endpoint had its
	// own: the same content type, the same disposition, and its own
	// filename sanitizer that only stripped the four characters a quoted
	// header cares about - where the shared one is an allowlist with a
	// length cap. No exploit was found in the difference, and that is not
	// the point: one rule with two implementations is one that has
	// already drifted, and the shared function's own comment claims to be
	// the only place this is decided.
	//
	// What it decides matters most here, of all three callers: a sandbox
	// attachment was composed by whatever application is under test, so
	// rendering it inline on this origin is the one thing that must not
	// happen.
	return response.Attachment(c, a.Filename, a.ContentType, a.Content)
}

// Delete serves DELETE /api/v1/sandbox/:id.
func (h *Handler) Delete(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	if err := h.Runtime.Store.Sandbox.Delete(c.Context(), rc.Project.ID, c.Params("id")); err != nil {
		return response.Internal(c, err)
	}

	return response.NoContent(c)
}

// Clear empties the project's sandbox.
//
// Available to a developer, unlike every other destructive route in
// the product. Nothing here was ever delivered and nothing depends on
// it, so the person testing against it is the right person to decide
// when the noise stops being useful.
func (h *Handler) Clear(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	n, err := h.Runtime.Store.Sandbox.Clear(c.Context(), rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, DeletedResponse{Deleted: n})
}

// Info reports how to send into this sandbox, and who the caller is.
//
// The role matters to the console: a developer has to be shown a
// single page rather than a navigation full of links that will all
// answer 403.
func (h *Handler) Info(c fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	cfg := h.Runtime.Config.Submission
	host := cfg.Hostname
	if host == "" {
		host = "localhost"
	}

	return response.Success(c, SettingsResponse{
		Submission: SubmissionInfo{
			Enabled:  cfg.Enabled,
			Host:     host,
			Addr:     cfg.Addr,
			STARTTLS: cfg.TLS.Enabled,
		},
		RetentionDays: h.retentionFor(c, rc.Project.ID),
		MaxMessages:   h.maxMessagesFor(c, rc.Project.ID),
		SandboxOnly:   sandboxOnly(rc),
	})
}

// sandboxOnly reports that this caller reaches the sandbox and no
// other resource in the project, so the page is their whole console.
//
// Asked of the permission SET rather than of a role name. A project
// writes its own roles, so there is no name to compare against - what
// matters is the shape of what they hold, which is exactly what the
// screen is deciding from.
func sandboxOnly(rc *domain.RequestContext) bool {
	if rc == nil || rc.ProjectOwner || !rc.Permissions.Touches(perm.ResourceSandbox) {
		return false
	}

	for _, d := range perm.Registry {
		if d.Resource != perm.ResourceSandbox && rc.Permissions.Touches(d.Resource) {
			return false
		}
	}

	return true
}

// retentionFor and maxMessagesFor answer what governs this project's
// sandbox, which is what the page should say.
//
// Not the platform settings. The plan sells the cap and the project
// chooses its window under the plan's ceiling, so reporting the
// installation's values would give a developer a number their own
// captures do not obey.
func (h *Handler) retentionFor(c fiber.Ctx, projID string) int {
	days := h.Runtime.Settings.Int(smodel.KeySandboxRetentionDays)
	if w, err := h.Runtime.Store.Project.Get(c.Context(), projID); err == nil && w != nil &&
		w.SandboxRetentionDays > 0 {
		days = w.SandboxRetentionDays
	}

	if _, ceiling, err := quota.Sandbox(c.Context(), h.Runtime.Store, projID); err == nil &&
		ceiling > 0 && (days <= 0 || days > ceiling) {
		days = ceiling
	}

	return days
}

func (h *Handler) maxMessagesFor(c fiber.Ctx, projID string) int {
	if msgs, _, err := quota.Sandbox(c.Context(), h.Runtime.Store, projID); err == nil && msgs > 0 {
		return msgs
	}

	return h.Runtime.Settings.Int(smodel.KeySandboxMaxMessages)
}
