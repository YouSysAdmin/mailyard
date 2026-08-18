// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package domains

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
			Path:        "/domains/",
			Tag:         "domains",
			Summary:     "List",
			Description: "Needs the `domains:read` permission.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", ListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/domains/",
			Tag:         "domains",
			Summary:     "Create",
			Description: "Needs the `domains:write` permission.",
			Request:     createInput{},
			// Created, not OK: the handler answers 201. And a shape at
			// last - all three of these were documented as returning
			// nothing while domainPayload was a map.
			Responses: []apidoc.Response{apidoc.Created("The result.", DetailResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/domains/:id",
			Tag:         "domains",
			Summary:     "Delete",
			Description: "Needs the `domains:delete` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "GET",
			Path:        "/domains/:id",
			Tag:         "domains",
			Summary:     "Get",
			Description: "Needs the `domains:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", DetailResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/domains/:id/verify",
			Tag:         "domains",
			Summary:     "Verify",
			Description: "Needs the `domains:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", DetailResponse{})},
		},
	}
}
