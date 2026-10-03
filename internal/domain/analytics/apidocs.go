// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package analytics

import "github.com/yousysadmin/mailyard/internal/core/apidoc"

// APIDocs describes the read-only aggregates. Both are scoped to the
// key's project: there is no cross-project view on this surface.
func APIDocs() []apidoc.Route {
	return []apidoc.Route{
		{
			Method:      "GET",
			Path:        "/dashboard/stats",
			Permission:  "analytics:read",
			Summary:     "Aggregate counts for a dashboard",
			Description: "Emails by status, inbound by status, configured resources, and the failure rate over finalized mail only.",
			Responses:   []apidoc.Response{apidoc.OK("The summary.", StatsResponse{})},
		},
		{
			Method:     "GET",
			Path:       "/analytics",
			Permission: "analytics:read",
			Summary:    "Daily delivery trend and status breakdown",
			Description: "The window defaults to the trailing 30 days and may not exceed 366. " +
				"`daily_counts` includes days with no traffic, so a chart cannot silently " +
				"rescale its axis, and `from`/`to` echo the window actually applied.",
			Query: []apidoc.Param{
				{Name: "from", Description: "A date (2026-08-01) or an RFC 3339 timestamp, inclusive."},
				{Name: "to", Description: "A date, which includes that whole day, or an RFC 3339 timestamp, exclusive."},
				{Name: "status"},
			},
			Responses: []apidoc.Response{
				apidoc.OK("The trend.", TrendResponse{}),
				apidoc.BadRequest,
			},
		},
	}
}
