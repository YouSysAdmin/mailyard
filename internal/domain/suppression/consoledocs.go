// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package suppression

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
			Method:      "DELETE",
			Path:        "/suppressions/",
			Tag:         "suppression",
			Summary:     "Delete",
			Description: "Needs the `suppressions:delete` permission.",
			Responses:   []apidoc.Response{apidoc.NoContent},
		},
		{
			Method:      "GET",
			Path:        "/suppressions/",
			Tag:         "suppression",
			Summary:     "List",
			Description: "Needs the `suppressions:read` permission.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", ListResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/suppressions/",
			Tag:         "suppression",
			Summary:     "Create",
			Description: "Needs the `suppressions:write` permission.",
			Request:     createInput{},
			Responses:   []apidoc.Response{apidoc.Created("The result.", CreateResponse{})},
		},
	}
}
