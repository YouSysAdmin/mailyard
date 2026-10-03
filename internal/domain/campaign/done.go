// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package campaign

import (
	"context"
	"fmt"

	"github.com/yousysadmin/mailyard/internal/core/notify"
	"github.com/yousysadmin/mailyard/internal/domain/store"
	cmodel "github.com/yousysadmin/mailyard/internal/models/campaign"
	nmodel "github.com/yousysadmin/mailyard/internal/models/notification"
	whmodel "github.com/yousysadmin/mailyard/internal/models/webhook"
)

// Emitter fans an event to the project's webhooks.
type Emitter func(ctx context.Context, projID, event, sender string, payload any)

// Finish emits campaign.completed and files campaign_done once the
// campaign has completed and every message has settled, so the totals
// are final.
//
// Called by the worker after each campaign message settles, and by the
// runner when it completes. The runner completes as soon as nothing is
// pending, while its last batch is usually still in the email queue.
// ClaimSettled decides which caller finishes, so the event goes out
// once.
func Finish(ctx context.Context, st *store.Store, emit Emitter, raiser *notify.Raiser, projID, campaignID string) error {
	if campaignID == "" {
		return nil
	}

	won, err := st.Campaign.ClaimSettled(ctx, projID, campaignID)
	if err != nil || !won {
		return err
	}

	c, err := st.Campaign.Get(ctx, projID, campaignID)
	if err != nil || c == nil {
		return err
	}

	totals, _, err := st.Campaign.MessageStats(ctx, campaignID)
	if err != nil {
		return err
	}

	if emit != nil {
		emit(ctx, projID, whmodel.EventCampaignCompleted, c.FromEmail, eventPayload(c, totals))
	}

	raiser.Raise(ctx, &nmodel.Notification{
		ProjectID: projID,
		Type:      nmodel.TypeCampaignDone,
		Severity:  nmodel.SeverityInfo,
		Title:     fmt.Sprintf("Campaign %q finished", c.Name),
		Body:      doneSummary(totals),
		Link:      "/campaigns/" + campaignID,
		DedupeKey: "campaign_done:" + campaignID,
	})

	return nil
}

// doneSummary words the final totals.
func doneSummary(totals map[string]int) string {
	return fmt.Sprintf("%d sent, %d failed, %d skipped.",
		totals[cmodel.MsgSent], totals[cmodel.MsgFailed], totals[cmodel.MsgSkipped])
}
