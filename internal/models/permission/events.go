// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package permission

import (
	"github.com/yousysadmin/mailyard/internal/core/eventbus"
	whmodel "github.com/yousysadmin/mailyard/internal/models/webhook"
)

// eventResource names the resource an event describes, for both the
// live feed and outgoing webhooks. Receiving an event takes read on it.
var eventResource = map[string]Resource{
	eventbus.TypeEmailQueued:      ResourceEmails,
	eventbus.TypeEmailSent:        ResourceEmails,
	eventbus.TypeEmailFailed:      ResourceEmails,
	eventbus.TypeInboundReceived:  ResourceInbound,
	eventbus.TypeCampaignProgress: ResourceCampaigns,
	eventbus.TypeNotification:     ResourceNotifications,

	whmodel.EventEmailSuppressed:   ResourceEmails,
	whmodel.EventEmailCancelled:    ResourceEmails,
	whmodel.EventCampaignStarted:   ResourceCampaigns,
	whmodel.EventCampaignCompleted: ResourceCampaigns,
	whmodel.EventInboundReceived:   ResourceInbound,
}

// EventResource returns the resource an event type describes, and
// false for a type that describes none.
func EventResource(eventType string) (Resource, bool) {
	r, ok := eventResource[eventType]

	return r, ok
}
