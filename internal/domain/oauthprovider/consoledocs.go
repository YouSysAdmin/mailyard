// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package oauthprovider

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
			Path:        "/oauth-providers/",
			Summary:     "List",
			Description: "Platform admin.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", ListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/oauth-providers/",
			Summary:     "Create",
			Description: "Platform admin.",
			Request:     upsertInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", ProviderResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/oauth-providers/:id",
			Summary:     "Delete",
			Description: "Platform admin.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", DeletedResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/oauth-providers/:id",
			Summary:     "Get",
			Description: "Platform admin.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", ProviderResponse{})},
		},
		{
			Method:      "PATCH",
			Path:        "/oauth-providers/:id",
			Summary:     "Update",
			Description: "Platform admin. A field the body does not name keeps its value, and the slug is never re-derived from the name.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     patchInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", ProviderResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/oauth-providers/:id/test",
			Summary:     "Test",
			Description: "Platform admin.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", TestResponse{})},
		},
	}
}
