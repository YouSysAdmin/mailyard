// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package notification

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
			Path:        "/notifications/",
			Tag:         "notification",
			Summary:     "List",
			Description: "Needs the `notifications:read` permission.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", ListResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/notifications/:id",
			Tag:         "notification",
			Summary:     "Delete",
			Description: "Needs the `notifications:delete` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "POST",
			Path:        "/notifications/:id/read",
			Tag:         "notification",
			Summary:     "Mark read",
			Description: "Needs the `notifications:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", ReadResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/notifications/read-all",
			Tag:         "notification",
			Summary:     "Mark all read",
			Description: "Needs the `notifications:write` permission.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", MarkedResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/notifications/unread",
			Tag:         "notification",
			Summary:     "Unread",
			Description: "Needs the `notifications:read` permission.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", UnreadResponse{})},
		},
	}
}
