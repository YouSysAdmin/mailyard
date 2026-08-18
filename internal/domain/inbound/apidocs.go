// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package inbound

import "github.com/yousysadmin/mailyard/internal/core/apidoc"

// APIDocs describes the read slice of received mail. Retry, delete and
// the raw download stay on the console surface, which is where an
// operator investigating a message already is.
func APIDocs() []apidoc.Route {
	return []apidoc.Route{
		{
			Method:     "GET",
			Path:       "/inbound-emails",
			Tag:        "inbound",
			Permission: "inbound:read",
			Summary:    "List mail received by the MX listener",
			Query: []apidoc.Param{
				{Name: "status"},
				{Name: "before", Format: "date-time"},
				{Name: "before_id", Description: "The id of the last row on the previous page. " +
					"Pass it with `before`, or rows sharing a received_at with the last one are skipped."},
				{Name: "limit", Type: "integer"},
			},
			Responses: []apidoc.Response{apidoc.OK("Newest first.", ListResponse{}), apidoc.BadRequest},
		},
		{
			Method:     "GET",
			Path:       "/inbound-emails/stats",
			Tag:        "inbound",
			Permission: "inbound:read",
			Summary:    "Count received mail by status",
			Responses:  []apidoc.Response{apidoc.OK("Counts keyed by status.", StatsResponse{})},
		},
		{
			Method:      "GET",
			Path:        "/inbound-emails/:id",
			Tag:         "inbound",
			Permission:  "inbound:read",
			Summary:     "One received message",
			Description: "`auth` carries the SPF, DKIM and DMARC verdicts stamped at ingest. `aligned` is the field worth acting on - a valid signature from some other domain is not authentication.",
			PathParams:  []apidoc.Param{{Name: "id", Format: "uuid"}},
			Responses:   []apidoc.Response{apidoc.OK("The message.", GetResponse{}), apidoc.NotFound},
		},
	}
}
