// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package subscriber

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
			Path:        "/subscribers/",
			Summary:     "List",
			Description: "Needs the `subscribers:read` permission.",
			Query: []apidoc.Param{
				{
					Name:        "status",
					Description: "Only subscribers in this status.",
					Enum:        []string{"subscribed", "unsubscribed", "bounced", "complained"},
				},
				{Name: "q", Description: "Part of the address or the name, case-insensitive."},
				{Name: "email", Description: "One whole address, without regard to case. Answers that subscriber alone, and `q` is ignored."},
				{Name: "limit", Type: "integer", Description: "Page size. Over-asking is clamped, never refused."},
				{Name: "offset", Type: "integer", Description: "Rows to skip."},
			},
			Responses: []apidoc.Response{apidoc.OK("The result.", ListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/subscribers/",
			Summary:     "Create",
			Description: "Needs the `subscribers:write` permission.",
			Request:     upsertInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", SubscriberResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/subscribers/:id",
			Summary:     "Delete",
			Description: "Needs the `subscribers:delete` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "GET",
			Path:        "/subscribers/:id",
			Summary:     "Get",
			Description: "Needs the `subscribers:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", SubscriberResponse{})},
		},
		{
			Method:      "PATCH",
			Path:        "/subscribers/:id",
			Summary:     "Update",
			Description: "Needs the `subscribers:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     upsertInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", SubscriberResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/subscribers/:id/lists",
			Summary:     "Lists",
			Description: "The static lists the subscriber is on. Needs the `subscribers:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", MembershipResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/subscribers/import",
			Summary:     "Import",
			Description: "Needs the `subscribers:write` permission.",
			Request:     importInput{},
			Responses:   []apidoc.Response{apidoc.OK("What the import did.", ImportResponse{})},
		},
		{
			Method:  "POST",
			Path:    "/subscribers/import/csv",
			Summary: "Import CSV",
			Description: "Upserts subscribers from a CSV posted as the raw request body - " +
				"there is no multipart form and no column-mapping parameter. The header " +
				"row names the columns: `email` is required, `name`, `status`, `timezone` " +
				"and `language` are recognised, and every other column becomes a custom " +
				"field keyed by its header. Needs the `subscribers:write` permission.",
			Responses: []apidoc.Response{apidoc.OK("What the import did.", ImportResponse{})},
		},
	}
}
