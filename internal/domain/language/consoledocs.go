// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package language

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
			Path:        "/languages/",
			Tag:         "language",
			Summary:     "List",
			Description: "Needs the `templates:read` permission.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", ListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/languages/",
			Tag:         "language",
			Summary:     "Create",
			Description: "Needs the `templates:write` permission.",
			Request:     upsertInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", LanguageResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/languages/:id",
			Tag:         "language",
			Summary:     "Delete",
			Description: "Needs the `templates:delete` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "PUT",
			Path:        "/languages/:id",
			Tag:         "language",
			Summary:     "Update",
			Description: "Needs the `templates:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     upsertInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", LanguageResponse{})},
		},
	}
}
