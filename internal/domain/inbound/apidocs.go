// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package inbound

import "github.com/yousysadmin/mailyard/internal/core/apidoc"

// APIDocs describes the read slice of received mail. Retry, delete and
// the raw download stay on the console surface, which is where an
// operator investigating a message already is.
func APIDocs() []apidoc.Route {
	return []apidoc.Route{
		{
			Method:      "GET",
			Path:        "/inbound-emails",
			Tag:         "inbound",
			Permission:  "inbound:read",
			Summary:     "List mail received by the MX listener",
			Description: "Cursor paged, newest first: follow `next_cursor` until it comes back empty.",
			Query: []apidoc.Param{
				{Name: "status", Enum: []string{"received", "rejected", "failed"}},
				{Name: "sender", Description: "Part of the sender (the From header), case-insensitive."},
				{Name: "recipient", Description: "Part of any envelope recipient, Bcc included, case-insensitive."},
				{Name: "search", Description: "Part of the subject, case-insensitive."},
				{Name: "limit", Type: "integer"},
				{Name: "cursor", Description: "The next_cursor of the previous page."},
			},
			Responses: []apidoc.Response{apidoc.OK("Newest first.", ListResponse{}), apidoc.BadRequest},
		},
		{
			Method:     "GET",
			Path:       "/inbound-emails/stats",
			Tag:        "inbound",
			Permission: "inbound:read",
			Summary:    "Count received mail by status",
			Query:      []apidoc.Param{{Name: "from", Description: "A date (2026-08-01) or an RFC 3339 timestamp, inclusive."}, {Name: "to", Description: "A date, which includes that whole day, or an RFC 3339 timestamp, exclusive."}},
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
