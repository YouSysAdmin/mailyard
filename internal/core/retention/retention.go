// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package retention purges aged data according to the platform
// settings. Without it an install accumulates email rows, received
// mail, attachment blobs, webhook deliveries, and tracking events
// forever.
//
// Every window is opt-in: a setting of 0 means "keep it", so an
// operator who never visits the settings page loses nothing. The
// content windows (bodies, attachments) are clamped to the metadata
// window - keeping a body longer than the row that owns it is
// meaningless.
package retention

import (
	"context"
	"log/slog"
	"time"

	"github.com/yousysadmin/mailyard/internal/core/blob"
	"github.com/yousysadmin/mailyard/internal/core/partition"
	"github.com/yousysadmin/mailyard/internal/core/settings"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	smodel "github.com/yousysadmin/mailyard/internal/models/setting"
)

// Sweeper runs one retention pass.
type Sweeper struct {
	Store    *store.Store
	Settings *settings.Service
	Blob     blob.Store
	Log      *slog.Logger

	// Partitions removes spent daily partitions of the emails table
	// before the row-by-row delete runs. Optional: nil falls back to
	// DELETE for everything, which is what this did before the table
	// was partitioned and is still correct, just slower.
	Partitions *partition.Maintainer
}

// Result reports what one pass removed, for the log line and tests.
type Result struct {
	EmailsPurged        int64
	PartitionsDropped   int
	EmailBodiesCleared  int64
	EmailAttsCleared    int64
	InboundPurged       int64
	SandboxPurged       int64
	InboundCleared      int64
	DeliveriesPurged    int64
	TrackingPurged      int64
	AuditPurged         int64
	ResetsPurged        int64
	VerificationsPurged int64
	SessionsPurged      int64
	NotificationsPurged int64
	BlobsDeleted        int
	BlobErrors          int
}

