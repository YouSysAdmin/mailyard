// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package eventstream

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
			Path:        "/events/stats",
			Summary:     "Stats",
			Description: "Platform admin.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", StatsResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/events/stream",
			Summary:     "Stream",
			Description: "Needs the `notifications:read` permission in the project the X-Mailyard-Project-Id header names.",
			Responses:   []apidoc.Response{apidoc.EventStream("An open stream of project events.")},
		},
	}
}
