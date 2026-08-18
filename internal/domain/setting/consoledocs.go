// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package setting

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
			Path:        "/jobs",
			Tag:         "setting",
			Summary:     "Jobs",
			Description: "Platform admin.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", JobsResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/jobs/:name/run",
			Tag:         "setting",
			Summary:     "Run job",
			Description: "Platform admin.",
			PathParams:  []apidoc.Param{{Name: "name"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", JobsResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/settings",
			Tag:         "setting",
			Summary:     "List",
			Description: "Platform admin.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", ListResponse{})},
		},
		{
			Method:      "PUT",
			Path:        "/settings",
			Tag:         "setting",
			Summary:     "Update",
			Description: "Platform admin.",
			Request:     updateInput{},
			Responses:   []apidoc.Response{apidoc.OK("Every setting, after the write.", ListResponse{})},
		},
	}
}
