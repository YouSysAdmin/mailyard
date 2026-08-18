// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package data

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
			Path:        "/data/export",
			Tag:         "data",
			Summary:     "Export data",
			Description: "Needs the `data:read` permission.",
			Responses:   []apidoc.Response{apidoc.OK("The result.", ExportResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/data/delete-contacts",
			Tag:         "data",
			Summary:     "Delete contacts",
			Description: "Needs the `data:delete` permission.",
			Request:     deleteContactsInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", ErasureResponse{})},
		},
		{
			Method:      "POST",
			Path:        "/data/delete-email-logs",
			Tag:         "data",
			Summary:     "Delete email logs",
			Description: "Needs the `data:delete` permission.",
			Request:     deleteLogsInput{},
			Responses:   []apidoc.Response{apidoc.OK("The result.", ErasureResponse{})},
		},
	}
}
