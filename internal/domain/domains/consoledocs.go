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
			// Created, not OK: the handler answers 201.
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
			Description: "Needs the `domains:write` permission. Re-checks every DNS record, and completes a pending DKIM rotation once the new record is published.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", DetailResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/domains/:id/dkim/rotate",
			Tag:         "domains",
			Summary:     "Rotate DKIM key",
			Description: "Needs the `domains:write` permission. Mints the next signing key under the other selector. Signing stays on the current key until verify sees the new record published.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The domain with the new record to publish.", DetailResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/domains/:id/dkim/rotate",
			Tag:         "domains",
			Summary:     "Cancel DKIM rotation",
			Description: "Needs the `domains:write` permission. Discards a pending key, the current one is untouched.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", DetailResponse{})},
		},
	}
}
