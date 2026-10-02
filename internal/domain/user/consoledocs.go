// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package user

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
			Path:        "/users/",
			Summary:     "List",
			Description: "Platform admin. Oldest first, the whole list unless `limit` asks for a page.",
			Query: []apidoc.Param{
				{Name: "q", Description: "Part of the address, case-insensitive."},
				{Name: "email", Description: "One whole address, without regard to case. Answers that account alone, and `q` is ignored."},
				{Name: "admin", Type: "boolean", Description: "Only administrators, or only everyone else."},
				{Name: "disabled", Type: "boolean", Description: "Only disabled accounts, or only enabled ones."},
				{Name: "limit", Type: "integer", Description: "Page size, at most 200. Without it the whole list is answered."},
				{Name: "offset", Type: "integer", Description: "Rows to skip."},
			},
			Responses: []apidoc.Response{apidoc.OK("The result.", ListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/users/",
			Summary:     "Create",
			Description: "Platform admin.",
			Request:     createInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", UserResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/users/:id",
			Summary:     "Delete",
			Description: "Platform admin.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "GET",
			Path:        "/users/:id",
			Summary:     "Get",
			Description: "Platform admin.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", UserResponse{})},
		},
		{
			Method:      "PATCH",
			Path:        "/users/:id",
			Summary:     "Update",
			Description: "Platform admin.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     updateInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", UserResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/users/:id/2fa",
			Summary:     "Reset t o t p",
			Description: "Platform admin.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", UserResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/users/:id/passkeys",
			Summary:     "Reset passkeys",
			Description: "Platform admin.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", PasskeyResetResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/users/:id/projects",
			Summary:     "Projects",
			Description: "Platform admin.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", ProjectsResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/users/:id/revoke-sessions",
			Summary:     "Revoke sessions",
			Description: "Platform admin.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", RevokedResponse{})},
		},
	}
}
