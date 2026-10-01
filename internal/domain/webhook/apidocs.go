// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package webhook

import "github.com/yousysadmin/mailyard/internal/core/apidoc"

// APIDocs describes the webhook slice of the machine API.
func APIDocs() []apidoc.Route {
	return []apidoc.Route{
		{
			Method:     "GET",
			Path:       "/webhooks",
			Tag:        "webhooks",
			Permission: "webhooks:read",
			Summary:    "List outgoing webhooks",
			Responses:  []apidoc.Response{apidoc.OK("Every webhook in the project.", ListResponse{})},
		},
		{
			Method:     "POST",
			Path:       "/webhooks",
			Tag:        "webhooks",
			Permission: "webhooks:write",
			Summary:    "Create an outgoing webhook",
			Description: "The response carries the signing secret, which appears there and " +
				"on a rotation, nowhere else. Deliveries are signed with it, so a receiver " +
				"can tell our POST from anyone else's.",
			Request: createInput{},
			Responses: []apidoc.Response{
				apidoc.Created("The webhook, with its secret.", CreateResponse{}),
				apidoc.BadRequest,
			},
		},
		{
			Method:     "GET",
			Path:       "/webhooks/:id",
			Tag:        "webhooks",
			Permission: "webhooks:read",
			Summary:    "One webhook",
			PathParams: []apidoc.Param{{Name: "id", Format: "uuid"}},
			Responses:  []apidoc.Response{apidoc.OK("The webhook, without its secret.", WebhookResponse{}), apidoc.NotFound},
		},
		{
			Method:     "PATCH",
			Path:       "/webhooks/:id",
			Tag:        "webhooks",
			Permission: "webhooks:write",
			Summary:    "Change a webhook",
			Description: "Each of `url`, `events` and `filters` is changed only when sent. " +
				"`filters: []` clears the list. The signing secret is untouched, so a " +
				"receiver keeps verifying through the edit - rotate it separately if " +
				"the endpoint changed hands.",
			PathParams: []apidoc.Param{{Name: "id", Format: "uuid"}},
			Request:    updateInput{},
			Responses: []apidoc.Response{
				apidoc.OK("The webhook.", WebhookResponse{}),
				apidoc.BadRequest,
				apidoc.NotFound,
			},
		},
		{
			Method:     "POST",
			Path:       "/webhooks/:id/disable",
			Tag:        "webhooks",
			Permission: "webhooks:write",
			Summary:    "Take a webhook out of rotation",
			Description: "Nothing is delivered to it until it is enabled again. The optional " +
				"`reason` is recorded on the hook where the dispatcher's own reason would " +
				"be. Idempotent on a webhook that is already disabled, which keeps its " +
				"first reason.",
			PathParams: []apidoc.Param{{Name: "id", Format: "uuid"}},
			Request:    disableInput{},
			Responses: []apidoc.Response{
				apidoc.OK("The webhook.", WebhookResponse{}),
				apidoc.NotFound,
			},
		},
		{
			Method:     "POST",
			Path:       "/webhooks/:id/test",
			Tag:        "webhooks",
			Permission: "webhooks:write",
			Summary:    "Post a test event now",
			Description: "One signed delivery of a `webhook.test` event, made during this " +
				"request, with the outcome in the answer and in the delivery log. No " +
				"retry and no disabling, whatever the receiver answers. Works on a " +
				"disabled webhook, which is how its owner learns it is fixed.",
			PathParams: []apidoc.Param{{Name: "id", Format: "uuid"}},
			Responses: []apidoc.Response{
				apidoc.OK("The attempt.", DeliveryResponse{}),
				apidoc.NotFound,
			},
		},
		{
			Method:     "POST",
			Path:       "/webhooks/:id/rotate-secret",
			Tag:        "webhooks",
			Permission: "webhooks:write",
			Summary:    "Rotate a webhook's signing secret",
			Description: "Mints a fresh secret and returns it once, the way creating the webhook did. " +
				"Deliveries are signed with the new secret from the next one on, so a receiver " +
				"verifies against both old and new for the length of the changeover.",
			PathParams: []apidoc.Param{{Name: "id", Format: "uuid"}},
			Responses: []apidoc.Response{
				apidoc.OK("The webhook, with its new secret.", CreateResponse{}),
				apidoc.NotFound,
			},
		},
		{
			Method:     "DELETE",
			Path:       "/webhooks/:id",
			Tag:        "webhooks",
			Permission: "webhooks:delete",
			Summary:    "Delete a webhook",
			PathParams: []apidoc.Param{{Name: "id", Format: "uuid"}},
			Responses:  []apidoc.Response{apidoc.NoContent, apidoc.NotFound},
		},
		{
			Method:     "POST",
			Path:       "/webhooks/:id/enable",
			Tag:        "webhooks",
			Permission: "webhooks:write",
			Summary:    "Re-enable a disabled webhook",
			Description: "A webhook whose deliveries fail on every attempt is disabled and the " +
				"project's owners are mailed the reason. Once the endpoint is fixed, this " +
				"puts it back into rotation. Idempotent on a webhook that is already enabled.",
			PathParams: []apidoc.Param{{Name: "id", Format: "uuid"}},
			Responses: []apidoc.Response{
				apidoc.OK("The webhook.", EnableResponse{}),
				apidoc.NotFound,
			},
		},
		{
			Method:     "GET",
			Path:       "/webhooks/:id/deliveries",
			Tag:        "webhooks",
			Permission: "webhooks:read",
			Summary:    "Delivery log of one webhook",
			Description: "Cursor paged. The log belongs to one webhook, so its id is part of the path. " +
				"Each row carries the body it posted, so a failed one can be read and sent again.",
			PathParams: []apidoc.Param{{Name: "id", Format: "uuid"}},
			Query: []apidoc.Param{
				{Name: "status", Enum: []string{"success", "failed"}},
				{Name: "event", Description: "One event name exactly."},
				{Name: "limit", Type: "integer"},
				{Name: "cursor"},
			},
			Responses: []apidoc.Response{
				apidoc.OK("One page of attempts.", DeliveriesResponse{}),
				apidoc.NotFound,
			},
		},
		{
			Method:     "POST",
			Path:       "/webhooks/:id/deliveries/:deliveryId/redeliver",
			Tag:        "webhooks",
			Permission: "webhooks:write",
			Summary:    "Send one delivery again",
			Description: "Posts the body that attempt sent, again, now, as a new attempt in the " +
				"log. One try, during this request. A delivery recorded before bodies were " +
				"kept answers 409, because the record is what the receiver was sent and " +
				"nothing else would be.",
			PathParams: []apidoc.Param{{Name: "id", Format: "uuid"}, {Name: "deliveryId", Format: "uuid"}},
			Responses: []apidoc.Response{
				apidoc.OK("The new attempt.", DeliveryResponse{}),
				apidoc.NotFound,
				apidoc.Conflict,
			},
		},
	}
}
