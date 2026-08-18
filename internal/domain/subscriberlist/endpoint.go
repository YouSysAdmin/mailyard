// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package subscriberlist

import (
	"database/sql"
	"errors"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yousysadmin/mailyard/internal/core/ids"

	"github.com/yousysadmin/mailyard/internal/core/env"
	"github.com/yousysadmin/mailyard/internal/core/paging"
	"github.com/yousysadmin/mailyard/internal/core/response"
	"github.com/yousysadmin/mailyard/internal/core/validation"
	"github.com/yousysadmin/mailyard/internal/domain"
	submodel "github.com/yousysadmin/mailyard/internal/models/subscriber"
	slmodel "github.com/yousysadmin/mailyard/internal/models/subscriberlist"
)

// Handler owns the /api/subscriber-lists surface plus the machine subscribe endpoints.
type Handler struct {
	Runtime *env.Runtime
}

func validateRules(rules []slmodel.FilterRule) string {
	for _, r := range rules {
		if _, ok := slmodel.ValidOperators[r.Operator]; !ok {
			return "unknown operator " + r.Operator
		}
	}

	return ""
}

// List serves GET /api/v1/subscriber-lists.
func (h *Handler) List(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	lists, err := h.Runtime.Store.SubscriberList.List(c.UserContext(), rc.Project.ID)
	if err != nil {
		return response.Internal(c, err)
	}

	if lists == nil {
		lists = []*slmodel.List{}
	}

	return response.Success(c, ListResponse{SubscriberLists: lists})
}

