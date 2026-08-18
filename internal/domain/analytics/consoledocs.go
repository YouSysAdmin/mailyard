// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package analytics

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
			Path:        "/analytics",
			Tag:         "analytics",
			Summary:     "Analytics",
			Description: "Any signed-in member.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", TrendResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/dashboard/stats",
			Tag:         "analytics",
			Summary:     "Dashboard stats",
			Description: "Any signed-in member.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", StatsResponse{})},
		},
	}
}
