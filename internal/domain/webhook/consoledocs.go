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
			Method:      "DELETE",
			Path:        "/webhooks/:id",
			Tag:         "webhook",
			Summary:     "Delete",
			Description: "Needs the `webhooks:delete` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "GET",
			Path:        "/webhooks/:id/deliveries",
			Tag:         "webhook",
			Summary:     "Deliveries",
			Description: "Needs the `webhooks:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", DeliveriesResponse{})},
		},
	}
}
