// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package stylesheet

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
			Path:        "/stylesheets/",
			Tag:         "stylesheet",
			Summary:     "List",
			Description: "Needs the `templates:read` permission.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", ListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/stylesheets/",
			Tag:         "stylesheet",
			Summary:     "Create",
			Description: "Needs the `templates:write` permission.",
			Request:     upsertInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", StylesheetResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/stylesheets/:id",
			Tag:         "stylesheet",
			Summary:     "Delete",
			Description: "Needs the `templates:delete` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "GET",
			Path:        "/stylesheets/:id",
			Tag:         "stylesheet",
			Summary:     "Get",
			Description: "Needs the `templates:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", StylesheetResponse{})},
		},
		{
			Method:      "PUT",
			Path:        "/stylesheets/:id",
			Tag:         "stylesheet",
			Summary:     "Update",
			Description: "Needs the `templates:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     upsertInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", StylesheetResponse{})},
		},
	}
}
