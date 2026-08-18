// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpcredential

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
			Path:        "/smtp-credentials/",
			Tag:         "smtpcredential",
			Summary:     "List",
			Description: "Needs the `apikeys:read` permission.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", ListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/smtp-credentials/",
			Tag:         "smtpcredential",
			Summary:     "Create",
			Description: "Needs the `apikeys:write` permission.",
			Request:     createInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", CreatedResponse{})},
		},
		{
			Method:      "DELETE",
			Path:        "/smtp-credentials/:id",
			Tag:         "smtpcredential",
			Summary:     "Delete",
			Description: "Needs the `apikeys:delete` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "POST",
			Path:        "/smtp-credentials/:id/revoke",
			Tag:         "smtpcredential",
			Summary:     "Revoke",
			Description: "Needs the `apikeys:write` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", CredentialResponse{})},
		},
	}
}
