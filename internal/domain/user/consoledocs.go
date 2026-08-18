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
			Tag:         "user",
			Summary:     "List",
			Description: "Platform admin.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", ListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/users/",
			Tag:         "user",
			Summary:     "Create",
			Description: "Platform admin.",
			Request:     createInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", UserResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/users/:id",
			Tag:         "user",
			Summary:     "Delete",
			Description: "Platform admin.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "GET",
			Path:        "/users/:id",
			Tag:         "user",
			Summary:     "Get",
			Description: "Platform admin.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", UserResponse{})},
		},
		{
			Method:      "PATCH",
			Path:        "/users/:id",
			Tag:         "user",
			Summary:     "Update",
			Description: "Platform admin.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Request:     updateInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", UserResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/users/:id/2fa",
			Tag:         "user",
			Summary:     "Reset t o t p",
			Description: "Platform admin.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", UserResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/users/:id/passkeys",
			Tag:         "user",
			Summary:     "Reset passkeys",
			Description: "Platform admin.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", PasskeyResetResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/users/:id/projects",
			Tag:         "user",
			Summary:     "Projects",
			Description: "Platform admin.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", ProjectsResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/users/:id/revoke-sessions",
			Tag:         "user",
			Summary:     "Revoke sessions",
			Description: "Platform admin.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", RevokedResponse{})},
		},
	}
}
