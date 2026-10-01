// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package audit

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
			Path:        "/audit-log",
			Tag:         "audit",
			Summary:     "Project log",
			Description: "Needs the `audit:read` permission.",
			Query:       []apidoc.Param{{Name: "limit", Type: "integer", Description: "Page size. Over-asking is clamped, never refused."}, {Name: "offset", Type: "integer", Description: "Rows to skip."}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", ListResponse{})},
		},
		{
			Method:  "GET",
			Path:    "/audit-log/export",
			Tag:     "audit",
			Summary: "Export the project log",
			Description: "Needs the `audit:read` permission. " +
				"`from` and `to` are optional and take a date (2026-08-01) or an " +
				"RFC 3339 timestamp - a bare date for `to` includes that whole day, " +
				"an explicit timestamp is an exclusive bound. Newest first, capped, " +
				"and `truncated` says whether the cap was reached.",
			Query:     []apidoc.Param{{Name: "from", Description: "A date (2026-08-01) or an RFC 3339 timestamp, inclusive."}, {Name: "to", Description: "A date, which includes that whole day, or an RFC 3339 timestamp, exclusive."}},
			Responses: []apidoc.Response{apidoc.OK("The result.", ExportResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/audit-log/:id",
			Tag:         "audit",
			Summary:     "Project event",
			Description: "Needs the `audit:read` permission.",
			PathParams:  []apidoc.Param{{Name: "id"}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", EventResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/security-log",
			Tag:         "audit",
			Summary:     "Security log",
			Description: "Any signed-in member, for their own account.",
			Query:       []apidoc.Param{{Name: "all", Type: "boolean", Description: "Every account's events rather than the caller's own. Honored only for a platform admin."}, {Name: "limit", Type: "integer", Description: "Page size. Over-asking is clamped, never refused."}, {Name: "offset", Type: "integer", Description: "Rows to skip."}},
			Responses:   []apidoc.Response{apidoc.OK("The result.", ListResponse{})},
		},
		{
			Method:  "GET",
			Path:    "/security-log/export",
			Tag:     "audit",
			Summary: "Export the security log",
			Description: "Any signed-in member, for their own account - `all=true` is " +
				"honored only for a platform admin. Same optional `from` and `to` as " +
				"the project export.",
			Query:     []apidoc.Param{{Name: "all", Type: "boolean", Description: "Every account's events rather than the caller's own. Honored only for a platform admin."}, {Name: "from", Description: "A date (2026-08-01) or an RFC 3339 timestamp, inclusive."}, {Name: "to", Description: "A date, which includes that whole day, or an RFC 3339 timestamp, exclusive."}},
			Responses: []apidoc.Response{apidoc.OK("The result.", ExportResponse{})},
		},
	}
}
