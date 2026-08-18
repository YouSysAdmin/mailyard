// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package notification

import (
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/paging"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/domain"
)

// Handler owns /api/notifications. Project scoped, any member -
// notifications describe the project, and every member is someone
// who might act on one.
type Handler struct {
	Runtime *env.Runtime
}

const (
	defaultLimit = 50
	maxLimit     = 200
)

// List serves GET /api/v1/notifications.
func (h *Handler) List(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	pg := paging.FromWith(c, defaultLimit, maxLimit)
	limit, offset := pg.Limit, pg.Offset
	unreadOnly := c.QueryBool("unread", false)

	items, err := h.Runtime.Store.Notification.List(c.UserContext(), rc.Project.ID, unreadOnly, limit, offset)
	if err != nil {
		return response.Internal(c, err)
	}

	unread, err := h.Runtime.Store.Notification.CountUnread(c.UserContext(), rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, ListResponse{
		Notifications: items,
		Unread:        unread,
		Limit:         limit,
		Offset:        offset,
	})
}

// Unread is the badge endpoint. Split from List because the console
// polls it far more often than it opens the list, and it is one
// indexed count rather than a page of rows.
func (h *Handler) Unread(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	n, err := h.Runtime.Store.Notification.CountUnread(c.UserContext(), rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, UnreadResponse{Unread: n})
}

// MarkRead serves POST /api/v1/notifications/:id/read.
func (h *Handler) MarkRead(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	n, err := h.Runtime.Store.Notification.Get(c.UserContext(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if n == nil {
		return response.NotFound(c, "notification not found")
	}

	if err := h.Runtime.Store.Notification.MarkRead(c.UserContext(), rc.Project.ID, n.ID, time.Now().UTC()); err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, ReadResponse{Read: true})
}

// MarkAllRead serves POST /api/v1/notifications/read-all.
func (h *Handler) MarkAllRead(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	n, err := h.Runtime.Store.Notification.MarkAllRead(c.UserContext(), rc.Project.ID, time.Now().UTC())
	if err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, MarkedResponse{Marked: n})
}

// Delete serves DELETE /api/v1/notifications/:id.
func (h *Handler) Delete(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	n, err := h.Runtime.Store.Notification.Get(c.UserContext(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if n == nil {
		return response.NotFound(c, "notification not found")
	}

	if err := h.Runtime.Store.Notification.Delete(c.UserContext(), rc.Project.ID, n.ID); err != nil {
		return response.Internal(c, err)
	}

	return response.NoContent(c)
}