// Run executes a full sweep. It keeps going after a per-section
// error: one failing table must not stop the rest from being
// trimmed. The first error is returned so the job is marked failed
// and the operator investigates.
func (s *Sweeper) Run(ctx context.Context) error {
	now := time.Now().UTC()
	var res Result
	var firstErr error

	note := func(section string, err error) {
		if err == nil {
			return
		}

		s.Log.Error("retention: section failed", "section", section, "err", err)
		if firstErr == nil {
			firstErr = err
		}
	}

	metaDays := s.Settings.Int(smodel.KeyRetentionDays)

	// Content windows never outlive the row they hang off.
	clamp := func(days int) int {
		if days <= 0 {
			return metaDays
		}

		if metaDays > 0 && days > metaDays {
			return metaDays
		}

		return days
	}

	bodyDays := clamp(s.Settings.Int(smodel.KeyEmailBodyRetentionDays))
	attDays := clamp(s.Settings.Int(smodel.KeyEmailAttachmentRetentionDays))
	inboundDays := s.Settings.Int(smodel.KeyInboundRetentionDays)
	if inboundDays <= 0 {
		inboundDays = metaDays
	}

	deliveryDays := s.Settings.Int(smodel.KeyWebhookDeliveryRetentionDays)
	trackingDays := s.Settings.Int(smodel.KeyTrackingEventRetentionDays)

	// Attachments first: drop the blobs, then the rows referencing
	// them. Doing it the other way round would orphan objects with no
	// record of their keys.
	if attDays > 0 {
		cutoff := now.AddDate(0, 0, -attDays)
		keys, err := s.Store.Email.StorageKeysOlderThan(ctx, cutoff)
		note("email attachment keys", err)
		if err == nil {
			d, e := s.deleteBlobs(ctx, keys)
			res.BlobsDeleted += d
			res.BlobErrors += e
			n, err := s.Store.Email.ClearAttachmentsOlderThan(ctx, cutoff)
			note("email attachments", err)
			res.EmailAttsCleared = n
		}
	}

	if bodyDays > 0 {
		n, err := s.Store.Email.ClearBodiesOlderThan(ctx, now.AddDate(0, 0, -bodyDays))
		note("email bodies", err)
		res.EmailBodiesCleared = n
	}

	if metaDays > 0 {
		cutoff := now.AddDate(0, 0, -metaDays)
		// Rows about to be deleted may still hold blobs if the
		// attachment window was longer or disabled.
		keys, err := s.Store.Email.StorageKeysOlderThan(ctx, cutoff)
		note("email purge keys", err)
		// The removal is inside this guard, and that is the whole
		// point of reading the keys first. If the key query failed we
		// do not know which blobs these rows own, so deleting the rows
		// anyway would strand those objects with nothing left that
		// names them - permanently, since the next pass has no row to
		// find them from. Skipping the pass leaves rows a week longer
		// and the error is already recorded, which is the recoverable
		// side of the trade.
		if err == nil {
			d, e := s.deleteBlobs(ctx, keys)
			res.BlobsDeleted += d
			res.BlobErrors += e

			// Drop whole partitions first, then DELETE what is left.
			//
			// The order matters and so does the fallback. A partition
			// whose entire day is past the cutoff goes in one DROP
			// TABLE - no dead tuples, nothing for autovacuum to
			// reclaim, which is the whole reason the table is
			// partitioned. What DROP cannot take is the current
			// partition, which straddles the cutoff, and any older one
			// still holding a scheduled message - so the DELETE below
			// runs regardless and handles exactly those.
			if s.Partitions != nil {
				dropped, derr := s.Partitions.DropSpent(ctx, cutoff)
				note("email partitions", derr)
				res.PartitionsDropped = len(dropped)
			}

			n, perr := s.Store.Email.PurgeOlderThan(ctx, cutoff)
			note("email purge", perr)
			res.EmailsPurged = n
		}
	}

	if inboundDays > 0 {
		cutoff := now.AddDate(0, 0, -inboundDays)
		keys, err := s.Store.Inbound.StorageKeysOlderThan(ctx, cutoff)
		note("inbound keys", err)
		// Inside the guard, for the reason spelled out on the email
		// purge above: rows removed without their keys leave blobs
		// nothing can ever name again.
		if err == nil {
			d, e := s.deleteBlobs(ctx, keys)
			res.BlobsDeleted += d
			res.BlobErrors += e

			n, perr := s.Store.Inbound.PurgeOlderThan(ctx, cutoff)
			note("inbound purge", perr)
			res.InboundPurged = n
		}
	} else if attDays > 0 {
		// No inbound window, but attachments still expire: strip the
		// content and keep the envelope.
		cutoff := now.AddDate(0, 0, -attDays)
		keys, err := s.Store.Inbound.StorageKeysOlderThan(ctx, cutoff)
		note("inbound content keys", err)
		if err == nil {
			d, e := s.deleteBlobs(ctx, keys)
			res.BlobsDeleted += d
			res.BlobErrors += e
			n, err := s.Store.Inbound.ClearContentOlderThan(ctx, cutoff)
			note("inbound content", err)
			res.InboundCleared = n
		}
	}

	if deliveryDays > 0 {
		n, err := s.Store.Webhook.PurgeDeliveriesOlderThan(ctx, now.AddDate(0, 0, -deliveryDays))
		note("webhook deliveries", err)
		res.DeliveriesPurged = n
	}

	if trackingDays > 0 {
		n, err := s.Store.Campaign.PurgeTrackingEventsOlderThan(ctx, now.AddDate(0, 0, -trackingDays))
		note("tracking events", err)
		res.TrackingPurged = n
	}

	if auditDays := s.Settings.Int(smodel.KeyAuditLogRetentionDays); auditDays > 0 {
		n, err := s.Store.Audit.PurgeOlderThan(ctx, now.AddDate(0, 0, -auditDays))
		note("audit events", err)
		res.AuditPurged = n
	}

	// The volume counter, which nothing can read past two days: the
	// longest window a plan limit asks about is 24 hours. Not governed by
	// a retention setting - this is not somebody's data, it is arithmetic
	// with an expiry, and an operator turning retention off must not be
	// left with a row per project per minute forever.
	if n, err := s.Store.Email.PruneVolumeBefore(ctx, now.AddDate(0, 0, -2)); err != nil {
		note("email volume counters", err)
	} else if n > 0 {
		s.Log.Info("retention: pruned email volume counters", "rows", n)
	}

	// Read notifications only. An unread alert is still trying to
	// tell somebody something, so age alone is not a reason to drop
	// it - the store enforces that, this just supplies the window.
	if noteDays := s.Settings.Int(smodel.KeyNotificationRetentionDays); noteDays > 0 {
		n, err := s.Store.Notification.PurgeOlderThan(ctx, now.AddDate(0, 0, -noteDays))
		note("notifications", err)
		res.NotificationsPurged = n
	}

	// Expired sessions can no longer authenticate, so there is
	// nothing to weigh against dropping them. Revoked rows survive
	// until their natural expiry so a user can still see a recent
	// "signed out everywhere" in their list.
	sn, err := s.Store.Session.PurgeExpired(ctx, now)
	note("sessions", err)
	res.SessionsPurged = sn

	// Spent and expired reset tokens are never useful and are not
	// governed by an operator setting - they go on every pass.
	// The sandbox carries its expiry on the ROW, decided when each
	// message was captured, so there is no window to read here and
	// nothing an operator can change that retroactively deletes mail
	// somebody is looking at. This sweep only enforces what was
	// already written down. The per-project cap does the rest, on
	// every capture, because a day window does nothing about ten
	// thousand messages arriving in one morning.
	{
		n, err := s.Store.Sandbox.PurgeExpired(ctx, now)
		note("sandbox", err)
		res.SandboxPurged = n
	}

	n, err := s.Store.PasswordReset.DeleteExpired(ctx, now)
	note("password resets", err)
	res.ResetsPurged = n

	nv, err := s.Store.SignupVerify.DeleteExpired(ctx, now)
	note("signup verifications", err)
	res.VerificationsPurged = nv

	s.Log.Info("retention: sweep complete",
		"emails_purged", res.EmailsPurged,
		"partitions_dropped", res.PartitionsDropped,
		"bodies_cleared", res.EmailBodiesCleared,
		"attachments_cleared", res.EmailAttsCleared,
		"inbound_purged", res.InboundPurged,
		"inbound_cleared", res.InboundCleared,
		"sandbox_purged", res.SandboxPurged,
		"deliveries_purged", res.DeliveriesPurged,
		"tracking_purged", res.TrackingPurged,
		"audit_purged", res.AuditPurged,
		"resets_purged", res.ResetsPurged,
		"verifications_purged", res.VerificationsPurged,
		"sessions_purged", res.SessionsPurged,
		"blobs_deleted", res.BlobsDeleted,
		"blob_errors", res.BlobErrors)

	return firstErr
}

// deleteBlobs removes objects, tolerating individual failures - a
// blob that will not delete is an orphan, not a reason to keep the
// database row that no longer describes it.
func (s *Sweeper) deleteBlobs(ctx context.Context, keys []string) (deleted, failed int) {
	if s.Blob == nil || len(keys) == 0 {
		return 0, 0
	}

	for _, k := range keys {
		if err := s.Blob.Delete(ctx, k); err != nil {
			s.Log.Warn("retention: blob delete failed", "key", k, "err", err)
			failed++
			continue
		}

		deleted++
	}

	return deleted, failed
}
