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
				"nowhere else - only its hash is stored. Deliveries are signed with it, " +
				"so a receiver can tell our POST from anyone else's.",
			Request: createInput{},
			Responses: []apidoc.Response{
				apidoc.Created("The webhook, with its secret.", CreateResponse{}),
				apidoc.BadRequest,
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
			Method:      "GET",
			Path:        "/webhooks/:id/deliveries",
			Tag:         "webhooks",
			Permission:  "webhooks:read",
			Summary:     "Delivery log of one webhook",
			Description: "Cursor paged. The log belongs to one webhook, so its id is part of the path.",
			PathParams:  []apidoc.Param{{Name: "id", Format: "uuid"}},
			Query: []apidoc.Param{
				{Name: "limit", Type: "integer"},
				{Name: "cursor"},
			},
			Responses: []apidoc.Response{
				apidoc.OK("One page of attempts.", DeliveriesResponse{}),
				apidoc.NotFound,
			},
		},
	}
}
