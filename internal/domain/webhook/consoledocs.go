// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package webhook

import "github.com/yousysadmin/mailyard/internal/core/apidoc"

// ConsoleDocs describes this domain's slice of the console API.
//
// Generated, then kept honest by TestEveryConsoleRouteIsDocumented:
// the routes come from routes.go and the shapes from the response
// types each handler actually constructs, which is readable only
// because every handler returns a declared type rather than a map.
// Edit the summaries and descriptions freely - they are the half a
// generator cannot know.
func ConsoleDocs() []apidoc.Route {
	return []apidoc.Route{
		{
			Method:      "GET",
			Path:        "/webhooks/",
			Tag:         "webhook",
			Summary:     "List",
			Description: "Needs the `webhooks:read` permission.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", ListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/webhooks/",
			Tag:         "webhook",
			Summary:     "Create",
			Description: "Needs the `webhooks:write` permission.",
			Request:     createInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", CreateResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/webhooks/:id",
			Tag:         "webhook",
			Summary:     "Get",
			Description: "Needs the `webhooks:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", WebhookResponse{})},
		},
		{
			Method:      "PATCH",
			Path:        "/webhooks/:id",
			Tag:         "webhook",
			Summary:     "Update",
			Description: "Needs the `webhooks:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     updateInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", WebhookResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/webhooks/:id",
			Tag:         "webhook",
			Summary:     "Delete",
			Description: "Needs the `webhooks:delete` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "POST",
			Path:        "/webhooks/:id/disable",
			Tag:         "webhook",
			Summary:     "Disable",
			Description: "Needs the `webhooks:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     disableInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", WebhookResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/webhooks/:id/test",
			Tag:         "webhook",
			Summary:     "Test",
			Description: "Needs the `webhooks:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", DeliveryResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/webhooks/:id/deliveries",
			Tag:         "webhook",
			Summary:     "Deliveries",
			Description: "Needs the `webhooks:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Query: []apidoc.Param{
				{Name: "status", Enum: []string{"success", "failed"}},
				{Name: "event", Description: "One event name exactly."},
				{Name: "limit", Type: "integer"},
				{Name: "cursor"},
			},
			Responses: []apidoc.Response{apidoc.OK("The result.", DeliveriesResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/webhooks/:id/deliveries/:deliveryId/redeliver",
			Tag:         "webhook",
			Summary:     "Redeliver",
			Description: "Needs the `webhooks:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}, {Name: "deliveryId"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", DeliveryResponse{})},
		},
	}
}