// Get serves GET /api/v1/subscriber-lists/:id.
func (h *Handler) Get(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	l, err := h.Runtime.Store.SubscriberList.Get(c.UserContext(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if l == nil {
		return response.NotFound(c, "subscriber list not found")
	}

	// A declared type, not a fiber.Map. ListDetailResponse already
	// existed and this handler built a map anyway, so the generated
	// console metadata described the body as nothing at all and the three
	// SDKs got an untyped object where every sibling route has a schema.
	// The keys are unchanged.
	out := ListDetailResponse{SubscriberList: l}
	if l.Type == slmodel.TypeStatic {
		n, err := h.Runtime.Store.SubscriberList.CountMembers(c.UserContext(), rc.Project.ID, l.ID)
		if err != nil {
			return response.Internal(c, err)
		}

		out.MemberCount = &n
	}

	return response.Success(c, out)
}

// Create serves POST /api/v1/subscriber-lists.
func (h *Handler) Create(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[upsertInput](c)
	if !ok {
		return resp
	}

	if msg := validateRules(in.FilterRules); msg != "" {
		return response.BadRequest(c, msg)
	}

	l := &slmodel.List{
		ID:          ids.New(),
		ProjectID:   rc.Project.ID,
		Name:        in.Name,
		Description: in.Description,
		Type:        in.Type,
		FilterRules: in.FilterRules,
	}
	if l.Type == "" {
		l.Type = slmodel.TypeStatic
	}

	if l.Type == slmodel.TypeDynamic && len(l.FilterRules) == 0 {
		return response.BadRequest(c, "dynamic lists require at least one filter rule")
	}

	if l.FilterRules == nil {
		l.FilterRules = []slmodel.FilterRule{}
	}

	if err := h.Runtime.Store.SubscriberList.Put(c.UserContext(), l); err != nil {
		return response.Internal(c, err)
	}

	return response.Created(c, ListDetailResponse{SubscriberList: l})
}

// Update serves PATCH /api/v1/subscriber-lists/:id.
func (h *Handler) Update(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	l, err := h.Runtime.Store.SubscriberList.Get(c.UserContext(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if l == nil {
		return response.NotFound(c, "subscriber list not found")
	}

	in, resp, ok := validation.Bind[upsertInput](c)
	if !ok {
		return resp
	}

	if msg := validateRules(in.FilterRules); msg != "" {
		return response.BadRequest(c, msg)
	}

	l.Name = in.Name
	l.Description = in.Description
	// Type is immutable: flipping static to dynamic would orphan the
	// membership silently.
	if in.FilterRules != nil {
		l.FilterRules = in.FilterRules
	}

	if l.Type == slmodel.TypeDynamic && len(l.FilterRules) == 0 {
		return response.BadRequest(c, "dynamic lists require at least one filter rule")
	}

	l.UpdatedAt = new(time.Now().UTC())
	if err := h.Runtime.Store.SubscriberList.Put(c.UserContext(), l); err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, ListDetailResponse{SubscriberList: l})
}

// Delete serves DELETE /api/v1/subscriber-lists/:id.
func (h *Handler) Delete(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	l, err := h.Runtime.Store.SubscriberList.Get(c.UserContext(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if l == nil {
		return response.NotFound(c, "subscriber list not found")
	}

	if err := h.Runtime.Store.SubscriberList.Delete(c.UserContext(), rc.Project.ID, l.ID); err != nil {
		return response.Internal(c, err)
	}

	return response.NoContent(c)
}

// ListMembers serves GET /api/v1/subscriber-lists/:id/members.
func (h *Handler) ListMembers(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	l, err := h.Runtime.Store.SubscriberList.Get(c.UserContext(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if l == nil {
		return response.NotFound(c, "subscriber list not found")
	}

	pg := paging.From(c)
	members, err := h.Runtime.Store.SubscriberList.ListMembers(c.UserContext(),
		rc.Project.ID, l.ID, pg.Limit, pg.Offset)
	if err != nil {
		return response.Internal(c, err)
	}

	if members == nil {
		members = []*submodel.Subscriber{}
	}

	return response.Success(c, MemberListResponse{Members: members})
}

// AddMember attaches a subscriber (by id or email) to a static list.
func (h *Handler) AddMember(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	l, err := h.Runtime.Store.SubscriberList.Get(c.UserContext(), rc.Project.ID, c.Params("id"))
	if err != nil {
		return response.Internal(c, err)
	}

	if l == nil {
		return response.NotFound(c, "subscriber list not found")
	}

	if l.Type != slmodel.TypeStatic {
		return response.BadRequest(c, "dynamic lists have no explicit members")
	}

	in, resp, ok := validation.Bind[memberInput](c)
	if !ok {
		return resp
	}

	sub, err := h.resolveSubscriber(c, rc.Project.ID, in.SubscriberID, in.Email)
	if err != nil {
		return response.Internal(c, err)
	}

	if sub == nil {
		return response.NotFound(c, "subscriber not found")
	}

	if err := h.Runtime.Store.SubscriberList.AddMember(c.UserContext(), rc.Project.ID, l.ID, sub.ID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return response.NotFound(c, "subscriber not found")
		}

		return response.Internal(c, err)
	}

	return response.Created(c, SubscriberResponse{Subscriber: sub})
}

// RemoveMember serves DELETE /api/v1/subscriber-
// lists/:id/members/:subscriberId.
func (h *Handler) RemoveMember(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	if err := h.Runtime.Store.SubscriberList.RemoveMember(c.UserContext(),
		rc.Project.ID, c.Params("id"), c.Params("subscriberId")); err != nil {
		return response.Internal(c, err)
	}

	return response.NoContent(c)
}

// PreviewSegment evaluates ad hoc filter rules and returns the match
// count plus a sample, so segment authors can iterate.
func (h *Handler) PreviewSegment(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[previewInput](c)
	if !ok {
		return resp
	}

	if msg := validateRules(in.FilterRules); msg != "" {
		return response.BadRequest(c, msg)
	}

	matched := 0
	sample := []*submodel.Subscriber{} // we need empty slice here
	for offset := 0; ; offset += resolvePageSize {
		page, err := h.Runtime.Store.Subscriber.ListPage(c.UserContext(), rc.Project.ID, resolvePageSize, offset)
		if err != nil {
			return response.Internal(c, err)
		}

		for _, sub := range page {
			if sub.Status != submodel.StatusSubscribed || !slmodel.MatchRules(sub, in.FilterRules) {
				continue
			}

			matched++
			if len(sample) < 10 {
				sample = append(sample, sub)
			}
		}

		if len(page) < resolvePageSize {
			break
		}
	}

	return response.Success(c, SegmentPreviewResponse{Matched: matched, Sample: sample})
}

// Subscribe is the machine endpoint: upsert the subscriber and attach
// it to a static list in one call (idempotent).
func (h *Handler) Subscribe(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[subscribeInput](c)
	if !ok {
		return resp
	}

	l, err := h.Runtime.Store.SubscriberList.Get(c.UserContext(), rc.Project.ID, in.ListID)
	if err != nil {
		return response.Internal(c, err)
	}

	if l == nil {
		return response.NotFound(c, "subscriber list not found")
	}

	if l.Type != slmodel.TypeStatic {
		return response.BadRequest(c, "dynamic lists have no explicit members")
	}

	sub, err := h.Runtime.Store.Subscriber.GetByEmail(c.UserContext(), rc.Project.ID, in.Email)
	if err != nil {
		return response.Internal(c, err)
	}

	if sub == nil {
		sub = &submodel.Subscriber{ProjectID: rc.Project.ID, Email: in.Email}
	}

	if in.Name != "" {
		sub.Name = in.Name
	}

	if in.CustomFields != nil {
		sub.CustomFields = in.CustomFields
	}

	if in.Timezone != "" {
		sub.Timezone = in.Timezone
	}

	if in.Language != "" {
		sub.Language = in.Language
	}

	// A subscribe call re-activates an unsubscribed address: the
	// caller asserts fresh consent.
	sub.Status = submodel.StatusSubscribed
	sub.UnsubscribedAt = nil
	if err := h.Runtime.Store.Subscriber.Put(c.UserContext(), sub); err != nil {
		return response.Internal(c, err)
	}

	if err := h.Runtime.Store.SubscriberList.AddMember(c.UserContext(), rc.Project.ID, l.ID, sub.ID); err != nil {
		return response.Internal(c, err)
	}

	// Fresh consent also clears a previous per-list opt-out.
	if err := h.Runtime.Store.SubscriberList.Resubscribe(c.UserContext(), rc.Project.ID, l.ID, sub.ID); err != nil {
		return response.Internal(c, err)
	}

	return response.Created(c, SubscribeResponse{Subscriber: sub, ListID: l.ID})
}

// UnsubscribeByEmail records a per-list opt-out.
func (h *Handler) UnsubscribeByEmail(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[listEmailInput](c)
	if !ok {
		return resp
	}

	sub, err := h.Runtime.Store.Subscriber.GetByEmail(c.UserContext(), rc.Project.ID, in.Email)
	if err != nil {
		return response.Internal(c, err)
	}

	if sub == nil {
		return response.NotFound(c, "subscriber not found")
	}

	if err := h.Runtime.Store.SubscriberList.Unsubscribe(c.UserContext(),
		rc.Project.ID, c.Params("id"), sub.ID, in.Reason); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return response.NotFound(c, "subscriber list not found")
		}

		return response.Internal(c, err)
	}

	return response.Success(c, MembershipChange{Unsubscribed: true, ListID: c.Params("id"), Email: sub.Email})
}

// ResubscribeByEmail clears a per-list opt-out.
func (h *Handler) ResubscribeByEmail(c *fiber.Ctx) error {
	rc := domain.GetRequestContext(c)
	in, resp, ok := validation.Bind[listEmailInput](c)
	if !ok {
		return resp
	}

	sub, err := h.Runtime.Store.Subscriber.GetByEmail(c.UserContext(), rc.Project.ID, in.Email)
	if err != nil {
		return response.Internal(c, err)
	}

	if sub == nil {
		return response.NotFound(c, "subscriber not found")
	}

	if err := h.Runtime.Store.SubscriberList.Resubscribe(c.UserContext(),
		rc.Project.ID, c.Params("id"), sub.ID); err != nil {
		return response.Internal(c, err)
	}

	return response.Success(c, MembershipChange{Resubscribed: true, ListID: c.Params("id"), Email: sub.Email})
}

func (h *Handler) resolveSubscriber(c *fiber.Ctx, projID, id, email string) (*submodel.Subscriber, error) {
	if id != "" {
		return h.Runtime.Store.Subscriber.Get(c.UserContext(), projID, id)
	}

	if email != "" {
		return h.Runtime.Store.Subscriber.GetByEmail(c.UserContext(), projID, email)
	}

	return nil, nil
}
