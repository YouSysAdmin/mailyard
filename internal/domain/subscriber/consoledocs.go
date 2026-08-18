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
			Tag:         "subscriber",
			Summary:     "List",
			Description: "Needs the `subscribers:read` permission.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", ListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/subscribers/",
			Tag:         "subscriber",
			Summary:     "Create",
			Description: "Needs the `subscribers:write` permission.",
			Request:     upsertInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", SubscriberResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/subscribers/:id",
			Tag:         "subscriber",
			Summary:     "Delete",
			Description: "Needs the `subscribers:delete` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "GET",
			Path:        "/subscribers/:id",
			Tag:         "subscriber",
			Summary:     "Get",
			Description: "Needs the `subscribers:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", SubscriberResponse{})},
		},
		{
			Method:      "PATCH",
			Path:        "/subscribers/:id",
			Tag:         "subscriber",
			Summary:     "Update",
			Description: "Needs the `subscribers:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     upsertInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", SubscriberResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/subscribers/import",
			Tag:         "subscriber",
			Summary:     "Import",
			Description: "Needs the `subscribers:write` permission.",
			Request:     importInput{},
			Responses:   []apidoc.Response{apidoc.OK("What the import did.", ImportResponse{})},
		},
		{
			Method:  "POST",
			Path:    "/subscribers/import/csv",
			Tag:     "subscriber",
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
