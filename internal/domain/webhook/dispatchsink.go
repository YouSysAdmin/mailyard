// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package webhook

import (
	"context"
	"strconv"
	"time"

	coreaudit "github.com/yousysadmin/mailyard/internal/core/audit"
	"github.com/yousysadmin/mailyard/internal/core/notify"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	amodel "github.com/yousysadmin/mailyard/internal/models/audit"
	nmodel "github.com/yousysadmin/mailyard/internal/models/notification"
	whmodel "github.com/yousysadmin/mailyard/internal/models/webhook"
)

// DispatchSink is what the dispatcher writes through: the webhook
// store, plus the audit trail for the one thing the dispatcher decides
// on its own - taking a hook out of rotation. That decision has no
// request behind it, so the auditWrites middleware never sees it, and
// recording it here is what lets alertmail tell the project's owners.
type DispatchSink struct {
	Store store.WebhookStore
	Audit *coreaudit.Recorder

	// Notify returns the in-app notification raiser. A function because
	// the dispatcher is built before serve.go has one. Nil-safe.
	Notify func() *notify.Raiser
}

// List returns the project's hooks.
func (s *DispatchSink) List(ctx context.Context, projID string) ([]*whmodel.Webhook, error) {
	return s.Store.List(ctx, projID)
}

// RecordDelivery files one attempt.
func (s *DispatchSink) RecordDelivery(ctx context.Context, d *whmodel.Delivery) error {
	return s.Store.RecordDelivery(ctx, d)
}

// Disable takes the hook out of rotation and records why, as a project
// event the owners are mailed about.
func (s *DispatchSink) Disable(ctx context.Context, h *whmodel.Webhook, reason string) error {
	if err := s.Store.Disable(ctx, h.ProjectID, h.ID, reason); err != nil {
		return err
	}

	h.DisabledReason = reason
	s.Audit.Record(&amodel.Event{
		Category:  amodel.CategoryProject,
		Type:      amodel.TypeWebhookDisabled,
		ProjectID: h.ProjectID,
		Detail:    h.URL + " - " + reason,
	})

	// Console only: the audit event above is what mails the owners.
	if s.Notify != nil {
		s.Notify().RaiseInConsole(ctx, &nmodel.Notification{
			ProjectID: h.ProjectID,
			Type:      nmodel.TypeWebhookDisabled,
			Severity:  nmodel.SeverityWarning,
			Title:     "Webhook disabled after repeated failures",
			Body:      h.URL + " - " + reason,
			Link:      "/webhooks",
			DedupeKey: "webhook_disabled:" + h.ID + ":" + strconv.FormatInt(time.Now().Unix(), 10),
		})
	}

	return nil
}
